package feature

import (
	"net/url"
	"strings"
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

func receiptDrawer(t *testing.T, body, orderID string) string {
	t.Helper()
	start := strings.Index(body, "id=\"tpl-receipt-"+orderID+"\"")
	if start < 0 {
		t.Fatalf("采购页没有渲染采购单 %s 的收货抽屉", orderID)
	}
	rest := body[start:]
	end := strings.Index(rest, "</template>")
	if end < 0 {
		t.Fatalf("收货抽屉模板未闭合")
	}
	return rest[:end]
}

func countIn(haystack, needle string) int {
	return strings.Count(haystack, needle)
}

func TestPurchaseReceiptDrawerPerLine(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, _, p, v := seedPurchasePage(t, f)

	p2 := mustProduct(t, f, "Hoodie")
	v2 := f.firstVariant(t, p2.ID)
	order := mustPurchaseOrder(t, f, "PO-DRAWER-1", ext.ID, wh.ID, []purchasedto.PurchaseLineReq{
		purchaseLine(p, v, 5, 2),
		purchaseLine(p2, v2, 4, 3),
	})

	mustReceiveLine(t, f, order.ID, order.Lines[1].ID, 4, "DRAWER-RECV-1")

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != 200 {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	drawer := receiptDrawer(t, rec.Body.String(), order.ID)

	if got := countIn(drawer, "action=\"/admin/inventory/purchases/receipt?"); got != 1 {
		t.Fatalf("收货抽屉应只有 1 个提交表单，实际 %d 个", got)
	}

	if got := countIn(drawer, "name=\"lineId\""); got != 2 {
		t.Fatalf("两行都应有 lineId 隐藏域（含已收满的行），实际 %d 个", got)
	}
	if got := countIn(drawer, "name=\"quantity\""); got != 1 {
		t.Fatalf("只有未收满的行给数量输入，实际 %d 个", got)
	}

	if !strings.Contains(drawer, "name=\"quantity\" value=\"5\"") {
		t.Fatalf("数量输入的默认值应为剩余量 5，抽屉片段：%s", drawer)
	}
	if !strings.Contains(drawer, "max=\"5\"") {
		t.Fatalf("数量输入的上限应为剩余量 5")
	}

	if !strings.Contains(drawer, "已收满") {
		t.Fatalf("已收满的行应显示「已收满」而不是输入框")
	}
}

func TestPurchaseReceiptFormMultiLineSubmit(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, _, p, v := seedPurchasePage(t, f)
	p2 := mustProduct(t, f, "Hoodie")
	v2 := f.firstVariant(t, p2.ID)
	order := mustPurchaseOrder(t, f, "PO-FORM-2", ext.ID, wh.ID, []purchasedto.PurchaseLineReq{
		purchaseLine(p, v, 5, 2),
		purchaseLine(p2, v2, 4, 3),
	})

	rec := postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"requestId": {"PAGE-ML-1"},
		"lineId":    {order.Lines[0].ID, order.Lines[1].ID},
		"quantity":  {"2", "4"},
	})
	assertInventoryJump(t, rec, "ok")

	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 2 {
		t.Fatalf("第 1 行按填的 2 件落库，实际 %d", got)
	}
	if got := lineReceivedIn(t, f, order.Lines[1].ID); got != 4 {
		t.Fatalf("第 2 行按默认 4 件落库，实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 2 {
		t.Fatalf("变体 1 库存应为 2，实际 %d", got)
	}
	if got := f.stockQty(t, v2.ID, wh.ID); got != 4 {
		t.Fatalf("变体 2 库存应为 4，实际 %d", got)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("第 1 行还差货，状态应为 partial，实际 %q", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("整单一次提交应只产生一张入库单，实际 %d", got)
	}

	rec = postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"requestId": {"PAGE-ML-1"},
		"lineId":    {order.Lines[0].ID, order.Lines[1].ID},
		"quantity":  {"2", "4"},
	})
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v2.ID, wh.ID); got != 4 {
		t.Fatalf("重复提交不该二次加库存，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("重复提交不该产生第二张入库单，实际 %d", got)
	}
}

func TestPurchaseReceiptFormSkipsZeroLines(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, _, p, v := seedPurchasePage(t, f)
	order := mustPurchaseOrder(t, f, "PO-FORM-3", ext.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 5, 2)})

	rec := postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"requestId": {"PAGE-ZERO-1"},
		"lineId":    {order.Lines[0].ID},
		"quantity":  {"0"},
	})

	assertInventoryJump(t, rec, "err", "入库清单不能为空")
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("零数量提交不该动库存，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 0 {
		t.Fatalf("零数量提交不该产生入库单，实际 %d", got)
	}

	rec = postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"requestId": {"PAGE-ZERO-2"},
		"lineId":    {order.Lines[0].ID},
		"quantity":  {"3"},
	})
	assertInventoryJump(t, rec, "ok")
	if got := f.stockQty(t, v.ID, wh.ID); got != 3 {
		t.Fatalf("有量的行应正常入库，实际 %d", got)
	}
}
