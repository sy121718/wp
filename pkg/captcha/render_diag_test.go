package captcha

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
		t.Fatalf("深色像素仅 %d，字符可能未绘制（预期 ≥500）", dark)
	}
	t.Logf("深色像素 %d（字符绘制正常）", dark)
}

func TestZeroConfigGeneratesCode(t *testing.T) {
	Init(&Config{})
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
