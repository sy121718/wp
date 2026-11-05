package adminui

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
	"go_wp/pkg/view"
)

// SuccessTemplate 是唯一共享的成功提示组件（只输出一条消息，不解释业务 data）。
const SuccessTemplate = "admin/partials/write_success.html"

// NewSuccessWriter 返回统一成功出口：默认“操作成功”，自定义消息只消费明确消息事实。
//
// 它不接模板名、业务 data、实体 ID、HTML、URL 或导航配置；
// 成功后刷新/关闭/导航由发起页面声明、共享前端基座执行。
func NewSuccessWriter() view.SuccessWriter {
	return func(c *gin.Context, fact view.SuccessFact) {
		if c == nil {
			return
		}
		// 与失败出口一致：提示组件按本请求语言取词。
		text.AttachScope(c)
		c.Header("Cache-Control", "no-store")
		c.HTML(http.StatusOK, SuccessTemplate, gin.H{
			"HasSuccessMessage": fact.Key != "" || fact.Fallback != "",
			"SuccessKey":        fact.Key,
			"SuccessFallback":   fact.Fallback,
			"SuccessArgs":       fact.Args,
		})
	}
}

// WriteSuccess 兼容既有的直写调用点，统一走同一提示组件。
func WriteSuccess(c *gin.Context) {
	view.SuccessWith(c, view.SuccessFact{})
}
