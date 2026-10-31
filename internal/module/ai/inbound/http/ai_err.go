package aihttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	aienums "go_wp/internal/module/ai/enums"
	"go_wp/pkg/logger"
)

const aiErrScene = "ai"

func aiErrKey(err error) string {
	if err == nil {
		return ""
	}
	if fact, ok := adminui.BusinessErrFact(err); ok {
		return fact.Key
	}
	logger.Scene(aiErrScene).Error(err, "ai 模块页面错误")
	return aienums.ErrInternal
}

func aiErrFallback(key string) string {
	if text, ok := aienums.FacingText(key); ok {
		return text
	}
	if text, ok := aienums.FacingText(aienums.ErrInternal); ok {
		return text
	}
	return "操作失败，请稍后重试"
}

func aiMsgText(c *gin.Context, key, fallback string) string {
	return adminui.MsgText(c, adminui.Msg{Key: key, Fallback: fallback})
}

func aiErrText(c *gin.Context, err error) string {
	key := aiErrKey(err)
	if key == "" {
		return ""
	}
	return aiMsgText(c, key, aiErrFallback(key))
}

func internalFallback(c *gin.Context) string { return facingText(c, aienums.ErrInternal) }
