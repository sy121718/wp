package feature

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	inventoryenums "go_wp/internal/module/inventory/enums"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

var internalTokens = []string{
	"SQLSTATE", "42P01", "23505", "does not exist",
	"inventory_purchase_orders", "inventory_purchase_order_lines",
	"inventory_stocks", "inventory_purchase_receipts", "inventory_warehouses",
	"uq_", "fk_", "pg_",
}

func assertNoInternalTokens(t *testing.T, where, text string) {
	t.Helper()
	for _, token := range internalTokens {
		if strings.Contains(text, token) {
			t.Fatalf("%s 泄漏了内部实现细节 %q", where, token)
		}
	}
}

func TestPurchaseBusinessErrorStillVisible(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, _, p, v := seedPurchasePage(t, f)
	order := mustPurchaseOrder(t, f, "PO-ERR-1", ext.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 2, 5)})
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 2, "ERR-LEAK-1")

	rec := postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"t"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"1"}, "requestId": {"ERR-LEAK-2"},
	})

	assertInventoryJump(t, rec, "err", "采购单已全部入库，无需再收")
	body := rec.Body.String()
	assertNoInternalTokens(t, "收货失败提示页", body)
	if strings.Contains(body, purchaseenums.ErrReceiptOrderDone) {
		t.Fatalf("提示页不该出现裸 key %q", purchaseenums.ErrReceiptOrderDone)
	}

	base := url.Values{
		"csrf_token": {"t"}, "projectId": {f.projectID},
		"sourceId": {ext.ID}, "warehouseId": {wh.ID},
		"lineQuantity": {"1"}, "lineUnitPrice": {"1"},
	}
	cases := []struct {
		name      string
		code      string
		sku       string
		wantText  string
		forbidden string
	}{
		{"缺 SKU 字段", "PO-ERR-2", "", "采购单至少要有一行", purchaseenums.ErrPurchaseLinesRequired},
		{"三段值坏掉", "PO-ERR-3", v.ID, "缺少仓库侧 SKU 编码", inventoryenums.ErrStockSKURequired},
	}
	for _, tc := range cases {
		form := copyForm(base)
		form.Set("code", tc.code)
		if tc.sku != "" {
			form.Set("lineSku", tc.sku)
		}
		rec = postForm(engine, "/admin/inventory/purchases/create", form)
		assertInventoryJump(t, rec, "err", tc.wantText)
		if strings.Contains(rec.Body.String(), tc.forbidden) {
			t.Fatalf("%s：提示页不该出现裸 key %q", tc.name, tc.forbidden)
		}
		assertNoInternalTokens(t, tc.name+"的建单提示页", rec.Body.String())
	}

	if n := purchaseOrderCount(t, f); n != 1 {
		t.Fatalf("被拒绝的建单不应留下单据（初始 1 张），实际 %d", n)
	}
}

func TestPurchaseInternalErrorNotLeaked(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	seedPurchasePage(t, f)
	if err := f.db.Exec("ALTER TABLE inventory_purchase_orders RENAME TO inventory_purchase_orders_gone").Error; err != nil {
		t.Fatalf("制造内部故障（改表名）失败: %v", err)
	}

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表取数失败时页面仍应 200（失败经提示条表达），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	assertNoInternalTokens(t, "内部故障时的采购入库页", body)
	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Fatalf("非业务错误应给归口文案（提示条里没有）")
	}
}
