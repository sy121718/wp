package mediaservice

// image_processor.go — media 模块侧图片处理封装：
// 变体生成使用纯 Go 库（disintegration/imaging 缩放 + 标准库 image/jpeg 有损编码），
// 只服务本模块的变体产物，不做通用图片工具。防御约定：
//   - 处理前用标准库 image.DecodeConfig 读 header，解码失败或任一边长超过
//     maxVariantSourceEdge（6000px）则拒绝生成（调用方将该变体记为 failed），不阻塞上传；
//   - 编码统一 JPEG（有损，质量 variantJPEGQuality）：此前用 nativewebp 无损编码，
//     实测 1280px 变体可达 1.7MB，体积是页面变慢的主因；JPEG 同尺寸通常 150~300KB；
//   - JPEG 不支持透明：带 alpha 的图先合成到白底（flattenToOpaque），避免透明区变黑；
//   - svg/gif 不参与变体生成（svg 为矢量无需位图，gif 多帧转码丢帧），在调用方过滤。

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // 注册 gif 解码器（DecodeConfig 探测用；变体本身排除 gif）
	"image/jpeg"
	_ "image/png" // 注册 png 解码器
	"io"

	// webp 解码器（纯 Go）：源图本身是 .webp 时 DecodeConfig/Decode 才可用。
	// 此前只注册 gif/jpeg/png，webp 源图会以 "unknown format" 跳过变体生成。
	_ "golang.org/x/image/webp"

	mediamodel "go_wp/internal/module/media/model"

	"github.com/disintegration/imaging"
)

// 变体处理的包内哨兵错误（面向用户的文案统一经 mediaenums，由调用方包装）.
var (
	errInvalidImageDimensions = errors.New("图片尺寸非法")
	errImageTooLarge          = errors.New("图片尺寸超过变体生成上限")
	errUnknownVariantType     = errors.New("未知变体类型")
)

const (
	// maxVariantSourceEdge 变体源图任一边长上限：超过则跳过变体（防御
	// 超大图解码的内存/CPU 放大，6000px 与设计规格一致）。
	maxVariantSourceEdge = 6000

	// thumbVariantEdge 缩略图变体的 Fit 边长（320x320）。
	thumbVariantEdge = 320

	// mediumVariantEdge 调节尺寸变体的 Fit 边长（1280x1280）。
	mediumVariantEdge = 1280

	// smallVariantEdge 中间档变体的 Fit 边长（768x768）。
	//
	// 取值理由：组件的 sizes 是「≤640px 视口 100vw、其余 50vw」，DPR=2 的 375pt
	// 手机需要约 750 设备像素。取 640 会让这些机型仍然只能选 1280（规则是「满足
	// 所需的最小候选」，640 < 750 不满足），取 768 才真正落进这一档。
	smallVariantEdge = 768

	// variantJPEGQuality JPEG 变体编码质量：82 是「视觉无损」常用档，
	// 相比无损 webp 体积小 5~10 倍，是页面加载速度的关键。
	variantJPEGQuality = 82
)

// probeImage 读取图片 header 探测尺寸，返回 (宽, 高, err)。
// 解码失败或任一边超过 maxVariantSourceEdge 时返回错误——调用方据此
// 把变体记录标记为 failed 并跳过生成（status=failed 语义）。
func probeImage(r io.Reader) (int, int, error) {
	cfg, _, err := image.DecodeConfig(r)
	if err != nil {
		return 0, 0, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, errInvalidImageDimensions
	}
	if cfg.Width > maxVariantSourceEdge || cfg.Height > maxVariantSourceEdge {
		return cfg.Width, cfg.Height, errImageTooLarge
	}
	return cfg.Width, cfg.Height, nil
}

// decodeImage 完整解码图片为 image.Image（调用方保证已通过 probeImage 防御）。
// AutoOrientation 按 EXIF 方向摆正，变体与视觉方向一致。
func decodeImage(r io.Reader) (image.Image, error) {
	return imaging.Decode(r, imaging.AutoOrientation(true))
}

// flattenToOpaque 把带 alpha 的图像合成到白底（JPEG 无透明通道）。
// 无 alpha 的图原样返回，避免不必要的拷贝。
func flattenToOpaque(img image.Image) image.Image {
	if _, ok := img.(*image.NRGBA); !ok {
		// 常见不透明类型（YCbCr / RGBA 且不透明）直接返回。
		switch img.(type) {
		case *image.YCbCr, *image.Gray:
			return img
		}
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, b, img, b.Min, draw.Over)
	return dst
}

// encodeJPEGBytes 编码为有损 JPEG 字节切片（落盘写入用）。
func encodeJPEGBytes(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flattenToOpaque(img), &jpeg.Options{Quality: variantJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildVariantImage 按变体类型生成目标图像：
//
//	thumb  = imaging.Fit 320x320 Lanczos
//	small  = imaging.Fit 768x768 Lanczos
//	medium = imaging.Fit 1280x1280 Lanczos
//	full   = 原图尺寸（重编码为 JPEG）
func buildVariantImage(src image.Image, variantType string) (image.Image, error) {
	switch variantType {
	case mediamodel.VariantTypeThumb:
		return imaging.Fit(src, thumbVariantEdge, thumbVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeSmall:
		return imaging.Fit(src, smallVariantEdge, smallVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeMedium:
		return imaging.Fit(src, mediumVariantEdge, mediumVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeFull:
		return src, nil
	default:
		return nil, errUnknownVariantType
	}
}
