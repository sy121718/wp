package shell

// authz.go — 后台**页面 GET** 鉴权的出口（判定在 internal/middleware/builtin/page_authz.go）。
//
// 分层：中间件判定 + 标记拒绝，本包负责「拒绝长什么样」—— 判定不认识 i18n，也不该认识 HTML。
//
// 与 API 侧唯一的差别是**拒绝的形态**（同一个判定，两种出口）：
//   · 未登录 → 302 到 /admin/login，与既有页面语义一致（API 侧返回 401 JSON）；
//   · 无权限 → 状态码 + 整页提示（jump 形态），不是 JSON 报文 —— 直接输 URL 的人看得懂
//     发生了什么、知道去找谁开权限；
//   · htmx 请求 → HX-Redirect 回后台首页：htmx 对 4xx 默认不 swap，直接回 403 的表现是
//     「点了没反应」，比看到提示页更糟。

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
)

const (
	// adminLoginPath 后台登录页：未登录的**页面**请求 302 到这里（API 请求返回 401 JSON）。
	adminLoginPath = "/admin/login"
	// backToAdminKey 「返回后台」词条：复用 page_redirects 页已有的那一条，
	// 不为一次拒绝提示新增全站词条（新增 key 必须同时补 seed，否则 i18n 门禁判失败）。
	backToAdminKey = "admin.redirect.back"
)

// PageAuthz 后台页面 GET 鉴权中间件的**唯一约定写法**：判定取 builtin 的实现，
// 出口绑本包 RejectPageRequest。页面 router 里写在 GET 的第二个参数位置。
func PageAuthz(obj string) gin.HandlerFunc {
	return builtin.PageCasbinMiddleware(obj, RejectPageRequest)
}

// RejectPageRequest 页面被拒时的整页出口（详情只进日志，对外给通用文案 —— 与 PageError 同一取舍）。
func RejectPageRequest(c *gin.Context, status int, key string) {
	if status == http.StatusUnauthorized {
		if IsHXRequest(c) {
			c.Header("HX-Redirect", adminLoginPath)
			c.Status(http.StatusOK)
			return
		}
		c.Redirect(http.StatusFound, adminLoginPath)
		return
	}

	text := TranslateFor(c)(key, "无权限访问")
	back := TranslateFor(c)(backToAdminKey, "返回后台")
	if IsHXRequest(c) {
		c.Header("HX-Redirect", adminHomePath)
		c.Status(http.StatusOK)
		return
	}

	// 与 RenderJump 同一形态（提示页 + 自动回落），差别只在状态码：
	// 这是「访问被拒」，客户端与监控必须能从状态码看出来（200 会让失败看起来像成功）。
	c.Header("Cache-Control", "no-store")
	c.HTML(status, "admin/jump.html", Prepare(c, gin.H{
		"title": text,
		"Jump": jumpView{
			OK:       false,
			Msg:      text,
			Back:     adminHomePath,
			BackText: back,
			Seconds:  0,
		},
	}))
}
