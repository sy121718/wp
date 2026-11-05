package adminui

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
	"go_wp/pkg/view"
	jet "go_wp/pkg/view/jet"
)

type failureData = jet.FailureData

type failureDetail = jet.FailureDetail

// RenderFailure 只传失败事实给 Jet；错误模板不依赖登录态、菜单或业务查询。
// 早期中间件尚未运行普通页面取词接线，故在这里装配本请求的语言。
func RenderFailure(c *gin.Context, status int, scene string, err error) {
	if c.Writer.Written() {
		return
	}
	text.AttachScope(c)
	data := jet.BuildFailureData(status, text.FactOf(c, scene, err), err)
	c.Header("Cache-Control", "no-store")
	tpl := "admin/errors/page.html"
	if view.IsFragment(c) {
		tpl = "admin/errors/failure.html"
	}
	c.HTML(status, tpl, data)
	if !c.Writer.Written() {
		// 错误模板自身失败：不递归、不改回 JSON；浏览器基座负责空响应的通用反馈。
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusInternalServerError)
		c.Writer.WriteHeaderNow()
	}
}
