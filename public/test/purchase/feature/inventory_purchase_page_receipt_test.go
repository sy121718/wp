// Package feature 后台页的「按行收货」入口（B10）。
//
// 页面上这一块的形态对齐 dy_erp 的收货对话框，四个特征缺一不可：
//
//	· 整单**一个** form：多行一次提交（逐行提交时失败在第 7 行，前 6 行已经落库）；
//	· 每行一个数量输入，**默认值 = 剩余量**（全到是常态，不该让人逐行敲）；
//	· 每行都带自己的 lineId（数量是行的属性，行号靠它定位）；
//	· 已收满的行不给表单（收不进来，给了也只是让人点出一个错误）。
//
// 断言一律从**渲染出来的 HTML** 取：手写字段名会让「模板改了而测试照绿」重现 ——
// 本批修的正是字段名与 handler 对不上这类缺陷。
package feature

import (
	"net/url"
	"strings"
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

// receiptDrawer 从采购页 HTML 里切出某张采购单的收货抽屉模板块。
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

// countIn 子串出现次数（断言「整单一个 form」「每行一个输入」）。
func countIn(haystack, needle string) int {
	return strings.Count(haystack, needle)
}

// TestPurchaseReceiptDrawerPerLine 收货抽屉：整单一个 form、每行默认填满剩余量、
// 已收满的行不再给输入。
func TestPurchaseReceiptDrawerPerLine(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, _, p, v := seedPurchasePage(t, f)
	// 第二行用另一个变体（同一张单不能有两行同 SKU：唯一索引 ORDER+VARIANT）。
	p2 := mustProduct(t, f, "Hoodie")
	v2 := f.firstVariant(t, p2.ID)
	order := mustPurchaseOrder(t, f, "PO-DRAWER-1", ext.ID, wh.ID, []purchasedto.PurchaseLineReq{
		purchaseLine(p, v, 5, 2),
		purchaseLine(p2, v2, 4, 3),
	})
	// 先把第 2 行收满：它应当在抽屉里显示「已收满」而不是输入框。
	mustReceiveLine(t, f, order.ID, order.Lines[1].ID, 4, "DRAWER-RECV-1")

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID)
	if rec.Code != 200 {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	drawer := receiptDrawer(t, rec.Body.String(), order.ID)

	// ① 整单一个 form（不是每行一个 form）。
	if got := countIn(drawer, "action=\"/admin/inventory/purchases/receipt?"); got != 1 {
		t.Fatalf("收货抽屉应只有 1 个提交表单，实际 %d 个", got)
	}
	// ② 每个未收满的行都有 lineId 隐藏域与数量输入。
	if got := countIn(drawer, "name=\"lineId\""); got != 2 {
		t.Fatalf("两行都应有 lineId 隐藏域（含已收满的行），实际 %d 个", got)
	}
	if got := countIn(drawer, "name=\"quantity\""); got != 1 {
		t.Fatalf("只有未收满的行给数量输入，实际 %d 个", got)
	}
	// ③ 默认值 = 剩余量（第 1 行 5 件一件没到 → 默认 5；max 也卡在 5）。
	if !strings.Contains(drawer, "name=\"quantity\" value=\"5\"") {
		t.Fatalf("数量输入的默认值应为剩余量 5，抽屉片段：%s", drawer)
	}
	if !strings.Contains(drawer, "max=\"5\"") {
		t.Fatalf("数量输入的上限应为剩余量 5")
	}
	// ④ 已收满的行显示「已收满」。
	if !strings.Contains(drawer, "已收满") {
		t.Fatalf("已收满的行应显示「已收满」而不是输入框")
	}
}

// TestPurchaseReceiptFormMultiLineSubmit 页面表单整单提交：改小一行的数量 →
// 该行只到这么多、另一行按默认值全到。
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

	// 第 1 行改小到 2（部分到货），第 2 行留默认（4 件全到）。
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

	// 同一份表单重复提交（双击 / 回退重发）：幂等键挡住，库存不动。
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

// TestPurchaseReceiptFormSkipsZeroLines 表单里留 0 的行整行跳过：
// 一格都没填时服务层报「没有可收货的行」，而不是给一个零数量明细。
func TestPurchaseReceiptFormSkipsZeroLines(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh, ext, _, p, v := seedPurchasePage(t, f)
	order := mustPurchaseOrder(t, f, "PO-FORM-3", ext.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 5, 2)})

	// 全部留 0 → 空行集。
	rec := postForm(engine, "/admin/inventory/purchases/receipt", url.Values{
		"csrf_token": {"test-csrf"}, "projectId": {f.projectID}, "orderId": {order.ID},
		"requestId": {"PAGE-ZERO-1"},
		"lineId":    {order.Lines[0].ID},
		"quantity":  {"0"},
	})
	// 对外文案是服务层的兜底（enums 的 ErrReceiptLinesRequired），不是裸 key。
	assertInventoryJump(t, rec, "err", "入库清单不能为空")
	if got := f.stockQty(t, v.ID, wh.ID); got != 0 {
		t.Fatalf("零数量提交不该动库存，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 0 {
		t.Fatalf("零数量提交不该产生入库单，实际 %d", got)
	}

	// 只填一行（另一行留 0）→ 只收有量的那行。
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
