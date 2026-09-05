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
// GET /api/captcha → 返回 captcha_id 与 base64 PNG 图片；
// 答案 code 只保存在服务端 Store 供 Verify 校验，绝不下发客户端。
func CaptchaHandle(c *gin.Context) {
	id, image := captcha.Get().GenerateImage()
	r.Success(c, gin.H{
		"captcha_id":    id,
		"captcha_image": image,
	})
}
