package inventoryenums

import (
	"errors"
	"strings"
	"testing"

	"go_wp/pkg/facing"
)

func TestErrCarriesFallbackAndKey(t *testing.T) {
	err := Err(ErrWarehouseNotFound)
	if err.Error() != ErrWarehouseNotFound {
		t.Fatalf("Err(%q).Error() = %q，want 原 key", ErrWarehouseNotFound, err.Error())
	}
	fact, ok := facing.FactOf(err)
	// enums 只声明 key + 状态码，中文归词库；声明处不再带成品兜底。
	if !ok || !fact.Business || fact.Key != ErrWarehouseNotFound || fact.Fallback != "" {
		t.Fatalf("FactOf 应给出业务事实（key，兜底回落 key），实际 %+v ok=%v", fact, ok)
	}
	detail := ErrDetail(ErrWarehouseNotFound, "仓库 WH-1")
	if !strings.HasPrefix(detail.Error(), ErrWarehouseNotFound+": ") {
		t.Fatalf("ErrDetail 的明细段必须是半角冒号：%q", detail.Error())
	}
	fact, ok = facing.FactOf(detail)
	if !ok || !fact.Business || fact.Key != ErrWarehouseNotFound || fact.Tail != "仓库 WH-1" {
		t.Fatalf("ErrDetail 的事实应保留 key 与明细段，实际 %+v ok=%v", fact, ok)
	}
	if _, ok := facing.FactOf(errors.New("pq: relation stock_items does not exist")); ok {
		t.Fatal("内部错误不该被判成业务事实")
	}
}
