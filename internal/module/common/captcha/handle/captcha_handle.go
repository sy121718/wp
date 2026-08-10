// Package handle 验证码公共模块的 HTTP 控制器。
//
// 验证码由 pkg/captcha 组件生成并存储于内存，此层只负责 HTTP 接口处理。
// 不需要 service、model 层，直接调用 pkg/captcha 单例。
package handle

import (
	"go_wp/pkg/captcha"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// CaptchaHandle 获取登录验证码。
// GET /api/captcha → 返回 captcha_id 和 captcha 文本码。
func CaptchaHandle(c *gin.Context) {
	id, code := captcha.Get().Generate()
	r.Success(c, gin.H{
		"captcha_id": id,
		"captcha":    code,
	})
}
