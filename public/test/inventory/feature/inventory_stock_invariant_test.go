package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"
	"time"

	"github.com/google/uuid"

	inventorymodel "go_wp/internal/module/inventory/model"
)

func TestEnsureStockCoercesUntrackedRowWithQuantity(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createWarehouse(t, "SZ", "苏州仓", true)
	sh := f.createWarehouse(t, "SH", "上海仓", false)
	nb := f.createWarehouse(t, "NB", "宁波仓", false)
	p := mustProductPriced(t, f, "不变量商品", 9.9)
	v := f.firstVariant(t, p.ID)
	m := inventorymodel.NewModel(f.db)

	ensure := func(warehouseID string, track bool, quantity int) (*inventorymodel.StockEntity, error) {
		now := time.Now().UTC()
		return m.EnsureStock(ctx, &inventorymodel.StockEntity{
			ID: uuid.NewString(), ProjectID: f.projectID, WarehouseID: warehouseID,
			ProductID: &p.ID, VariantID: &v.ID, SKUCode: bareSKU(v.SKUCode, sh.Code),
			TrackQuantity: track, Quantity: quantity,
			Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
		})
	}

	got, err := ensure(sh.ID, false, 7)
	if err != nil {
		t.Fatalf("不跟踪却带数量的行应被改判为跟踪行，不该撞 DDL CHECK：%v", err)
	}
	if !got.TrackQuantity || got.Quantity != 7 {
		t.Fatalf("改判后应是「跟踪且 7 件」，实际 track=%v quantity=%d", got.TrackQuantity, got.Quantity)
	}

	var track bool
	var qty int
	if qerr := f.db.Raw("SELECT track_quantity, quantity FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", v.ID, sh.ID).Row().Scan(&track, &qty); qerr != nil {
		t.Fatalf("读库存行失败: %v", qerr)
	}
	if !track || qty != 7 {
		t.Fatalf("落库应为 track_quantity=true quantity=7，实际 track=%v quantity=%d", track, qty)
	}

	got0, err := ensure(nb.ID, false, 0)
	if err != nil {
		t.Fatalf("不跟踪 + 0 应正常插入: %v", err)
	}
	if got0.TrackQuantity || got0.Quantity != 0 {
		t.Fatalf("不跟踪 + 0 应保持 track=false quantity=0，实际 track=%v quantity=%d",
			got0.TrackQuantity, got0.Quantity)
	}
}
