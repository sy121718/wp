package builder

// fragment_base.go 保留尚未迁移的订单片段样式清单，以及 JSON 购物车静态外壳的基座样式。
// 购物车按 data-site-cart 选取样式，订单片段仍按能力名选取；两者均只控制布局和主题令牌。
// 账号等仍依赖组件节点作用域的样式不进入全局基座，对表测试钉住每个遗留能力的归属。

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
		{id: "orders", caps: ordersFragmentCapabilities(), css: ordersFragmentBaseCSS()},
	}
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
		"commentList":   "评论列表 + 提交表单：.sky-comment-* 为语义钩子，当前无布局样式；宽窄由放置它的容器组件决定",
		"commentSubmit": "评论提交结果只输出一句人话（.sky-comment-result 无布局诉求），当前无样式定义",
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

// cartFragmentBaseCSS JSON 购物车静态外壳的基座样式。
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
