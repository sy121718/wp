package captcha

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/response"
)

// HTTPServe 是通用验证码 HTTP 出口：只产出验证码事实，路由/豁免/限流归应用装配。
func HTTPServe(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	svc := Get()
	id, image := svc.GenerateImage()
	response.Success(c, gin.H{
		"captcha_id":     id,
		"captcha_image":  image,
		"expire_seconds": svc.ExpireSeconds(),
	})
}
