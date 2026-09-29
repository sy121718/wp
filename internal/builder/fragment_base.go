package builder

// fragment_base.go — 运行时片段的**基座样式**（审计 UIK-003 第二层 / UIK-005）。
//
// 为什么片段样式归基座、而不是归组件：
//   片段 HTML 是运行时渲染的（/_fragments/<capability>），页面作者在编辑器里看不到它，
//   也很难针对它调样式。此前这份样式由 core.cartIcon / core.addToCart / core.orderList
//   在构建期调 AddCartFragmentCSS / AddOrdersFragmentCSS 注入 —— 于是「片段有没有样式」
//   取决于「页面上有没有放那几个组件」：只放一个自定义按钮（作者自己写
//   hx-post="/_fragments/cartAdd"）的页面，购物车片段刷新出来就是一列裸 HTML。
//   现在改成按**页面里是否存在指向 /_fragments/ 的 htmx 请求**判定（fragment_caps.go），
//   任何方式引用片段都能拿到样式；组件那边不再负责片段样式。
//
// 为什么按**族**切分，而不是一份大表：
//   体积。只用购物车片段的页面不该带上订单片段那份 CSS（UIK-005 的 verification 第二条）。
//   族 = 「一份片段模板渲染出来的内容」：cartView / cartAdd / cartSetQty / cartClear /
//   checkout 渲染的都是 cart_view.jet（同一批 .sky-cart-* 类），订单侧同理。
//
// 样式口径（与迁移前逐字一致，产物视觉零变化）：只做布局与层次（对齐、间距、可点区域、
// 分隔线），颜色一律走主题令牌（带兜底），不带动画、不带定位 —— 浮层怎么摆由容器组件决定。
// 站点主题仍可覆盖这些全局类（它们是保留命名空间 sky- 下的基座类）。
//
// 新增片段时照这条判据分派（两层各有归属，别混）：
//   · 片段的样式**由片段模板自己消费**（类名写在 fragments/*.jet 里、且不依赖页面上的
//     组件作用域）→ 归基座，在这里建/扩展一个族，并把能力名登记进族的 caps；
//   · 片段插在某个组件容器内、类名依赖该组件的作用域（.sky-c-{id} 下的 .sky-user-*
//     就是这种）→ 属**组件私有层**，不进基座 —— 进了会与「一组件一作用域」的隔离规则打架。
//
// 未归基座的能力必须显式登记（fragmentUnstyledCapabilities，含现状说明）：漏登记会让
// 「新片段有没有样式」变成没人能回答的问题，而这正是本条目要消灭的那类隐式耦合。
// 两个方向的清单都由测试钉住：builder 侧 TestFragmentCapabilityTablesCoverEachOther，
// runtimefragment 侧 TestFragmentStyleTablesCoverRegistry（对表 runtimefragment.Types()）。

import (
	"sort"
	"strings"

	"go_wp/internal/builder/core"
)

// fragmentStyleGroup 一族片段基座样式。
type fragmentStyleGroup struct {
	// ID 族标识（日志与测试用）。
	id string
	// caps 该族覆盖的片段能力名（与 runtimefragment 注册表同名，小写比较）。
	caps []string
	// css 该族的 CSS 文本（编译期生成一次，见 fragmentGroupCSS 的调用点）。
	css string
}

// fragmentStyleGroups 全部片段基座样式族（顺序即产物里的拼接顺序）。
func fragmentStyleGroups() []fragmentStyleGroup {
	return []fragmentStyleGroup{
		{id: "cart", caps: cartFragmentCapabilities(), css: cartFragmentBaseCSS()},
		{id: "orders", caps: ordersFragmentCapabilities(), css: ordersFragmentBaseCSS()},
	}
}

// cartFragmentCapabilities 购物车族的片段能力。
func cartFragmentCapabilities() []string {
	return []string{"cartSummary", "cartView", "cartAdd", "cartSetQty", "cartClear", "checkout"}
}

// ordersFragmentCapabilities 订单族的片段能力（含退货申请：它渲染在同一份订单片段里）。
func ordersFragmentCapabilities() []string {
	return []string{"ordersList", "orderDetail", "returnRequest"}
}

// fragmentUnstyledCapabilities 已知**暂无**片段基座样式的能力 → 现状说明。
//
// 说明写的是实测事实（这些类名在全仓库没有任何样式定义），不是「不需要」的托词：
// 它们是「片段裸 HTML」问题的剩余部分，归属与整改需要单独判断（有的该进基座族、
// 有的该走组件私有层、有的只是纯文本）。登记在这里的价值是让「哪些片段还没有样式」
// 一眼可查，而不是让下一个人重新 grep 一遍。
func fragmentUnstyledCapabilities() map[string]string {
	return map[string]string{
		"loginPanel":                 "登录面板只输出一段提示文本（.sky-fragment-login-panel 无布局诉求），当前无样式定义",
		"loginForm":                  "账号表单片段用 .sky-user-* 类，插在 core.userForms 容器内、依赖它的节点作用域，样式由 userforms 组件私有层提供",
		"registerForm":               "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"forgotForm":                 "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"resetForm":                  "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"accountPanel":               "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"accountProfileForm":         "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"accountPreferenceForm":      "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"accountPasswordForm":        "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"accountSessionsPanel":       "同 loginForm：账号表单片段走 userforms 的节点作用域，属组件私有层",
		"productList":                "商品列表片段由 core.productList 组件用同一个 node id 渲染（类名与静态产物同源），样式来自组件私有层",
		"searchResults":              "搜索结果片段的 .sky-search-* 当前没有任何样式定义（遗留的裸 HTML），整改归属待定",
		"productLivePrice":           "实时价格片段只输出一个价格文本（.sky-live-price 无布局诉求），当前无样式定义",
		"productVariantAvailability": "实时库存片段只输出一句库存文案（.sky-variant-stock-msg 无布局诉求），当前无样式定义",
		// 会员身份两个能力（BIZ-3）：模板只输出等级名 / 权益短句 / 一句降级文案，
		// .sky-membership-* 是保留命名空间下的**语义钩子**（站点主题可覆写），
		// 基座里刻意不给布局 —— 徽标与面板的宽窄由放置它的容器决定，
		// 进基座会与「容器决定浮层怎么摆」的分工打架。
		"membershipBadge": "会员角标只输出一个等级名或一句降级文案（.sky-membership-tier 无布局诉求），归属待定：需要主题级排版时再按族建基座族",
		"membershipPanel": "会员面板输出等级 + 权益短句（.sky-membership-* 为语义钩子），当前无布局样式；宽窄由放置它的容器组件决定",
		// 评论两个能力（BIZ-5）：模板输出列表 / 表单 / 一句降级文案，
		// .sky-comment-* 同样是保留命名空间下的**语义钩子**（站点主题可覆写），
		// 基座刻意不给布局 —— 列表密度与表单宽窄由放置它的容器组件决定，
		// 进基座会与「容器决定排版」的分工打架（与会员片段同一取舍）。
		"commentList":             "评论列表 + 提交表单：.sky-comment-* 为语义钩子，当前无布局样式；宽窄由放置它的容器组件决定",
		"commentSubmit":           "评论提交结果只输出一句人话（.sky-comment-result 无布局诉求），当前无样式定义",
		"bundleConfigurator":      "捆绑配置器内嵌在后台工作台页面里（用后台的 workbench.css），不进访问产物",
		"bundleConfiguratorCheck": "同 bundleConfigurator：整单校验结果内嵌在后台工作台，样式来自 workbench.css",
	}
}

// FragmentStyleCapabilities 有基座样式族的片段能力（供 runtimefragment 侧对表测试）。
func FragmentStyleCapabilities() []string {
	var out []string
	for _, g := range fragmentStyleGroups() {
		out = append(out, g.caps...)
	}
	sort.Strings(out)
	return out
}

// FragmentUnstyledCapabilities 暂无基座样式的片段能力 → 现状说明。
func FragmentUnstyledCapabilities() map[string]string {
	in := fragmentUnstyledCapabilities()
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// fragmentBaseCSSFor 按页面引用到的片段能力选出基座样式（未命中任何族时返回空串）。
func fragmentBaseCSSFor(caps htmlFeatures) string {
	if len(caps) == 0 {
		return ""
	}
	parts := make([]string, 0, 2)
	for _, g := range fragmentStyleGroups() {
		if !g.matches(caps) {
			continue
		}
		if strings.TrimSpace(g.css) != "" {
			parts = append(parts, g.css)
		}
	}
	return strings.Join(parts, "\n\n")
}

// matches 该族是否被页面引用的能力命中。
func (g fragmentStyleGroup) matches(caps htmlFeatures) bool {
	for _, c := range g.caps {
		if _, ok := caps[strings.ToLower(c)]; ok {
			return true
		}
	}
	return false
}

// ===== 购物车族 =====

// cartFragmentBaseCSS 购物车片段的基座样式（自 components/carticon/cartfrag.go 迁移）。
func cartFragmentBaseCSS() string {
	b := core.NewCSSBuckets()
	b.Add(core.BreakpointDesktop, ".sky-cart", []string{
		"display: flex", "flex-direction: column", "gap: 12px", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-items", []string{
		"display: flex", "flex-direction: column", "gap: 10px",
		"margin: 0", "padding: 0", "list-style: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-item", []string{
		"display: flex", "flex-wrap: wrap", "align-items: center", "gap: 8px", "min-width: 0",
	})
	// 已下架 / 库存不足的行：整体压暗，但**不隐藏** —— 访客要看得见为什么结算不了。
	b.Add(core.BreakpointDesktop, ".sky-cart-item.is-missing", []string{"opacity: 0.65"})
	b.Add(core.BreakpointDesktop, ".sky-cart-name", []string{
		"flex: 1 1 auto", "min-width: 0", "overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-variant", []string{
		"display: block", "font-size: 0.8rem", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-unit", []string{
		"white-space: nowrap", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-qty", []string{
		"display: inline-flex", "align-items: center", "gap: 6px", "margin: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-qty-input", []string{
		"width: min(100%, 72px)", "min-height: 40px", "padding: 6px 8px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: var(--sky-c-surface, #fff)", "color: inherit", "font: inherit",
		"text-align: center",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-qty-submit", []string{
		"min-height: 40px", "padding: 6px 12px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: transparent", "color: inherit", "font: inherit", "cursor: pointer",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-line", []string{"white-space: nowrap", "font-weight: 600"})
	b.Add(core.BreakpointDesktop, ".sky-cart-note", []string{
		"margin: 0", "flex-basis: 100%", "font-size: 0.8rem",
		"color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-total", []string{
		"display: flex", "justify-content: space-between", "gap: 12px",
		"margin: 0", "padding-top: 10px",
		"border-top: 1px solid var(--sky-c-border, #e5e7eb)", "font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-checkout", []string{
		"display: inline-flex", "align-items: center", "justify-content: center",
		"min-height: 44px", "padding: 10px 18px", "border-radius: 8px",
		"background: var(--sky-c-primary, #2563eb)", "color: #fff",
		"font-weight: 600", "text-decoration: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-clear", []string{"margin: 0", "align-self: flex-start"})
	b.Add(core.BreakpointDesktop, ".sky-cart-clear button", []string{
		"padding: 4px 0", "border: 0", "background: none",
		"color: var(--sky-c-text-muted, #6b7280)", "font: inherit", "font-size: 0.85rem",
		"text-decoration: underline", "cursor: pointer",
	})
	b.Add(core.BreakpointDesktop, ".sky-cart-empty", []string{
		"margin: 0", "color: var(--sky-c-text-muted, #6b7280)",
	})
	return b.String()
}

// ===== 订单族 =====

// ordersFragmentBaseCSS 订单片段的基座样式（自 components/orderlist/orderfrag.go 迁移）。
//
// 多端：订单行在窄屏折行（flex-wrap + min-width: 0），不写死宽度；
// 移动断点把金额与时间挪到第二行，避免横向滚动条。
func ordersFragmentBaseCSS() string {
	b := core.NewCSSBuckets()
	b.Add(core.BreakpointDesktop, ".sky-orders", []string{
		"display: flex", "flex-direction: column", "gap: 12px", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-notice", []string{
		"margin: 0", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-action", []string{
		"display: inline-flex", "align-items: center", "min-height: 40px",
		"align-self: flex-start", "color: var(--sky-c-primary, #2563eb)",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-tabs", []string{
		"display: flex", "flex-wrap: wrap", "gap: 8px",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-tab", []string{
		"padding: 6px 12px", "border-radius: 999px",
		"border: 1px solid var(--sky-c-border, #d1d5db)",
		"color: inherit", "text-decoration: none", "white-space: nowrap",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-tab.is-active", []string{
		"border-color: var(--sky-c-primary, #2563eb)",
		"color: var(--sky-c-primary, #2563eb)", "font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-list", []string{
		"display: flex", "flex-direction: column", "gap: 12px",
		"margin: 0", "padding: 0", "list-style: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-item", []string{
		"display: flex", "flex-direction: column", "gap: 8px",
		"padding: 12px", "min-width: 0",
		"border: 1px solid var(--sky-c-border, #e5e7eb)", "border-radius: 10px",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-head", []string{
		"display: flex", "flex-wrap: wrap", "align-items: baseline",
		"gap: 8px 14px", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-no", []string{
		"font-weight: 600", "overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-status", []string{
		"padding: 2px 8px", "border-radius: 999px", "font-size: 0.85rem",
		"background: var(--sky-c-surface-muted, #f3f4f6)",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-total", []string{
		"margin-left: auto", "font-weight: 600", "white-space: nowrap",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-time", []string{
		"color: var(--sky-c-text-muted, #6b7280)", "font-size: 0.85rem",
		"white-space: nowrap",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-toggle", []string{
		"align-self: flex-start", "min-height: 40px", "padding: 6px 14px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: transparent", "color: inherit", "font: inherit", "cursor: pointer",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-detail:empty", []string{"display: none"})
	b.Add(core.BreakpointDesktop, ".sky-orders-pager", []string{
		"display: flex", "flex-wrap: wrap", "align-items: center", "gap: 12px",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-page", []string{
		"display: inline-flex", "align-items: center", "min-height: 40px",
	})
	b.Add(core.BreakpointDesktop, ".sky-orders-count", []string{
		"color: var(--sky-c-text-muted, #6b7280)", "font-size: 0.85rem",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-meta", []string{
		"display: flex", "flex-wrap: wrap", "align-items: baseline", "gap: 8px 14px",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-lines", []string{
		"display: flex", "flex-direction: column", "gap: 8px",
		"margin: 8px 0", "padding: 0", "list-style: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-line", []string{
		"display: flex", "flex-wrap: wrap", "align-items: baseline", "gap: 8px 12px",
		"min-width: 0", "padding-bottom: 8px",
		"border-bottom: 1px dashed var(--sky-c-border, #e5e7eb)",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-line-name", []string{
		"flex: 1 1 auto", "min-width: 0", "overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-line-variant", []string{
		"display: block", "font-size: 0.8rem", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-line-sku", []string{
		"font-size: 0.8rem", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-line-price", []string{
		"white-space: nowrap", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-line-total", []string{
		"white-space: nowrap", "font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-sum", []string{
		"display: flex", "flex-wrap: wrap", "justify-content: space-between",
		"gap: 8px 14px", "margin: 0", "font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-pay", []string{
		"margin: 0", "font-size: 0.9rem", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-address", []string{
		"margin: 0", "font-size: 0.9rem", "color: var(--sky-c-text-muted, #6b7280)",
		"overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-remark", []string{
		"margin: 0", "font-size: 0.9rem", "color: var(--sky-c-text-muted, #6b7280)",
		"overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-logs", []string{
		"display: flex", "flex-direction: column", "gap: 6px",
		"margin: 8px 0 0", "padding-left: 18px",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-log", []string{
		"display: flex", "flex-wrap: wrap", "gap: 6px 12px", "font-size: 0.85rem",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-log-time", []string{
		"color: var(--sky-c-text-muted, #6b7280)", "white-space: nowrap",
	})
	b.Add(core.BreakpointDesktop, ".sky-order-log-remark", []string{
		"color: var(--sky-c-text-muted, #6b7280)",
	})
	// 退货区（BIZ-1 退货入库）：已有申请列表 + 申请表单 + 提交结果。
	b.Add(core.BreakpointDesktop, ".sky-returns", []string{
		"display: flex", "flex-direction: column", "gap: 8px", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-returns-title", []string{"margin: 0", "font-weight: 600"})
	b.Add(core.BreakpointDesktop, ".sky-returns-list", []string{
		"display: flex", "flex-direction: column", "gap: 6px",
		"margin: 0", "padding: 0", "list-style: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-item", []string{
		"display: flex", "flex-wrap: wrap", "align-items: baseline", "gap: 8px 12px", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-no", []string{
		"font-weight: 600", "overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-reason", []string{
		"color: var(--sky-c-text-muted, #6b7280)", "font-size: 0.85rem", "overflow-wrap: anywhere",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-form", []string{
		"display: flex", "flex-direction: column", "gap: 10px", "min-width: 0",
		"padding-top: 12px", "border-top: 1px solid var(--sky-c-border, #e5e7eb)",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-lines", []string{
		"display: flex", "flex-direction: column", "gap: 8px",
		"margin: 0", "padding: 0", "list-style: none",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-line", []string{
		"display: flex", "flex-wrap: wrap", "align-items: center", "gap: 8px 12px", "min-width: 0",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-qty", []string{
		"width: min(100%, 96px)", "min-height: 40px", "padding: 6px 8px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: var(--sky-c-surface, #fff)", "color: inherit", "font: inherit",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-label", []string{
		"display: flex", "flex-direction: column", "gap: 6px", "font-size: 0.9rem",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-reason-input", []string{
		"min-height: 64px", "padding: 8px 10px", "font: inherit",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: var(--sky-c-surface, #fff)", "color: inherit",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-submit", []string{
		"align-self: flex-start", "min-height: 44px", "padding: 10px 18px",
		"border: 0", "border-radius: 8px", "cursor: pointer",
		"background: var(--sky-c-primary, #2563eb)", "color: #fff", "font: inherit", "font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-ok", []string{
		"margin: 0", "padding: 8px 12px", "border-radius: 8px",
		"background: var(--sky-c-surface-muted, #f3f4f6)", "font-weight: 600",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-result", []string{
		"display: flex", "flex-direction: column", "gap: 6px", "padding: 12px",
		"border: 1px solid var(--sky-c-border, #e5e7eb)", "border-radius: 10px",
	})
	b.Add(core.BreakpointDesktop, ".sky-return-title", []string{"margin: 0", "font-weight: 600"})
	b.Add(core.BreakpointDesktop, ".sky-return-note", []string{
		"margin: 0", "color: var(--sky-c-text-muted, #6b7280)", "font-size: 0.9rem",
	})
	// 窄屏：金额与时间挪到第二行（首行只留订单号与状态），避免挤压成一列两三个字。
	b.Add(core.BreakpointMobile, ".sky-orders-total", []string{"margin-left: 0"})
	b.Add(core.BreakpointMobile, ".sky-orders-tab", []string{"padding: 6px 10px"})
	return b.String()
}
