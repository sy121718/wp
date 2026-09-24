// Package feature 采购 / 收货 / 生产入库三条路径的错误回显收口（本批第三波泄漏面的 inventory 切片）。
//
// 两类断言必须同时成立，缺一类就会把好错误也压掉：
//
//	· 已知业务错误（超收 / 单已收满 / 货源停用…）的文案**原样可见** —— 运营要知道「哪一条不合法」；
//	· 内部实现细节（SQLSTATE / 驱动文案 / 表名 / 约束名）一个字都不许出现在重定向与页面正文里。
//
// 覆盖两种错误回显渠道：?err= 重定向（页面会把它渲染成提示条）与页面正文（列表取数失败时经提示条回显）。
package feature

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

// internalTokens 内部实现细节的黑名单：驱动文案、SQLSTATE、表名与约束名前缀。
//
// 前两类是「数据库把话说全了」，后两类是「哪张表 / 哪个约束」—— 对运营都没有意义，
// 对攻击者是有用的地图，所以对外必须是归口文案，原文只进结构化日志。
var internalTokens = []string{
	"SQLSTATE", "42P01", "23505", "does not exist",
	"inventory_purchase_orders", "inventory_purchase_order_lines",
	"inventory_stocks", "inventory_purchase_receipts", "inventory_warehouses",
	"uq_", "fk_", "pg_",
}

// assertNoInternalTokens 断言给定文本里没有任何内部实现细节。
func assertNoInternalTokens(t *testing.T, where, text string) {
	t.Helper()
	for _, token := range internalTokens {
		if strings.Contains(text, token) {
			t.Fatalf("%s 泄漏了内部实现细节 %q", where, token)
		}
	}
}

// TestPurchaseBusinessErrorStillVisible 已知业务错误的文案必须原样可见（不许一刀切成通用提示）。
func TestPurchaseBusinessErrorStillVisible(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, _, p, v := seedPurchasePage(t, f)
	order := mustPurchaseOrder(t, f, "PO-ERR-1", ext.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 2, 5)})
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 2, "ERR-LEAK-1")

	// 已收满的单再收一次：这是一条**业务**错误（单据已完成），文案必须可行动。
	rec := postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"t"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"lineId": {order.Lines[0].ID}, "quantity": {"1"}, "requestId": {"ERR-LEAK-2"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("收货失败应 302 回列表，实际 %d：%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	assertNoInternalTokens(t, "收货失败的重定向", loc)
	if !strings.Contains(loc, url.QueryEscape("采购单已全部入库，无需再收")) {
		t.Fatalf("业务错误的文案必须原样可见（中文），实际 %q", loc)
	}
	if strings.Contains(loc, inventoryenums.ErrReceiptOrderDone) {
		t.Fatalf("回显里不该出现裸 key %q：%q", inventoryenums.ErrReceiptOrderDone, loc)
	}

	// 回跳页把同一句话渲染成提示条：整页正文同样不许出现内部细节。
	page := httptestGet(engine, loc)
	if page.Code != http.StatusOK {
		t.Fatalf("回跳的采购入库页应 200，实际 %d", page.Code)
	}
	body := page.Body.String()
	if !strings.Contains(body, "采购单已全部入库，无需再收") {
		t.Fatalf("业务错误的文案在页面上不可见（被压成通用提示？）")
	}
	assertNoInternalTokens(t, "收货失败回显页", body)

	// 建单路径的同一判据（两条都是业务错误，文案都必须原样可见）：
	//   a) 完全不提交 SKU 字段 —— 模板与 handler 的字段名对不上时就是这个形态：整行被跳过，
	//      单据一行都没有 → 「采购单至少要有一行」；
	//   b) 三段值坏掉（只给了变体 ID）—— 仓库侧编码为空 → 「缺少仓库侧 SKU 编码」，
	//      这条文案必须可行动（告诉调用方传裸码），不能被压成通用提示。
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
		{"缺 SKU 字段", "PO-ERR-2", "", "采购单至少要有一行", inventoryenums.ErrPurchaseLinesRequired},
		{"三段值坏掉", "PO-ERR-3", v.ID, "缺少仓库侧 SKU 编码", inventoryenums.ErrStockSKURequired},
	}
	for _, tc := range cases {
		form := copyForm(base)
		form.Set("code", tc.code)
		if tc.sku != "" {
			form.Set("lineSku", tc.sku)
		}
		rec = postForm(engine, "/admin/inventory/purchases/create", form)
		if rec.Code != http.StatusFound {
			t.Fatalf("%s：建单失败应 302 回列表，实际 %d：%s", tc.name, rec.Code, rec.Body.String())
		}
		loc = rec.Header().Get("Location")
		assertNoInternalTokens(t, tc.name+"的建单重定向", loc)
		if !strings.Contains(loc, url.QueryEscape(tc.wantText)) {
			t.Fatalf("%s：业务错误的文案应原样可见，实际 %q", tc.name, loc)
		}
		if strings.Contains(loc, tc.forbidden) {
			t.Fatalf("%s：回显里不该出现裸 key %q：%q", tc.name, tc.forbidden, loc)
		}
	}
	// 被拒绝的两次建单一张都没落库（半截状态不允许存在）。
	if n := purchaseOrderCount(t, f); n != 1 {
		t.Fatalf("被拒绝的建单不应留下单据（初始 1 张），实际 %d", n)
	}
}

// TestPurchaseInternalErrorNotLeaked 内部故障（数据库原文）不许随提示条直出。
//
// 这里刻意制造一个**真实的**故障：把采购单表改名，让列表取数报出 PostgreSQL 原文
// （relation ... does not exist / SQLSTATE 42P01）。隔离 schema 下这条改表只影响本用例
// （NewMigratedPGTestDB 每个用例一份库），跑完随库一起丢弃。
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
