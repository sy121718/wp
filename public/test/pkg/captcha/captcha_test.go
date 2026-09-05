// Package captcha_test 验证码链路测试（C4 修复回归）：
//   - 接口响应只含 captcha_id 与 captcha_image，绝不出现明文 code
//   - GenerateImage 返回合法 base64 PNG data URL
//   - 测试内获取验证码答案的唯一途径：同进程直调 pkg/captcha 的 Get().Generate()
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

// TestCaptchaGenerateImageReturnsPNG GenerateImage 返回非空 id 与合法 base64 PNG 图片。
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

// TestCaptchaVerifyWithInProcessGenerate 答案获取方式回归：同进程直调 Generate
// 拿到的答案可通过 Verify，错误答案不通过（接口层不回 code 后的唯一取答案途径）。
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

// TestCaptchaHandleResponseNoPlaintextCode C4 回归：验证码接口响应
// 只含 captcha_id 与 captcha_image，不出现任何明文答案字段。
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

	// data 结构断言：仅 captcha_id + captcha_image 两个字段，杜绝明文回显
	if len(result.Data) != 2 {
		t.Fatalf("data 应仅含 captcha_id 与 captcha_image，got: %v", result.Data)
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
