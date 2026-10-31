package captcha_test

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	"go_wp/internal/module/common/captcha/handle"
	"go_wp/pkg/captcha"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

const pngDataURLPrefix = "data:image/png;base64,"

func TestCaptchaGenerateImageReturnsPNG(t *testing.T) {
	id, image := captcha.Get().GenerateImage()
	if id == "" {
		t.Fatal("captcha_id 不应为空")
	}
	if !strings.HasPrefix(image, pngDataURLPrefix) {
		t.Fatalf("captcha_image 应为 PNG data URL，got: %.40s", image)
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(image, pngDataURLPrefix))
	if err != nil {
		t.Fatalf("图片 base64 解码失败: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("图片应为合法 PNG: %v", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		t.Fatalf("图片尺寸异常: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestCaptchaVerifyWithInProcessGenerate(t *testing.T) {
	id, code := captcha.Get().Generate()
	if !captcha.Get().Verify(id, code, true) {
		t.Fatal("同进程直调 Generate 获取的答案应校验通过")
	}

	id2, code2 := captcha.Get().Generate()
	if captcha.Get().Verify(id2, code2+"0", true) {
		t.Fatal("错误答案不应校验通过")
	}
}

func TestCaptchaHandleResponseNoPlaintextCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/api/captcha", handle.CaptchaHandle)

	recorder, err := support.SendRequest(engine, support.RequestOptions{
		Method: "GET",
		Path:   "/api/captcha",
	})
	if err != nil {
		t.Fatalf("请求验证码接口失败: %v", err)
	}

	var result struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := support.DecodeResponseBody(recorder, &result); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	if len(result.Data) != 3 {
		t.Fatalf("data 应仅含 captcha_id、captcha_image、expire_seconds，got: %v", result.Data)
	}
	if _, ok := result.Data["expire_seconds"]; !ok {
		t.Fatal("缺少 expire_seconds 字段")
	}
	id, _ := result.Data["captcha_id"].(string)
	image, _ := result.Data["captcha_image"].(string)
	if id == "" {
		t.Fatal("captcha_id 不应为空")
	}
	if !strings.HasPrefix(image, pngDataURLPrefix) {
		t.Fatalf("captcha_image 应为 PNG data URL，got: %.40s", image)
	}
}
