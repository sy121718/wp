package captcha

// 诊断测试：渲染验证码并统计深色像素（字符笔迹），验证字符确实画上画布。
// 一次性诊断工具（验证码清晰度排查），保留作回归。

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"testing"
)

func TestRenderPixelsContainGlyph(t *testing.T) {
	Init(&Config{Length: 6, Width: 120, Height: 40})
	svc := Get()
	_, dataURL := svc.GenerateImage()
	const prefix = "data:image/png;base64,"
	if !bytes.HasPrefix([]byte(dataURL), []byte(prefix)) {
		t.Fatalf("data URL 前缀错误")
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[len(prefix):])
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("PNG 解码失败: %v", err)
	}
	// 统计深色像素（字符颜色 RGB 分量 0~60 → 16bit RGBA ≈ 0~15400）。
	dark := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r < 0x4000 && g < 0x4000 && b < 0x4000 {
				dark++
			}
		}
	}
	// 6 位数字点阵（scale≥2），深色像素应显著（≥500）。
	if dark < 500 {
		t.Fatalf("深色像素仅 %d，字符可能未绘制（预期 ≥500）", dark)
	}
	t.Logf("深色像素 %d（字符绘制正常）", dark)
}

// TestZeroConfigGeneratesCode 零配置兜底回归：config.yaml 缺 captcha 段时
// （GetInt 全零）Length 兜底 6，验证码非空且渲染含字符笔迹。
// 根因回归：Length=0 → generateDigitCode(0) 空串 → 图上零字符（用户无法登录）。
func TestZeroConfigGeneratesCode(t *testing.T) {
	Init(&Config{}) // 模拟 config.yaml 无 captcha 段：全零配置
	svc := Get()
	id, dataURL := svc.GenerateImage()
	if id == "" {
		t.Fatalf("id 为空")
	}
	const prefix = "data:image/png;base64,"
	raw, err := base64.StdEncoding.DecodeString(dataURL[len(prefix):])
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("PNG 解码失败: %v", err)
	}
	// 深色像素（字符 0~60）应显著（6 位数字 scale≥2 预期 ≥500）。
	dark := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r < 0x4000 && g < 0x4000 && b < 0x4000 {
				dark++
			}
		}
	}
	if dark < 500 {
		t.Fatalf("零配置下深色像素仅 %d，字符未绘制（回归！）", dark)
	}
	t.Logf("零配置深色像素 %d（兜底生效）", dark)
}
