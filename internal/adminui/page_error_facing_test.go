package adminui

import (
	"errors"
	"fmt"
	"testing"

	"go_wp/pkg/facing"
)

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

func TestBusinessErrFactStillRejectsPlainError(t *testing.T) {
	if _, ok := BusinessErrFact(errors.New("dial tcp 127.0.0.1:5432: connection refused")); ok {
		t.Errorf("数据库原文不该被当成业务错误透出")
	}
}
