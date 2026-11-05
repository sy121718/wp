package membershipenums

import (
	"errors"
	"testing"

	"go_wp/pkg/facing"
)

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrDefaultTierMissing)
	if err.Error() != ErrDefaultTierMissing {
		t.Fatalf("Error() = %q，应为枚举 key", err.Error())
	}
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("FactOf(Err(...)) 未判成业务错误: %+v, %v", fact, ok)
	}
	// enums 只声明 key + 状态码，中文归词库；声明处不再带成品句（兜底为空）。
	if fact.Fallback != "" {
		t.Errorf("声明处不得再带兜底句，实际 %+v", fact)
	}
	if fact.Tail != "" {
		t.Errorf("不带定位信息的错误不该有 Tail: %+v", fact)
	}

	if got := Err("membership.err.notDeclared"); got.Error() != "membership.err.notDeclared" {
		t.Errorf("未登记 key 的 Error() = %q", got.Error())
	}

	if _, ok := facing.FactOf(errors.New(`
pq: relation "membership_tiers" does not exist`)); ok {
		t.Error("数据库原文不该被判成业务错误")
	}
}

func TestErrDetailKeepsTail(t *testing.T) {
	err := ErrDetail(ErrDefaultTierMissing, "p-1")
	fact, ok := facing.FactOf(err)
	if !ok || !fact.Business {
		t.Fatalf("FactOf(ErrDetail(...)) 未判成业务错误: %+v, %v", fact, ok)
	}
	if fact.Key != ErrDefaultTierMissing || fact.Tail != "p-1" {
		t.Fatalf("Fact = %+v，应保留 key 与定位信息", fact)
	}
	if fact.Fallback != "" {
		t.Errorf("声明处不得再带兜底句: %+v", fact)
	}

	if ErrDetail(ErrNotFound, "   ").Error() != ErrNotFound {
		t.Errorf("空定位信息不该拼分隔符: %q", ErrDetail(ErrNotFound, "   ").Error())
	}
}

func TestEntitlementKindsMatchConstants(t *testing.T) {
	if len(EntitlementKinds) != 2 {
		t.Fatalf("EntitlementKinds = %v，本期恰好两种", EntitlementKinds)
	}
	if EntitlementKinds[0] != KindFreeShipping || EntitlementKinds[1] != KindDiscount {
		t.Errorf("EntitlementKinds = %v", EntitlementKinds)
	}
}
