package orderlist

// orderfrag.go — 订单片段的基础样式（随组件产物一起进页面）。
//
// 与购物车片段同一口径（components/carticon/cartfrag.go）：这份 HTML 是**运行时片段**
// 渲染出来的，页面作者在编辑器里看不到、也难针对它调样式。
// 「结构给类名、样式交给主题」在片段层行不通 —— 实际表现是一列裸 HTML。
//
// 所以给一套克制的默认：只做布局与层次（对齐、间距、可点区域、分隔线），
// 颜色一律走主题 token（带兜底），不带动画、不带定位。
// 站点主题仍可覆盖这些类名。
//
// 幂等：CSSBuckets.Add 按 (断点 + 规则) 去重，一页放多个订单列表也只出一份。
//
// 多端：订单行在窄屏折行（flex-wrap + min-width: 0），不写死宽度；
// 移动断点把金额与时间挪到第二行，避免横向滚动条。

import "go_wp/internal/builder/core"

// AddOrdersFragmentCSS 注入订单片段的基础样式。
func AddOrdersFragmentCSS(b *core.CSSBuckets) {
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
		"width: 96px", "min-height: 40px", "padding: 6px 8px",
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
}
