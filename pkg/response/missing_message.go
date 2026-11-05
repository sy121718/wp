package response

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/logger"
)

// missingMessageScene 是「词库缺词」日志的固定场景名。
// 与业务场景分开，巡检时一条 grep 就能捞出所有未登记的词条。
const missingMessageScene = "i18n"

// logMissingMessage 记录一次「业务错误声明有 key、词库没有对应词条」的出口。
//
// 这类缺词在前端只表现为通用提示（不把裸 key 交给前端），所以必须有一条可定位的记录：
// 记 key、请求语言、调用场景与请求路径，够拼出补词用的迁移；不记请求参数、错误明细与堆栈，
// 那些可能带用户输入或内部原文，而缺词本身不是故障，不需要排查上下文。
func logMissingMessage(c *gin.Context, scene, key string) {
	if key == "" {
		return
	}
	if scene == "" {
		scene = "http"
	}
	fields := map[string]any{
		"key":    key,
		"lang":   requestLanguage(c),
		"caller": scene,
	}
	if c != nil && c.Request != nil {
		fields["path"] = c.Request.URL.Path
	}
	logger.SceneCtx(missingMessageScene, requestContext(c)).WithFields(fields).
		Warn("错误声明有 key、词库缺词：前端退成归口提示")
}
