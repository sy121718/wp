package feature

// inventory_stock_invariant_test.go — 直接走 model 写入路径时的不变量（迁移 261）：
// 「不跟踪（无限）的行不允许带非零数量」必须由 model 兜住，而不是从 DDL 冒出 23514。
//
// service 路径（ensureStockRowWithExternalTx）传了数量就自己翻开关，所以这个缺陷只在
// **直接调 model** 的调用点暴露（测试夹具、脚本、将来的导入路径）；页面侧会看到一个
// 内部错误 —— 这正是「别只依赖 DDL 报错」的理由：CHECK 是最后一道防线，不是唯一一道。

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	inventorymodel "go_wp/internal/module/product/inventory/model"
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

	// 直连 model：只给数量、不翻开关（service 会自己翻，所以只能这样复现）。
	ensure := func(warehouseID string, track bool, quantity int) (*inventorymodel.StockEntity, error) {
		now := time.Now().UTC()
		return m.EnsureStock(ctx, &inventorymodel.StockEntity{
			ID: uuid.NewString(), ProjectID: f.projectID, WarehouseID: warehouseID,
			ProductID: p.ID, VariantID: v.ID, SKUCode: bareSKU(v.SKUCode, sh.Code),
			TrackQuantity: track, Quantity: quantity,
			Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
		})
	}

	// ① 不跟踪 + 7 件：必须插得进去（model 改判成跟踪行），而不是撞 CHECK 报 23514。
	got, err := ensure(sh.ID, false, 7)
	if err != nil {
		t.Fatalf("不跟踪却带数量的行应被改判为跟踪行，不该撞 DDL CHECK：%v", err)
	}
	if !got.TrackQuantity || got.Quantity != 7 {
		t.Fatalf("改判后应是「跟踪且 7 件」，实际 track=%v quantity=%d", got.TrackQuantity, got.Quantity)
	}
	// 真源列也要一致：返回值就是入参实体，只信它会漏掉「改判只发生在内存里」这种写法。
	var track bool
	var qty int
	if qerr := f.db.Raw("SELECT track_quantity, quantity FROM inventory_stocks "+
		"WHERE variant_id = ? AND warehouse_id = ?", v.ID, sh.ID).Row().Scan(&track, &qty); qerr != nil {
		t.Fatalf("读库存行失败: %v", qerr)
	}
	if !track || qty != 7 {
		t.Fatalf("落库应为 track_quantity=true quantity=7，实际 track=%v quantity=%d", track, qty)
	}

	// ② 反向：不跟踪 + 0 仍是**不跟踪**（0 不是「给了数量」，别把无限改判成卖光）。
	got0, err := ensure(nb.ID, false, 0)
	if err != nil {
		t.Fatalf("不跟踪 + 0 应正常插入: %v", err)
	}
	if got0.TrackQuantity || got0.Quantity != 0 {
		t.Fatalf("不跟踪 + 0 应保持 track=false quantity=0，实际 track=%v quantity=%d",
			got0.TrackQuantity, got0.Quantity)
	}
}
