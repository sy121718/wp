package facing

import (
	"errors"
	"fmt"
	"testing"
)

func TestStatusOfReadsDeclaredStatus(t *testing.T) {
	base := New("inventory.err.warehouseNotFound", "仓库不存在")

	if _, ok := StatusOf(base); ok {
		t.Fatal("未声明状态的错误不该有状态码")
	}

	with := WithStatus(base, 404)
	status, ok := StatusOf(with)
	if !ok || status != 404 {
		t.Fatalf("StatusOf = (%d, %v)，want (404, true)", status, ok)
	}

	// 包装之后原来的语义要全在：文案、哨兵、事实判据。
	if with.Error() != base.Error() {
		t.Fatalf("Error() = %q，want %q", with.Error(), base.Error())
	}
	if !errors.Is(with, base) {
		t.Fatal("WithStatus 之后 errors.Is 认不出原错误")
	}
	if fact, ok := FactOf(with); !ok || !fact.Business || fact.Key != base.key {
		t.Fatalf("包装后的事实变了：%+v ok=%v", fact, ok)
	}
}

func TestStatusOfWalksWrappedErrors(t *testing.T) {
	base := WithStatus(New("inventory.err.stockInsufficient", "库存不足"), 409)
	wrapped := fmt.Errorf("扣减失败: %w", base)

	status, ok := StatusOf(wrapped)
	if !ok || status != 409 {
		t.Fatalf("复合错误的 StatusOf = (%d, %v)，want (409, true)", status, ok)
	}

	// 明细形态（半角冒号）同样穿透：ErrDetail 就是 fmt.Errorf("%w: %s") 的产物。
	detailed := fmt.Errorf("%w: sku-1", base)
	if s, ok := StatusOf(detailed); !ok || s != 409 {
		t.Fatalf("带明细的复合错误 StatusOf = (%d, %v)", s, ok)
	}
}

func TestWithStatusIgnoresInvalidInput(t *testing.T) {
	if WithStatus(nil, 404) != nil {
		t.Fatal("nil 错误不该被包装成非 nil")
	}
	base := New("inventory.err.x", "x")
	if got := WithStatus(base, 0); got != base {
		t.Fatal("status <= 0 时应原样返回")
	}
	if _, ok := StatusOf(nil); ok {
		t.Fatal("nil 不该有状态码")
	}
}
