package adminui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/view"
)

type failureData struct {
	Status      int
	Fact        facing.Fact
	Args        []string
	Details     []failureDetail
	PlainDetail string
	Items       []facing.BlockedItem
}

type failureDetail struct {
	Key  string
	Args map[string]any
}

// RenderFailure 只传失败事实给 Jet；错误模板不依赖登录态、菜单或业务查询。
// 早期中间件尚未运行普通页面取词接线，故在这里装配本请求的语言。
func RenderFailure(c *gin.Context, status int, scene string, err error) {
	if c.Writer.Written() {
		return
	}
	text.AttachScope(c)
	data := failureData{Status: status, Fact: text.FactOf(c, scene, err)}
	if data.Fact.Business {
		// 解开错误事实的位置参数，交 Jet 取词；编码串本身不是词条键。
		if key, tail, ok := strings.Cut(data.Fact.Key, "|"); ok {
			data.Fact.Key = key
			// fallback 是调用点给的明确消息事实，保留它；清空会让缺词时退成归口文案。
			data.Args = strings.Split(tail, "|")
		}
		parts, _ := i18n.ParseErrorDetails(data.Fact.Tail)
		if len(parts) == 0 && !i18n.EncodedDetailLike(data.Fact.Tail) {
			data.PlainDetail = data.Fact.Tail
		}
		for _, part := range parts {
			args := make(map[string]any, len(part.Args))
			for name, value := range part.Args {
				args[name] = value
			}
			data.Details = append(data.Details, failureDetail{Key: part.Key, Args: args})
		}
		var blocked facing.BlockedError
		if errors.As(err, &blocked) {
			data.Items = blocked.BlockedItems()
		}
	}
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
