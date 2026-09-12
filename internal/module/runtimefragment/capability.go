package runtimefragment

// capability.go — 内置运行时片段能力（0-D 首批，docs/04 §1.1）。
// 后续按需注册：productAvailability / productLivePrice / searchResults 等。
// 处理器只实现固定运行时协议 + 返回 HTML 片段，不读 Page Document、
// 不执行 Jet、不解释 Binding。

import (
	"context"

	"go_wp/internal/templates"
)

func init() {
	// loginPanel：登录面板片段（anonymous，未登录访客可见）。
	Register(Spec{
		Type:   "loginPanel",
		Method: "GET",
		Auth:   AuthAnonymous,
		Render: renderLoginPanel,
	})
	// cartSummary 与购物车 / 结算能力已迁到 cart.go —— 它们要读客户端签名 cookie，
	// 而 cookie 名与购物车逻辑都归那里。留在本文件会造成**两处注册同一类型**，
	// 后注册的覆盖先注册的，而 init 顺序不保证：那是一个只在特定构建顺序下复现的 bug。
}

// renderLoginPanel 登录面板：提示文案（Jet 模板渲染，用户数据默认转义）。
func renderLoginPanel(_ context.Context, r *Request) (string, error) {
	// 语义上下文：visitorSession 时显示会话态文案（MVP：统一提示）。
	label := "登录 / 注册"
	if r.Context == "visitorSession" {
		label = "继续购物"
	}
	return templates.RenderFragment("login_panel", struct{ Label string }{Label: label})
}
