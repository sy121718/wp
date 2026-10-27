// Package feature 按行收货（B10）：整单一次提交、每行独立到货量、部分到货。
//
// 验收形状对齐 dy_erp 的收货对话框：每行「订购 / 已到 / 剩余」，本次到货默认填满剩余，
// 可以改小成本次只到一部分。服务端契约本身就是逐行数组
// （RegisterReceiptReq.Lines），这里钉的是四条边界：
//
//	· 一次提交多行 → 每行各自落库、各自进库存、同一张入库单；
//	· 改小数量 → 部分到货（状态 partial，剩余量减少）；
//	· 数量 0 的行 → 不落零数量行（表单侧跳过），不影响其他行；
//	· 超收 → 拒绝（received + n <= quantity 写在 UPDATE 的 WHERE 里）。
package feature

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

// twoLines 造一张两行的采购单（两个不同变体，避免同 SKU 撞唯一索引）。
func twoLines(t *testing.T, f *purchaseFixture, code, sourceID, warehouseID string,
	qty1, qty2 int) (*purchasedto.PurchaseOrderResp,
	*productdto.ProductResp, *productdto.ProductResp,
	*productdto.VariantResp, *productdto.VariantResp) {
	t.Helper()
	p1 := mustProduct(t, f, "Tee")
	v1 := f.firstVariant(t, p1.ID)
	p2 := mustProduct(t, f, "Hoodie")
	v2 := f.firstVariant(t, p2.ID)
	order := mustPurchaseOrder(t, f, code, sourceID, warehouseID, []purchasedto.PurchaseLineReq{
		purchaseLine(p1, v1, qty1, 2),
		purchaseLine(p2, v2, qty2, 3),
	})
	return order, p1, p2, v1, v2
}

// TestReceiptMultiLineOneSubmit 一次提交收两行：每行各自的数量都落库、都进库存、
// 都在同一张入库单里。
//
// 为什么必须是同一张入库单：入库单是「这次到货」的凭据，拆成两张后
// 「这一批货到齐了吗」就没有答案了；库存流水侧也靠 batch_id 归集同一批。
func TestReceiptMultiLineOneSubmit(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	order, _, _, v1, v2 := twoLines(t, f, "PO-ML-1", src.ID, wh.ID, 5, 4)

	res, err := f.purchase.RegisterReceipt(context.Background(), &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "ML-RECV-1",
		Lines: []purchasedto.ReceiptLineReq{
			{LineID: order.Lines[0].ID, Quantity: 3},
			{LineID: order.Lines[1].ID, Quantity: 2},
		},
	})
	if err != nil {
		t.Fatalf("整单两行收货失败: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("一张入库单应含 2 个明细行，实际 %d", len(res.Items))
	}
	if res.MovementBatchID == "" {
		t.Fatalf("入库单应记下库存批次号（收货必须落库存）")
	}
	// 每行各自落库。
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 3 {
		t.Fatalf("第 1 行已收应为 3，实际 %d", got)
	}
	if got := lineReceivedIn(t, f, order.Lines[1].ID); got != 2 {
		t.Fatalf("第 2 行已收应为 2，实际 %d", got)
	}
	// 每行各自进库存（不同变体，各 1 行真源）。
	if got := f.stockQty(t, v1.ID, wh.ID); got != 3 {
		t.Fatalf("变体 1 库存应为 3，实际 %d", got)
	}
	if got := f.stockQty(t, v2.ID, wh.ID); got != 2 {
		t.Fatalf("变体 2 库存应为 2，实际 %d", got)
	}
	// 两行都没收满（3/5、2/4）→ partial；汇总进度 5/9。
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("两行都没收满时状态应为 partial，实际 %q", got)
	}
	detail := getPurchaseOrder(t, f, order.ID)
	if detail.TotalQuantity != 9 || detail.ReceivedQuantity != 5 {
		t.Fatalf("汇总应为 订 9 / 收 5，实际 %d / %d", detail.TotalQuantity, detail.ReceivedQuantity)
	}
	if detail.OutstandingQuantity != 4 {
		t.Fatalf("未到量应为 4，实际 %d", detail.OutstandingQuantity)
	}
	if detail.ProgressPercent != 55 {
		t.Fatalf("进度应为 5*100/9=55（向下取整），实际 %d", detail.ProgressPercent)
	}
}

// TestReceiptPartialPerLine 按行改小 = 部分到货：只收第 1 行的一部分，第 2 行本次不收。
func TestReceiptPartialPerLine(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	order, _, _, v1, v2 := twoLines(t, f, "PO-PL-1", src.ID, wh.ID, 5, 4)

	// 只提交第 1 行的 2 件（第 2 行本次不到）—— 这就是「按行填不同的到货数量」。
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 2, "PL-RECV-1")

	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("部分到货状态应为 partial，实际 %q", got)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != 2 {
		t.Fatalf("第 1 行库存应为 2，实际 %d", got)
	}
	if got := f.stockQty(t, v2.ID, wh.ID); got != 0 {
		t.Fatalf("本次没提交的行不该进库存，实际 %d", got)
	}
	if got := lineReceivedIn(t, f, order.Lines[1].ID); got != 0 {
		t.Fatalf("本次没提交的行不该改已收数量，实际 %d", got)
	}
	// 剩余量按行算：第 1 行差 3、第 2 行差 4。
	detail := getPurchaseOrder(t, f, order.ID)
	if detail.Lines[0].OutstandingQuantity != 3 || detail.Lines[1].OutstandingQuantity != 4 {
		t.Fatalf("按行剩余量应为 3 / 4，实际 %d / %d",
			detail.Lines[0].OutstandingQuantity, detail.Lines[1].OutstandingQuantity)
	}
	if detail.OutstandingQuantity != 7 || detail.ProgressPercent != 22 {
		t.Fatalf("汇总未到应为 7、进度应为 2*100/9=22，实际 %d / %d",
			detail.OutstandingQuantity, detail.ProgressPercent)
	}

	// 第二批：第 1 行收满、第 2 行收 1 → 状态仍 partial（第 2 行还差 3）。
	res, err := f.purchase.RegisterReceipt(context.Background(), &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "PL-RECV-2",
		Lines: []purchasedto.ReceiptLineReq{
			{LineID: order.Lines[0].ID, Quantity: 3},
			{LineID: order.Lines[1].ID, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("第二批收货失败: %v", err)
	}
	if res.Idempotent {
		t.Fatalf("新的 requestId 不该命中幂等重放")
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("第 2 行还差货时状态应为 partial，实际 %q", got)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != 5 {
		t.Fatalf("第 1 行累计库存应为 5，实际 %d", got)
	}
	if got := f.stockQty(t, v2.ID, wh.ID); got != 1 {
		t.Fatalf("第 2 行累计库存应为 1，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 2 {
		t.Fatalf("两批到货应留两张入库单，实际 %d", got)
	}
}

// TestReceiptZeroQuantityLineSkipped 表单里「留 0」的行整行跳过，不产生零数量明细。
//
// 这条不是表单的私有约定：0 在收货语义里没有「收到 0 件」的位置 —— 收 0 件等于没收货，
// 而一次「没收货」却占了一张入库单和一个幂等键，会让下一次真收货撞上幂等重放。
func TestReceiptZeroQuantityLineSkipped(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	order, _, _, v1, _ := twoLines(t, f, "PO-ZQ-1", src.ID, wh.ID, 5, 4)

	// 直接走服务层给 0：服务层仍然拒绝（表单跳过 0 是 UI 的便利，不是契约的允诺）。
	_, err := f.purchase.RegisterReceipt(context.Background(), &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "ZQ-RECV-1",
		Lines: []purchasedto.ReceiptLineReq{
			{LineID: order.Lines[0].ID, Quantity: 2},
			{LineID: order.Lines[1].ID, Quantity: 0},
		},
	})
	purchaseErr(t, err, purchaseenums.ErrReceiptQuantityInvalid)
	// 整单原子：前一行也不该落库。
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 0 {
		t.Fatalf("被拒绝的整单不该改动任何行，实际第 1 行已收 %d", got)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != 0 {
		t.Fatalf("被拒绝的整单不该动库存，实际 %d", got)
	}

	// 只提交有量的那一行（表单的实际形态）→ 成功，且只产生 1 个明细行。
	res := mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 2, "ZQ-RECV-2")
	if len(res.Items) != 1 {
		t.Fatalf("只填一行的提交应产生 1 个明细，实际 %d", len(res.Items))
	}
}

// TestReceiptOverReceivePerLine 逐行超收必须拒：每行的守卫是「已收 + 本次 <= 订购」。
func TestReceiptOverReceivePerLine(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	order, _, _, v1, v2 := twoLines(t, f, "PO-OR-1", src.ID, wh.ID, 5, 4)

	// 第 1 行多收 1 件：整单拒绝，第 2 行也不落。
	_, err := f.purchase.RegisterReceipt(context.Background(), &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "OR-RECV-1",
		Lines: []purchasedto.ReceiptLineReq{
			{LineID: order.Lines[0].ID, Quantity: 2},
			{LineID: order.Lines[1].ID, Quantity: 5},
		},
	})
	purchaseErr(t, err, purchaseenums.ErrReceiptOverReceive)
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 0 {
		t.Fatalf("超收被拒时第 1 行也不该落，实际 %d", got)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != 0 {
		t.Fatalf("超收被拒时不该动库存，实际 %d", got)
	}
	if got := f.stockQty(t, v2.ID, wh.ID); got != 0 {
		t.Fatalf("超收被拒时第 2 行也不该动库存，实际 %d", got)
	}

	// 已收之后再超：先收 3，再想收 3（累计 6 > 5）。
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "OR-RECV-2")
	_, err = receiveLine(t, f, order.ID, order.Lines[0].ID, 3, "OR-RECV-3")
	purchaseErr(t, err, purchaseenums.ErrReceiptOverReceive)
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 3 {
		t.Fatalf("超收被拒后已收应仍为 3，实际 %d", got)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != 3 {
		t.Fatalf("超收被拒后库存应仍为 3，实际 %d", got)
	}
}

// TestReceiptMultiLineIdempotency 整单提交的幂等键：同一 requestId 重复提交
// （浏览器双击 / 网络重试）只产生一张入库单，库存只加一次。
func TestReceiptMultiLineIdempotency(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_A", "外部货源 A", purchaseenums.SourceTypeExternal)
	order, _, _, v1, v2 := twoLines(t, f, "PO-IDEM-1", src.ID, wh.ID, 5, 4)

	req := &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "IDEM-ML-1",
		Lines: []purchasedto.ReceiptLineReq{
			{LineID: order.Lines[0].ID, Quantity: 2},
			{LineID: order.Lines[1].ID, Quantity: 1},
		},
	}
	first, err := f.purchase.RegisterReceipt(context.Background(), req)
	if err != nil {
		t.Fatalf("首次收货失败: %v", err)
	}
	if first.Idempotent {
		t.Fatalf("首次收货不该是幂等重放")
	}
	second, err := f.purchase.RegisterReceipt(context.Background(), req)
	if err != nil {
		t.Fatalf("重复提交应命中幂等重放而非报错: %v", err)
	}
	if !second.Idempotent || second.ID != first.ID {
		t.Fatalf("重复提交应返回同一张入库单（idempotent=true），实际 %q / %v", second.ID, second.Idempotent)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != 2 {
		t.Fatalf("重复提交不该二次加库存（变体 1），实际 %d", got)
	}
	if got := f.stockQty(t, v2.ID, wh.ID); got != 1 {
		t.Fatalf("重复提交不该二次加库存（变体 2），实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("重复提交不该产生第二张入库单，实际 %d", got)
	}
}
