package productservice

import (
	"context"
)

// product_translate.go — service 层取词函数的传递通道（后台展示文案专用）。
//
// 背景：本模块的 service 要产出**展示文案**（内置定价 / 标签规则的展示名与描述、
// 筛选条件与状态的可读标签），而它不是纯数据 —— 英文界面上必须显示英文。
// service 层没有语言上下文（语言来自后台 Cookie / Accept-Language，只有 gin.Context
// 知道），因此取词函数只能由 inbound 传进来。
//
// 为什么用 ctx 而不是给每个方法加参数：
//   - 这些文案散布在多层内部函数里（pricingStatusLabel → setPricingLineStatus →
//     computePricingLines → PreviewPricing），逐层加到 contract 方法签名会把
//     `tr func(...)` 写进一堆与展示无关的接口（预览、应用、留痕列表、抽屉数据…）；
//   - 传播方向是单向的（inbound → service → 内部函数），与 ctx 的语义一致；
//   - 未注入时的行为是**返回中文兜底**（不是裸 key），与 i18n 未初始化时的降级一致，
//     所以漏注入不会把页面打坏，只是不翻译。
//
// 写入侧只有一处：inbound 的 productTranslateMiddleware（路由组中间件），
// 它把 shell.TranslateFor(c) 放进 Request 的 Context —— handler 里
// `ctx := c.Request.Context()` 拿到的就是带取词函数的 ctx，无需逐调用点改动。
type translateCtxKey struct{}

// TranslateFunc 取词函数签名（与 shell.TranslateFor(c) 返回的函数同型）。
type TranslateFunc func(key, fallback string) string

// WithTranslate 把取词函数放进 ctx（inbound 中间件调用）。
//
// tr 为 nil 时原样返回 ctx（不写入坏值）。
func WithTranslate(ctx context.Context, tr TranslateFunc) context.Context {
	if ctx == nil || tr == nil {
		return ctx
	}
	return context.WithValue(ctx, translateCtxKey{}, tr)
}

// translateFrom 取当前请求的取词函数；未注入时返回**恒等函数**。
//
// 恒等函数的语义与 pkg/i18n.Translate 在词条缺失时一致：返回调用点给的中文兜底
// （兜底为空才退回 key）—— service 的每条文案都在调用点带中文兜底，因此未注入时
// 页面显示的就是中文原文，而不是 `admin.product_pricing.rule.costMultiple.name`
// 这种裸 key。
func translateFrom(ctx context.Context) TranslateFunc {
	if ctx != nil {
		if tr, ok := ctx.Value(translateCtxKey{}).(TranslateFunc); ok && tr != nil {
			return tr
		}
	}
	return func(key, fallback string) string {
		if fallback != "" {
			return fallback
		}
		return key
	}
}
