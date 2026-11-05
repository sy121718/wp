package contentenums

import (
	"errors"
	"testing"

	"go_wp/pkg/facing"
)

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrNotFound)
	if err.Error() != ErrNotFound {
		t.Fatalf("Error() = %q，应为枚举 key", err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok {
		t.Fatalf("%q 没有被判成业务事实", ErrNotFound)
	}
	// enums 只声明 key + 状态码：FactOf 走 Source 接口拿到 (key, "")，
	// 所以兜底必须是空串 —— 内嵌任何中文都说明文案又回到了声明处。
	if fact.Key != ErrNotFound {
		t.Errorf("Key = %q，want %q", fact.Key, ErrNotFound)
	}
	if fact.Fallback != "" {
		t.Errorf("%q 内嵌了兜底文案 %q —— enums 只声明 key，文案归词库", ErrNotFound, fact.Fallback)
	}
	if !fact.Business {
		t.Errorf("%q 应标记为业务事实", ErrNotFound)
	}
}

func TestErrDetailKeepsTail(t *testing.T) {
	fact, ok := facing.FactOf(ErrDetail(ErrInvalidField, "`body`"))
	if !ok || !fact.Business || fact.Key != ErrInvalidField {
		t.Fatalf("带明细的业务错误应判为业务事实，得到 %+v / %v", fact, ok)
	}
	if fact.Tail != "`body`" {
		t.Errorf("Tail = %q，应为定位信息", fact.Tail)
	}
	if got := ErrDetail(ErrInvalidField, "  ").Error(); got != ErrInvalidField {
		t.Errorf("空明细应退化成纯 key，实际 %q", got)
	}

	if _, ok := facing.FactOf(errors.New(`pq: relation "contents" does not exist`)); ok {
		t.Error("内部原文不该被判成业务事实")
	}
}
