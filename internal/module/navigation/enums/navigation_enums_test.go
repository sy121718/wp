package navigationenums

import (
	"errors"
	"testing"

	"go_wp/pkg/facing"
)

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrStaleVersion)
	if err.Error() != ErrStaleVersion {
		t.Fatalf("Error() = %q，应为枚举 key", err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("FactOf 应判为业务错误，得到 %+v / %v", fact, ok)
	}
	// enums 只声明 key + 状态码，中文归词库；声明处不再带成品兜底。
	if fact.Fallback != "" {
		t.Errorf("声明处不得再带兜底句，实际 %+v", fact)
	}
	if fact.Tail != "" {
		t.Errorf("纯 key 形态不该带定位信息：%q", fact.Tail)
	}
}

func TestErrDetailKeepsTail(t *testing.T) {
	fact, ok := facing.FactOf(ErrDetail(ErrStaleVersion, "产品详情页"))
	if !ok || !fact.Business {
		t.Fatalf("带明细的业务错误应判为业务事实，得到 %+v / %v", fact, ok)
	}
	if fact.Key != ErrStaleVersion {
		t.Errorf("Key = %q，应为 %q", fact.Key, ErrStaleVersion)
	}
	if fact.Tail != "产品详情页" {
		t.Errorf("Tail = %q，应为定位信息", fact.Tail)
	}

	if got := ErrDetail(ErrStaleVersion, "   ").Error(); got != ErrStaleVersion {
		t.Errorf("空明细应退化成纯 key，实际 %q", got)
	}

	if _, ok := facing.FactOf(errors.New(`ERROR: relation "navigations" (SQLSTATE 42P01)`)); ok {
		t.Error("内部原文不该被判成业务事实")
	}
}
