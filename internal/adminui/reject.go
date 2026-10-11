// Package adminui 承载后台整页外壳里**归属 admin 的那部分**：页面鉴权被拒的出口（reject.go）
// 与侧栏视图构建（nav.go，task-72 自 internal/shell/nav.go 整体迁入）。
//
// 为什么独立成包：判定在 internal/middleware/builtin（只认请求管道，不认 HTML），
// 出口要渲染整页 —— 两者混在一层会让判定被渲染依赖污染（见 builtin/page_authz.go 文件头）；
// 侧栏同理：菜单树的真源是 sys_menus 表、按权限过滤在 admin 模块，只有「把树变成视图」这一步属外壳知识。
//
// 分层方向（不可反转）：
//
//	internal/middleware/builtin  判定 + 标记拒绝（PageRejectFunc 注入点）
//	internal/adminui             拒绝长什么样（reject.go）+ 侧栏长什么样（nav.go）
//	                             + 路由侧唯一约定写法（authz.go，task-92 批 4a 自 shell 迁入）
//
// 本包是**叶包**：本仓内只依赖 middleware/builtin / templates / pkg/response / admin 模块的 dto /
// builder/core / pkg/pathkit（外加 gin 与标准库），**绝不 import internal/shell** ——
// shell 反向引用本包，一旦本包又引用 shell 即成环。依赖清单可复算：
// `go list -f '{{join .Imports "\n"}}' ./internal/adminui`。
//
// 壳键（lang / t / langs / lang_redirect / csrf_token / NavGroups / SidebarOpen / SidebarPinned /
// HasSubnav / PermSet / Buttons）不由本包组装，而是由调用方通过 ShellInjector 注入：
// 本包不认识 adminui.Prepare，只认这个函数值签名（过渡期由 routes 侧传 adminui.Prepare）。
package adminui

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/reqparams"
)

const (
	// adminLoginPath 后台登录页：未登录的**页面**请求 302 到这里（API 请求返回 401 JSON）。
	adminLoginPath = "/admin/login"
	// adminHomePath 控制面首页：拒绝页的「回到首页」与 htmx 的 HX-Redirect 目标。
	adminHomePath = "/admin"
	// backToAdminKey 「返回后台」词条：复用 page_redirects 页已有的那一条，
	// 不为一次拒绝提示新增全站词条（新增 key 必须同时补 seed，否则 i18n 门禁判失败）。
	backToAdminKey = "admin.redirect.back"
)

// ShellInjector 把外壳键补进渲染数据，签名与 adminui.Prepare 逐字一致。
//
// 之所以是函数值而不是接口：出口只需要「补壳键」这一个能力，注入点保持一处，
// 且本包不必知道壳是谁实现的（终局由本包自持时，调用方不再传这个参数）。
type ShellInjector func(c *gin.Context, data gin.H) gin.H

// Reject 返回可直接交给 builtin.PageCasbinMiddleware(As) 的页面拒绝出口。
//
// inject 必须非空（调用方给壳键注入器，如 adminui.Prepare）；本函数不持有任何包级状态，
// 每次调用返回新闭包，便于路由侧按需构造。
//
// 分档（与迁移前逐字一致，四件事都是用户可见行为）：
//   - 未登录（401）：非 htmx → 302 /admin/login；htmx → 200 + HX-Redirect（htmx 对 4xx 不 swap，
//     直接回 401 的表现是「点了没反应」）；
//   - 无权限/内部错误（403/500）：非 htmx → **真实状态码** + Cache-Control: no-store + 整页提示
//     （200 会让失败看起来像成功，监控与客户端都靠状态码区分）；htmx → 200 + HX-Redirect /admin。
func Reject(inject ShellInjector) builtin.PageRejectFunc {
	return func(c *gin.Context, status int, key string) {
		if status == http.StatusUnauthorized {
			if isHXRequest(c) {
				c.Header("HX-Redirect", adminLoginPath)
				c.Status(http.StatusOK)
				return
			}
			c.Redirect(http.StatusFound, adminLoginPath)
			return
		}

		if isHXRequest(c) {
			c.Header("HX-Redirect", adminHomePath)
			c.Status(http.StatusOK)
			return
		}

		// 与 shell.RenderJump 同一形态（整页提示 + 自动回落），差别只在状态码：
		// 视图与渲染入口共用 JumpView / RenderJumpPage，字段名漂移不会两边各错一次。
		//
		// 文案走 i18n（分页条等 Go 侧拼接文案同一取舍）：词条缺失时回落调用点给的原文，
		// 绝不输出空串 —— 正文与链接文字都按 key + 中文兜底给出，取词在模板里发生。
		RenderJumpPageWith(c, status, JumpView{
			OK:          false,
			MsgKey:      key,
			Msg:         "无权限访问",
			Back:        adminHomePath,
			BackTextKey: backToAdminKey,
			BackText:    "返回后台",
		}, inject)
	}
}

// isHXRequest 是否为 htmx 发起的请求（请求头 HX-Request: true）。
//
// 判据统一由 pkg/reqparams 提供；请求头矩阵测试守住大小写与首尾空白行为。
func isHXRequest(c *gin.Context) bool {
	return reqparams.IsHXRequest(c)
}
