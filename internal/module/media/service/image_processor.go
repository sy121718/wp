package mediaservice

// image_processor.go — media 模块侧图片处理封装：
// 变体生成使用纯 Go 库（disintegration/imaging 缩放 + HugoSmits86/nativewebp 无损编码），
// 只服务本模块的变体产物，不做通用图片工具。防御约定：
//   - 处理前用标准库 image.DecodeConfig 读 header，解码失败或任一边长超过
//     maxVariantSourceEdge（6000px）则拒绝生成（调用方将该变体记为 failed），不阻塞上传；
//   - nativewebp 硬限宽高 ≤16384，变体目标边长均远小于该值，无越界风险；
//   - svg/gif 不参与变体生成（svg 为矢量无需位图，gif 多帧转码丢帧），在调用方过滤。

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"  // 注册 gif 解码器（DecodeConfig 探测用；变体本身排除 gif）
	_ "image/jpeg" // 注册 jpeg 解码器
	_ "image/png"  // 注册 png 解码器
	"io"

	mediamodel "go_wp/internal/module/media/model"

	"github.com/HugoSmits86/nativewebp"
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

// encodeWebP 把任意 image.Image 编码为无损 VP8L webp
// （nativewebp.Encode 接受任意 image.Image，内部自动转 NRGBA）。
func encodeWebP(w io.Writer, img image.Image) error {
	return nativewebp.Encode(w, img, &nativewebp.Options{CompressionLevel: nativewebp.DefaultCompression})
}

// encodeWebPBytes 编码为字节切片，供落盘写入。
func encodeWebPBytes(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeWebP(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// buildVariantImage 按变体类型生成目标图像：
//
//	thumb  = imaging.Fit 320x320 Lanczos
//	medium = imaging.Fit 1280x1280 Lanczos
//	webp   = 原图尺寸（重编码）
func buildVariantImage(src image.Image, variantType string) (image.Image, error) {
	switch variantType {
	case mediamodel.VariantTypeThumb:
		return imaging.Fit(src, thumbVariantEdge, thumbVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeMedium:
		return imaging.Fit(src, mediumVariantEdge, mediumVariantEdge, imaging.Lanczos), nil
	case mediamodel.VariantTypeWebp:
		return src, nil
	default:
		return nil, errUnknownVariantType
	}
}
