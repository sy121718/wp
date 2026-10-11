package adminui

// page_error_facing_test.go — 钉「错误自带对外文案」这一档。
//
// 背景：模块的「sentinel → 中文兜底」映射原先写在各自 inbound/http 的 switch 里
// （page 的 pageLangErrMsg、navigation 的 navigationErrMsg …）。那让「加一个业务错误」
// 要改两个包，而第二个包（http 层）在 service 里看不见 —— 二开的人加完 sentinel，
// 页面上显示的是裸 key，还不知道该去哪补那句话。
//
// 现在兜底跟着错误声明走（pkg/facing.New），出口只认 `Facing() (key, fallback)` 这个形状。
//
// 这三条是会变红的判据：
//   - 去掉 BusinessErrFact 里的 facing 分支 → 第 ① ② 条红（退回「fallback == key」）；
//   - 让 Facing 的 fallback 被忽略（只取 key）→ 第 ① 条红；
//   - 复合形态不再走 errors.As（改成只看外层 err 的形状）→ 第 ② 条红。

import (
	"errors"
	"fmt"
	"testing"

	"go_wp/pkg/facing"
)

// ①自带文案的错误：fallback 必须来自声明处那句话，不能退化成 key 字面量。
func TestBusinessErrFactTakesFacingFallback(t *testing.T) {
	const (
		key      = "page.err.facing_demo"
		fallback = "这句话来自错误声明处（不是 key 字面量）"
	)
	fact, ok := BusinessErrFact(facing.New(key, fallback))
	if !ok {
		t.Fatalf("自带文案的错误应命中业务判据（否则页面只会给归口文案）")
	}
	if fact.Key != key {
		t.Errorf("Key = %q，want %q", fact.Key, key)
	}
	if fact.Fallback != fallback {
		t.Errorf("Fallback = %q，want %q —— 兜底没跟着错误走，页面会显示 key 字面量", fact.Fallback, fallback)
	}
	if !fact.Business {
		t.Errorf("Business = false，want true")
	}
}

// ②复合形态：`fmt.Errorf("%w: 明细")` 包装后仍取到自带文案，且明细接回 Tail
// （明细丢了的表现是「解析失败」后面没有「第 3 行缺右括号」，用户无从下手）。
func TestBusinessErrFactKeepsDetailTailForFacingError(t *testing.T) {
	err := fmt.Errorf("%w: 第 3 行缺右括号", facing.New("page.err.facing_demo", "文档解析失败"))
	fact, ok := BusinessErrFact(err)
	if !ok {
		t.Fatalf("复合形态应命中业务判据")
	}
	if fact.Key != "page.err.facing_demo" {
		t.Errorf("Key = %q，want %q", fact.Key, "page.err.facing_demo")
	}
	if fact.Fallback != "文档解析失败" {
		t.Errorf("Fallback = %q，want %q", fact.Fallback, "文档解析失败")
	}
	if fact.Tail != "第 3 行缺右括号" {
		t.Errorf("Tail = %q，want %q —— 明细段被丢掉了", fact.Tail, "第 3 行缺右括号")
	}
}

// ③声明处不给兜底时回落成 key —— 兜底为空会让失败在页面上完全不可见
// （用户以为操作成功了），这比显示 key 更糟，所以 facing.New 必须挡住空串。
func TestFacingNewFallsBackToKeyOnEmptyText(t *testing.T) {
	err := facing.New("page.err.facing_no_text", "")
	if err.Error() != "page.err.facing_no_text" {
		t.Errorf("Error() = %q，want key（哨兵形态：整条消息就是 key）", err.Error())
	}
	fact, ok := BusinessErrFact(err)
	if !ok {
		t.Fatalf("应命中业务判据")
	}
	if fact.Fallback == "" {
		t.Errorf("Fallback 为空 —— 页面上这次失败完全不可见")
	}
}

// ④非 facing 的普通错误不受影响：仍走原来的形态判据与归口（这条是防回归的护栏，
// 保证新增分支没有把「未命中」那一档也吞掉）。
func TestBusinessErrFactStillRejectsPlainError(t *testing.T) {
	if _, ok := BusinessErrFact(errors.New("dial tcp 127.0.0.1:5432: connection refused")); ok {
		t.Errorf("数据库原文不该被当成业务错误透出")
	}
}
