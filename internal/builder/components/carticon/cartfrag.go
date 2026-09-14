package carticon

// cartfrag.go — 购物车片段的**基础样式**（片段内容用，随组件产物一起进页面）。
//
// 为什么由组件注入，而不是让页面作者自己写：
//
//   这份 HTML 是**运行时片段**渲染出来的 —— 页面作者在编辑器里看不到它，也很难针对它
//   调样式（不像构建期烘进产物的组件，类名与结构都摆在产物 DOM 里）。
//   「购物车片段带语义结构 + 类名，样式交给主题」这条口径在片段层是行不通的：
//   实际表现是「点开购物车是一列裸 HTML」，用户会直接当成坏了。
//
// 所以给一套**克制**的默认：只做布局与层次（对齐、间距、可点区域、分隔线），
// 颜色一律走主题 token（--sky-c-primary / --sky-c-border / --sky-c-text-muted，带兜底），
// 不带动画、不带定位 —— 浮层怎么摆由容器（core.cartIcon）决定，
// 这里只管「车里那一列长什么样」。
//
// 幂等：CSSBuckets.Add 按 (断点 + 规则) 去重，加购按钮与购物车图标同时用也只出一份。

import "go_wp/internal/builder/core"

// AddCartFragmentCSS 注入购物车片段的基础样式。
//
// 由 core.cartIcon 与 core.addToCart 两个组件调用 —— 它们是最常见的两个购物车入口，
// 任何一个在场都意味着这一页可能有购物车片段。
func AddCartFragmentCSS(b *core.CSSBuckets) {
	b.Add("", ".sky-cart", []string{
		"display: flex", "flex-direction: column", "gap: 12px", "min-width: 0",
	})
	b.Add("", ".sky-cart-items", []string{
		"display: flex", "flex-direction: column", "gap: 10px",
		"margin: 0", "padding: 0", "list-style: none",
	})
	b.Add("", ".sky-cart-item", []string{
		"display: flex", "flex-wrap: wrap", "align-items: center", "gap: 8px", "min-width: 0",
	})
	// 已下架 / 库存不足的行：整体压暗，但**不隐藏** —— 访客要看得见为什么结算不了。
	b.Add("", ".sky-cart-item.is-missing", []string{"opacity: 0.65"})
	b.Add("", ".sky-cart-name", []string{
		"flex: 1 1 auto", "min-width: 0", "overflow-wrap: anywhere",
	})
	b.Add("", ".sky-cart-variant", []string{
		"display: block", "font-size: 0.8rem", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add("", ".sky-cart-unit", []string{
		"white-space: nowrap", "color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add("", ".sky-cart-qty", []string{
		"display: inline-flex", "align-items: center", "gap: 6px", "margin: 0",
	})
	b.Add("", ".sky-cart-qty-input", []string{
		"width: min(100%, 72px)", "min-height: 40px", "padding: 6px 8px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: var(--sky-c-surface, #fff)", "color: inherit", "font: inherit",
		"text-align: center",
	})
	b.Add("", ".sky-cart-qty-submit", []string{
		"min-height: 40px", "padding: 6px 12px",
		"border: 1px solid var(--sky-c-border, #d1d5db)", "border-radius: 8px",
		"background: transparent", "color: inherit", "font: inherit", "cursor: pointer",
	})
	b.Add("", ".sky-cart-line", []string{"white-space: nowrap", "font-weight: 600"})
	b.Add("", ".sky-cart-note", []string{
		"margin: 0", "flex-basis: 100%", "font-size: 0.8rem",
		"color: var(--sky-c-text-muted, #6b7280)",
	})
	b.Add("", ".sky-cart-total", []string{
		"display: flex", "justify-content: space-between", "gap: 12px",
		"margin: 0", "padding-top: 10px",
		"border-top: 1px solid var(--sky-c-border, #e5e7eb)", "font-weight: 600",
	})
	b.Add("", ".sky-cart-checkout", []string{
		"display: inline-flex", "align-items: center", "justify-content: center",
		"min-height: 44px", "padding: 10px 18px", "border-radius: 8px",
		"background: var(--sky-c-primary, #2563eb)", "color: #fff",
		"font-weight: 600", "text-decoration: none",
	})
	b.Add("", ".sky-cart-clear", []string{"margin: 0", "align-self: flex-start"})
	b.Add("", ".sky-cart-clear button", []string{
		"padding: 4px 0", "border: 0", "background: none",
		"color: var(--sky-c-text-muted, #6b7280)", "font: inherit", "font-size: 0.85rem",
		"text-decoration: underline", "cursor: pointer",
	})
	b.Add("", ".sky-cart-empty", []string{
		"margin: 0", "color: var(--sky-c-text-muted, #6b7280)",
	})
}
