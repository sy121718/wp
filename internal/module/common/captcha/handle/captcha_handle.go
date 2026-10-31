package handle

import (
	"go_wp/pkg/captcha"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func CaptchaHandle(c *gin.Context) {
	svc := captcha.Get()
	id, image := svc.GenerateImage()
	r.Success(c, gin.H{
		"captcha_id":     id,
		"captcha_image":  image,
		"expire_seconds": svc.ExpireSeconds(),
	})
}
