package runtimefragment

// capability.go — 内置运行时片段能力（0-D 首批，docs/04 §1.1）。
// 后续按需注册：productAvailability / productLivePrice / searchResults 等。
// 处理器只实现固定运行时协议 + 返回 HTML 片段，不读 Page Document、
// 不执行 Jet、不解释 Binding。

import (
	"context"
	"html"
)

func init() {
	// loginPanel：登录面板片段（anonymous，未登录访客可见）。
	Register(Spec{
		Type:   "loginPanel",
		Method: "GET",
		Auth:   AuthAnonymous,
		Render: renderLoginPanel,
	})
	// cartSummary：购物车计数片段（session，需登录）。
	Register(Spec{
		Type:   "cartSummary",
		Method: "GET",
		Auth:   AuthSession,
		Render: renderCartSummary,
	})
}

// renderLoginPanel 登录面板：提示文案（用户数据经 escape）。
func renderLoginPanel(_ context.Context, r *Request) (string, error) {
	// 语义上下文：visitorSession 时显示会话态文案（MVP：统一提示）。
	label := "登录 / 注册"
	if r.Context == "visitorSession" {
		label = "继续购物"
	}
	return `<div class="wp-fragment-login-panel" data-fragment="loginPanel">` +
		`<a href="/admin/login" rel="nofollow">` + html.EscapeString(label) + `</a>` +
		`</div>`, nil
}

// renderCartSummary 购物车计数（session；购物车数据模块后续接入，MVP 占位 0）。
func renderCartSummary(_ context.Context, r *Request) (string, error) {
	// MVP：购物车数据模块未落地，返回占位计数 0（结构真实，数据占位）。
	return `<span class="wp-fragment-cart-count" data-fragment="cartSummary">` +
		html.EscapeString("0") + `</span>`, nil
}
