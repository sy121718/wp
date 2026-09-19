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
//
// 文案走 r.tr（请求语言取词，词条 site.fragment.login_panel.*，迁移 293）——
// 这里曾是本包**唯一**恒为默认语言的片段文案：多语言站点上端点已按 ?lang 解析出语言，
// 而这两句写死在 Go 里，于是英文站点的登录面板一直显示中文。
// fallback 是中文原文（与 fragment_i18n*.go 同口径）：词条缺失时回退原文，
// 绝不输出裸 key、也不输出空串。
func renderLoginPanel(_ context.Context, r *Request) (string, error) {
	// 语义上下文：visitorSession 时显示会话态文案（MVP：统一提示）。
	label := r.tr("site.fragment.login_panel.login_register", "登录 / 注册")
	if r.Context == "visitorSession" {
		label = r.tr("site.fragment.login_panel.continue_shopping", "继续购物")
	}
	return templates.RenderFragment("login_panel", struct{ Label string }{Label: label})
}
