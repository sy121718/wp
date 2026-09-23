package runtimefragment

// fragment_user_message_test.go — 片段里 cart / order 文案「按 key 取词」的回归。
//
// 根因（2026-09）：cartenums / orderenums 已接 i18n，白名单 UserFacingMessages 的**值就是
// item_key**（order.err.stockInsufficient / cart.err.outOfStock …），而 fragmentUserMessage
// 当时只查 fragmentMessageKeys —— 那是一张「中文常量 → site.fragment.msg.*」的过渡映射表，
// key 形态一条都匹配不上，函数原样返回 msg，于是**访客在片段里看到的是裸 key**。
// 片段模板是直接渲染文本、不经过 pkg/response 的 translate，所以这一层必须自己取词。
//
// 判据：调用方（cartUserMessage / orderUserMessage、orders.go 与 returns.go 的直接调用点）
// 传进来的一定是白名单命中的 key —— 本用例把「按 key 取词」钉住，并把旧的中文过渡形态一并保住。

import (
	"testing"

	cartenums "go_wp/internal/module/cart/enums"
	orderenums "go_wp/internal/module/order/enums"
)

// fakeFragmentT 桩取词函数：记录被查的 (key, fallback)，返回与 key 毫无相似度的「译文」，
// 任何一处没取词都会立刻暴露。
type fakeFragmentT struct {
	calls [][2]string
}

func (f *fakeFragmentT) fn(key, fallback string) string {
	f.calls = append(f.calls, [2]string{key, fallback})
	switch key {
	case orderenums.ErrStockInsufficient:
		return "EN-STOCK-INSUFFICIENT"
	case cartenums.ErrOutOfStock:
		return "EN-OUT-OF-STOCK"
	case "site.fragment.msg.stock_insufficient":
		return "EN-LEGACY-STOCK"
	default:
		return fallback
	}
}

// TestFragmentUserMessageTranslatesEnumsKey key 形态（enums 已接 i18n）必须取词。
func TestFragmentUserMessageTranslatesEnumsKey(t *testing.T) {
	f := &fakeFragmentT{}
	r := &Request{T: f.fn}

	if got := fragmentUserMessage(r, orderenums.ErrStockInsufficient); got != "EN-STOCK-INSUFFICIENT" {
		t.Errorf("order key 形态应取词，实际 %q（等于 key 说明这一层没取词）", got)
	}
	if got := fragmentUserMessage(r, cartenums.ErrOutOfStock); got != "EN-OUT-OF-STOCK" {
		t.Errorf("cart key 形态应取词，实际 %q", got)
	}

	// 查询键与 fallback 都应是 key 本身：缺词条时页面显示 key（一眼可见），不静默吞掉整句。
	if len(f.calls) == 0 ||
		f.calls[0][0] != orderenums.ErrStockInsufficient ||
		f.calls[0][1] != orderenums.ErrStockInsufficient {
		t.Errorf("应以 key 为查询键与 fallback，实际 %v", f.calls)
	}

	// 中文旧形态仍走过渡映射表（行为不变）。
	if got := fragmentUserMessage(r, "库存不足"); got != "EN-LEGACY-STOCK" {
		t.Errorf("中文常量应走过渡映射表，实际 %q", got)
	}
}

// TestFragmentUserMessageKeepsEmptyAndNil 空值路径不取词、原样返回。
func TestFragmentUserMessageKeepsEmptyAndNil(t *testing.T) {
	r := &Request{T: func(string, string) string { return "SHOULD-NOT-BE-CALLED" }}

	if got := fragmentUserMessage(nil, orderenums.ErrStockInsufficient); got != orderenums.ErrStockInsufficient {
		t.Errorf("nil request 应原样返回，实际 %q", got)
	}
	if got := fragmentUserMessage(r, ""); got != "" {
		t.Errorf("空串应原样返回，实际 %q", got)
	}
	// 未装配取词函数（T 为空）时回落 fallback —— 与 r.tr 的既定语义一致。
	if got := fragmentUserMessage(&Request{}, "x"); got != "x" {
		t.Errorf("T 为空时应回落 fallback，实际 %q", got)
	}
}
