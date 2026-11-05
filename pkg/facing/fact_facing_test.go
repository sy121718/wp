package facing_test

import (
	"errors"
	"fmt"
	"testing"

	"go_wp/pkg/facing"

)

// 错误声明只给 key（AGENTS.md「业务错误在声明处只给 key + 状态码」），
// 文案只在 JSON 出口由词库取；这里守的是「key 能被认成业务错误、明细不丢」这条事实链。
func TestBusinessErrFactReadsKeyOnlyFacing(t *testing.T) {
	const key = "page.err.facing_demo"

	fact, ok := facing.FactOf(facing.New(key))
	if !ok {
		t.Fatalf("key-only 错误应命中业务判据（否则页面只会给归口文案）")
	}
	if fact.Key != key {
		t.Errorf("Key = %q，want %q", fact.Key, key)
	}
	if fact.Fallback == key {
		t.Errorf("Fallback = %q —— 等于 key 就等于把裸 key 交给前端", fact.Fallback)
	}
	if !fact.Business {
		t.Errorf("Business = false，want true")
	}
}

func TestBusinessErrFactKeepsDetailTailForFacingError(t *testing.T) {
	const key = "page.err.facing_demo"

	err := fmt.Errorf("%w: 第 3 行缺右括号", facing.New(key))
	fact, ok := facing.FactOf(err)
	if !ok {
		t.Fatalf("复合形态应命中业务判据")
	}
	if fact.Key != key {
		t.Errorf("Key = %q，want %q", fact.Key, key)
	}
	if fact.Tail != "第 3 行缺右括号" {
		t.Errorf("Tail = %q，want %q —— 明细段被丢掉了", fact.Tail, "第 3 行缺右括号")
	}
}

// 缺词只在 JSON 出口兜成归口文案；页面侧的事实对象不自己编文案，
// 但也不能把 key 当兜底顶上去。
func TestFacingNewKeepsEmptyFallback(t *testing.T) {
	err := facing.New("page.err.facing_no_text")
	if err.Error() != "page.err.facing_no_text" {
		t.Errorf("Error() = %q，want key（哨兵形态：整条消息就是 key）", err.Error())
	}
	key, fallback := err.Facing()
	if key != "page.err.facing_no_text" {
		t.Errorf("Facing() key = %q", key)
	}
	if fallback != "" {
		t.Errorf("Facing() fallback = %q，want 空 —— 兜底句归词库与出口，不在声明处", fallback)
	}
	fact, ok := facing.FactOf(err)
	if !ok {
		t.Fatalf("应命中业务判据")
	}
	if fact.Fallback == fact.Key {
		t.Errorf("Fallback 退成了 key 字面量，前端会看到内部 key")
	}
}

func TestBusinessErrFactStillRejectsPlainError(t *testing.T) {
	if _, ok := facing.FactOf(errors.New("dial tcp 127.0.0.1:5432: connection refused")); ok {
		t.Errorf("数据库原文不该被当成业务错误透出")
	}
}
