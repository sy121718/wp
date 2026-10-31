package facing_test

import (
	"fmt"
	"testing"

	"go_wp/pkg/facing"
)

func TestFactOfHalfWidthColonStillWorks(t *testing.T) {
	const key = "order.err.returnQuantityExceeded"
	fact, ok := facing.FactOf(fmt.Errorf("%s: %s", key, "detail"))
	if !ok || fact.Key != key || fact.Tail != "detail" {
		t.Fatalf("半角冒号形态应仍然成立，实际 ok=%v fact=%+v", ok, fact)
	}
}

func TestFactOfSeparatorIsTheFirstOne(t *testing.T) {
	const key = "order.err.returnQuantityExceeded"
	fact, ok := facing.FactOf(fmt.Errorf("%s: 见 https://example.test/a:b", key))
	if !ok || fact.Key != key {
		t.Fatalf("应认出 key，实际 ok=%v key=%q", ok, fact.Key)
	}
	if fact.Tail != "见 https://example.test/a:b" {
		t.Errorf("明细应完整保留（含内部冒号），实际 %q", fact.Tail)
	}
}

func TestFactOfSingleKeyIsBusiness(t *testing.T) {
	fact, ok := facing.FactOf(facing.New("ErrSessionNotFound", "会话不存在"))
	if !ok || !fact.Business || fact.Key != "ErrSessionNotFound" {
		t.Fatalf("单段 key 应命中业务判据，实际 ok=%v fact=%+v", ok, fact)
	}
}

func TestFactOfPlainErrorIsNotBusiness(t *testing.T) {
	for _, raw := range []string{
		`pq: relation "sys_config" does not exist`,
		`open /var/lib/data/x.db: permission denied`,
	} {
		if fact, ok := facing.FactOf(fmt.Errorf("%s", raw)); ok && fact.Business {
			t.Errorf("%q 不该被判成业务错误（会原样上屏）", raw)
		}
	}
}
