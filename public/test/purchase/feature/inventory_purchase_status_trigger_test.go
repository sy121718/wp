package feature

import (
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
)

func TestPurchaseStatusTriggerFollowsLineWrites(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}

	wh := f.createWarehouse(t, "TRG", "触发器测试仓", true)
	src := mustPurchaseSource(t, f, "TRG_SUP", "触发器测试货源", "external")
	p := mustProductPriced(t, f, "触发器商品", 199)
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "TRG-001", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 10, 5)})

	if got := purchaseStatusIn(t, f, order.ID); got != "ordered" {
		t.Fatalf("建单后状态应为 ordered，实得 %s", got)
	}

	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = quantity WHERE order_id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接写明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Errorf("明细全部收满后数据库应把状态推成 received，实得 %s", got)
	}

	if err := f.db.Exec("UPDATE inventory_purchase_orders SET status = 'partial' WHERE id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("伪造不一致状态失败: %v", err)
	}
	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = 4 WHERE order_id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接改明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "partial" {
		t.Errorf("4/10 应为 partial，实得 %s", got)
	}

	extraProduct := mustProductPriced(t, f, "触发器商品-附加行", 299)
	extraVariant := f.firstVariant(t, extraProduct.ID)
	if err := f.db.Exec(
		"INSERT INTO inventory_purchase_order_lines "+
			"(id, order_id, project_id, product_id, variant_id, sku_code, quantity, received_quantity, unit_price, sort, remark, metadata, create_time, update_time) "+
			"VALUES (gen_random_uuid(), ?, ?, ?, ?, 'TRG-EXTRA', 1, 0, 1, 9, '', '{}'::jsonb, now(), now())",
		order.ID, f.projectID, extraProduct.ID, extraVariant.ID).Error; err != nil {
		t.Fatalf("直接插入明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "partial" {
		t.Errorf("新增一行未收明细后应为 partial，实得 %s", got)
	}

	if err := f.db.Exec("DELETE FROM inventory_purchase_order_lines WHERE order_id = ? AND sku_code = 'TRG-EXTRA'",
		order.ID).Error; err != nil {
		t.Fatalf("直接删除明细行失败: %v", err)
	}
	if err := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = quantity WHERE order_id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接改写明细行失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Errorf("删掉未收行并收满后应为 received，实得 %s", got)
	}

	if err := f.db.Exec("UPDATE inventory_purchase_orders SET status = 'ordered' WHERE id = ?",
		order.ID).Error; err != nil {
		t.Fatalf("直接改写单头状态失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Errorf("直改单头状态应被守卫换回推导值 received，实得 %s", got)
	}
}

func TestPurchaseStatusTriggerKeepsServicePathIntact(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}

	wh := f.createWarehouse(t, "SVC", "服务路径测试仓", true)
	src := mustPurchaseSource(t, f, "SVC_SUP", "服务路径测试货源", "external")
	p := mustProductPriced(t, f, "服务路径商品", 66)
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "SVC-001", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 8, 3)})

	lineID := order.Lines[0].ID
	mustReceiveLine(t, f, order.ID, lineID, 3, "trg-svc-1")
	if got := purchaseStatusIn(t, f, order.ID); got != "partial" {
		t.Fatalf("收 3/8 后应为 partial，实得 %s", got)
	}
	mustReceiveLine(t, f, order.ID, lineID, 5, "trg-svc-2")
	if got := purchaseStatusIn(t, f, order.ID); got != "received" {
		t.Fatalf("收满后应为 received，实得 %s", got)
	}

	var derived string
	if err := f.db.Raw(
		"SELECT CASE "+
			"WHEN COUNT(*) = 0 THEN 'ordered' "+
			"WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received' "+
			"WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial' "+
			"ELSE 'ordered' END "+
			"FROM inventory_purchase_order_lines WHERE order_id = ?", order.ID).Scan(&derived).Error; err != nil {
		t.Fatalf("独立推导状态失败: %v", err)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != derived {
		t.Errorf("服务层写回的状态与数据库推导不一致：单头 %s / 推导 %s", got, derived)
	}
}

const statusSyncRegistryConditionSQL = "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM (" +
	"SELECT p.proname AS name FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace " +
	"WHERE p.proname IN ('fn_inventory_purchase_order_status_sync', 'fn_inventory_purchase_order_status_guard') " +
	"AND n.nspname = current_schema() " +
	"UNION ALL " +
	"SELECT t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid " +
	"JOIN pg_namespace n2 ON n2.oid = c.relnamespace " +
	"WHERE t.tgname IN ('trg_inventory_purchase_lines_status_sync', 'trg_inventory_purchase_orders_status_guard') " +
	"AND n2.nspname = current_schema() " +
	"AND NOT t.tgisinternal" +
	") AS batch_196"

func TestPurchaseStatusTriggerRegistryCondition(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}

	var after int64
	if err := f.db.Raw(statusSyncRegistryConditionSQL).Scan(&after).Error; err != nil {
		t.Fatalf("判据 SQL 执行失败: %v", err)
	}
	if after != 1 {
		t.Errorf("196 执行后判据应为 1，实得 %d（函数或触发器没建全，或注册块里的名字与本文件不一致）", after)
	}

	var tgtype int64
	if err := f.db.Raw("SELECT t.tgtype::bigint FROM pg_trigger t " +
		"JOIN pg_class c ON c.oid = t.tgrelid " +
		"WHERE t.tgname = 'trg_inventory_purchase_lines_status_sync' AND c.relname = 'inventory_purchase_order_lines'").
		Scan(&tgtype).Error; err != nil {
		t.Fatalf("读触发器属性失败: %v", err)
	}
	if tgtype&1 == 0 || tgtype&2 != 0 || tgtype&4 == 0 || tgtype&8 == 0 || tgtype&16 == 0 {
		t.Errorf("明细行触发器应为行级 AFTER INSERT/UPDATE/DELETE，tgtype=%d", tgtype)
	}

	var guardType int64
	if err := f.db.Raw("SELECT t.tgtype::bigint FROM pg_trigger t " +
		"JOIN pg_class c ON c.oid = t.tgrelid " +
		"WHERE t.tgname = 'trg_inventory_purchase_orders_status_guard' AND c.relname = 'inventory_purchase_orders'").
		Scan(&guardType).Error; err != nil {
		t.Fatalf("读守卫触发器属性失败: %v", err)
	}
	if guardType&1 == 0 || guardType&2 == 0 || guardType&16 == 0 {
		t.Errorf("守卫应为行级 BEFORE UPDATE，tgtype=%d", guardType)
	}
}
