package feature

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
)

func mustPurchaseSource(t *testing.T, f *purchaseFixture, code, name, sourceType string) *purchasedto.SourceResp {
	t.Helper()
	res, err := f.purchase.CreateSource(context.Background(), &purchasedto.CreateSourceReq{
		ProjectID: f.projectID, Code: code, Name: name, Type: sourceType,
	})
	if err != nil {
		t.Fatalf("建货源 %s 失败: %v", code, err)
	}
	return res
}

func mustProductPriced(t *testing.T, f *purchaseFixture, name string, price float64) *productdto.ProductResp {
	t.Helper()
	slug := fmt.Sprintf("%s-%d", strings.ToLower(name), slugSeq.Add(1))
	p, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	return p
}

func purchaseLine(p *productdto.ProductResp, v *productdto.VariantResp, quantity int, unitPrice float64) purchasedto.PurchaseLineReq {
	return purchasedto.PurchaseLineReq{
		VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
		Quantity: quantity, UnitPrice: unitPrice,
	}
}

func mustPurchaseOrder(t *testing.T, f *purchaseFixture, code, sourceID, warehouseID string,
	lines []purchasedto.PurchaseLineReq) *purchasedto.PurchaseOrderResp {
	t.Helper()
	res, err := f.purchase.CreatePurchaseOrder(context.Background(), &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: code, SourceID: sourceID, WarehouseID: warehouseID, Lines: lines,
	})
	if err != nil {
		t.Fatalf("建采购单 %s 失败: %v", code, err)
	}
	return res
}

func getPurchaseOrder(t *testing.T, f *purchaseFixture, id string) *purchasedto.PurchaseOrderResp {
	t.Helper()
	res, err := f.purchase.GetPurchaseOrder(context.Background(), &purchasedto.GetPurchaseOrderReq{ID: id})
	if err != nil {
		t.Fatalf("读采购单失败: %v", err)
	}
	return res
}

func receiveLine(t *testing.T, f *purchaseFixture, orderID, lineID string, quantity int, requestID string) (
	*purchasedto.ReceiptResp, error) {
	t.Helper()
	return f.purchase.RegisterReceipt(context.Background(), &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: orderID, RequestID: requestID,
		Lines: []purchasedto.ReceiptLineReq{{LineID: lineID, Quantity: quantity}},
	})
}

func mustReceiveLine(t *testing.T, f *purchaseFixture, orderID, lineID string, quantity int,
	requestID string) *purchasedto.ReceiptResp {
	t.Helper()
	res, err := receiveLine(t, f, orderID, lineID, quantity, requestID)
	if err != nil {
		t.Fatalf("登记入库 %d 失败: %v", quantity, err)
	}
	return res
}

func purchaseErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("应返回错误 %s，实际成功", want)
	}
	if err.Error() != want {
		t.Fatalf("应返回错误 %s，实际 %v", want, err)
	}
}

func lineReceivedIn(t *testing.T, f *purchaseFixture, lineID string) int {
	t.Helper()
	var qty int
	if err := f.db.Raw("SELECT received_quantity FROM inventory_purchase_order_lines WHERE id = ?", lineID).
		Scan(&qty).Error; err != nil {
		t.Fatalf("读采购行已入库数量失败: %v", err)
	}
	return qty
}

func purchaseStatusIn(t *testing.T, f *purchaseFixture, orderID string) string {
	t.Helper()
	var status string
	if err := f.db.Raw("SELECT status FROM inventory_purchase_orders WHERE id = ?", orderID).
		Scan(&status).Error; err != nil {
		t.Fatalf("读采购单状态失败: %v", err)
	}
	return status
}

func orderReceiptCount(t *testing.T, f *purchaseFixture, orderID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_purchase_receipts WHERE order_id = ?", orderID).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计入库单失败: %v", err)
	}
	return n
}

func purchaseOrderCount(t *testing.T, f *purchaseFixture) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_purchase_orders WHERE project_id = ?", f.projectID).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计采购单失败: %v", err)
	}
	return n
}

func variantCostIn(t *testing.T, f *purchaseFixture, variantID string) *float64 {
	t.Helper()
	var row struct {
		Cost *float64 `gorm:"column:cost"`
	}
	if err := f.db.Raw("SELECT cost_price AS cost FROM product_variants WHERE id = ?", variantID).
		Scan(&row).Error; err != nil {
		t.Fatalf("读商品侧成本价失败: %v", err)
	}
	return row.Cost
}

type purchaseMovement struct {
	Direction      string
	ReasonCode     string
	SourceType     string
	SourceRef      string
	BatchID        string
	Quantity       int
	Delta          int
	QuantityBefore int
	QuantityAfter  int
}

func latestMovement(t *testing.T, f *purchaseFixture, variantID string) purchaseMovement {
	t.Helper()
	row := purchaseMovement{}
	if err := f.db.Raw("SELECT direction, reason_code, source_type, source_ref, batch_id, "+
		"quantity, delta, quantity_before, quantity_after "+
		"FROM inventory_stock_movements WHERE variant_id = ? ORDER BY create_time DESC, id DESC LIMIT 1",
		variantID).Scan(&row).Error; err != nil {
		t.Fatalf("读库存流水失败: %v", err)
	}
	return row
}

func TestPurchaseOrderCreateWithAtomicReceive(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	p1 := mustProduct(t, f, "Tee")
	v1 := f.firstVariant(t, p1.ID)
	p2 := mustProduct(t, f, "Mug")
	v2 := f.firstVariant(t, p2.ID)

	_, err := f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-EMPTY", SourceID: src.ID, Lines: nil,
	})
	purchaseErr(t, err, purchaseenums.ErrPurchaseLinesRequired)
	_, err = f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-DUP", SourceID: src.ID,
		Lines: []purchasedto.PurchaseLineReq{
			purchaseLine(p1, v1, 1, 1), purchaseLine(p1, v1, 2, 1),
		},
	})
	purchaseErr(t, err, purchaseenums.ErrPurchaseLineDuplicate)
	_, err = f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-QTY", SourceID: src.ID,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 0, 1)},
	})
	purchaseErr(t, err, purchaseenums.ErrPurchaseQuantityInvalid)
	_, err = f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-PRICE", SourceID: src.ID,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 1, 0)},
	})
	purchaseErr(t, err, purchaseenums.ErrPurchasePriceInvalid)
	_, err = f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-NOSRC", Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 1, 1)},
	})
	purchaseErr(t, err, purchaseenums.ErrPurchaseSourceRequired)

	order := mustPurchaseOrder(t, f, "po-2026-001", src.ID, wh.ID, []purchasedto.PurchaseLineReq{
		purchaseLine(p1, v1, 10, 12.5),
		purchaseLine(p2, v2, 4, 3.25),
	})
	if order.Code != "PO-2026-001" {
		t.Fatalf("采购单号应归一为大写，实际 %q", order.Code)
	}
	if order.Status != purchaseenums.PurchaseStatusOrdered {
		t.Fatalf("新建采购单状态应为 ordered，实际 %q", order.Status)
	}

	if order.ProgressPercent != 0 || order.OutstandingQuantity != 14 {
		t.Fatalf("未收货的单应为 0%% 且未到量 = 订购量，实际 %d%% / %d",
			order.ProgressPercent, order.OutstandingQuantity)
	}
	if len(order.Lines) != 2 || order.TotalQuantity != 14 || order.ReceivedQuantity != 0 {
		t.Fatalf("采购行不完整：%+v", order)
	}
	if order.Lines[0].OutstandingQuantity != 10 {
		t.Fatalf("未入库余量应为采购数量，实际 %d", order.Lines[0].OutstandingQuantity)
	}
	if order.SourceName != "苏州通达电子" || order.WarehouseName != "苏州仓" {
		t.Fatalf("采购单应带来源与收货仓展示信息：%+v", order)
	}

	_, err = f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-2026-001", SourceID: src.ID,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 1, 1)},
	})
	purchaseErr(t, err, purchaseenums.ErrPurchaseCodeTaken)

	_, err = f.purchase.CreatePurchaseOrder(ctx, &purchasedto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO 2026/001", SourceID: src.ID,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 1, 1)},
	})
	purchaseErr(t, err, purchaseenums.ErrPurchaseCodeInvalid)

	line1, line2 := order.Lines[0], order.Lines[1]

	_, err = f.purchase.RegisterReceipt(ctx, &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "REQ-DUP-LINE",
		Lines: []purchasedto.ReceiptLineReq{
			{LineID: line1.ID, Quantity: 1}, {LineID: line1.ID, Quantity: 1},
		},
	})
	purchaseErr(t, err, purchaseenums.ErrReceiptLineDuplicate)

	mustReceiveLine(t, f, order.ID, line1.ID, 6, "REQ-A1")
	if got := lineReceivedIn(t, f, line1.ID); got != 6 {
		t.Fatalf("已入库数量应为 6（原子递增的结果），实际 %d", got)
	}
	got := getPurchaseOrder(t, f, order.ID)
	if got.Status != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("收 6/10 后状态应为 partial，实际 %q", got.Status)
	}
	if got.Lines[0].ReceivedQuantity != 6 || got.Lines[0].OutstandingQuantity != 4 {
		t.Fatalf("部分入库后行数量不正确：%+v", got.Lines[0])
	}
	if got.ReceivedQuantity != 6 || got.TotalQuantity != 14 {
		t.Fatalf("单据汇总不正确：收到 %d / 合计 %d", got.ReceivedQuantity, got.TotalQuantity)
	}

	beforeStock := f.stockQty(t, v1.ID, wh.ID)
	_, err = receiveLine(t, f, order.ID, line1.ID, 5, "REQ-A2")
	purchaseErr(t, err, purchaseenums.ErrReceiptOverReceive)
	if got := lineReceivedIn(t, f, line1.ID); got != 6 {
		t.Fatalf("超收被拒后已入库数量不应变化，实际 %d", got)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != beforeStock {
		t.Fatalf("超收被拒后库存不应变化，实际 %d（原 %d）", got, beforeStock)
	}

	_, err = receiveLine(t, f, order.ID, "00000000-0000-0000-0000-000000000000", 1, "REQ-A3")
	purchaseErr(t, err, purchaseenums.ErrPurchaseLineNotFound)
	_, err = receiveLine(t, f, order.ID, line1.ID, 0, "REQ-A4")
	purchaseErr(t, err, purchaseenums.ErrReceiptQuantityInvalid)

	mustReceiveLine(t, f, order.ID, line1.ID, 4, "REQ-A5")
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusPartial {
		t.Fatalf("还有一行没收满时状态应保持 partial，实际 %q", got)
	}
	mustReceiveLine(t, f, order.ID, line2.ID, 4, "REQ-A6")
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusReceived {
		t.Fatalf("全部收满后状态应为 received，实际 %q", got)
	}

	_, err = receiveLine(t, f, order.ID, line2.ID, 1, "REQ-A7")
	purchaseErr(t, err, purchaseenums.ErrReceiptOrderDone)

	list, lerr := f.purchase.ListPurchaseOrders(ctx, &purchasedto.ListPurchaseOrderReq{
		ProjectID: f.projectID, Status: purchaseenums.PurchaseStatusReceived,
	})
	if lerr != nil || len(list) != 1 || list[0].ID != order.ID {
		t.Fatalf("按 received 筛选应命中本单：%v %+v", lerr, list)
	}
	_, lerr = f.purchase.ListPurchaseOrders(ctx, &purchasedto.ListPurchaseOrderReq{
		ProjectID: f.projectID, Status: "shipped",
	})
	purchaseErr(t, lerr, purchaseenums.ErrPurchaseStatusInvalid)

	if werr := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = quantity + 1 WHERE id = ?",
		line1.ID).Error; werr == nil {
		t.Fatalf("已入库数量超过采购数量的直写应被 CHECK 约束拒绝")
	}

	_, err = f.purchase.UpdatePurchaseOrder(ctx, &purchasedto.UpdatePurchaseOrderReq{
		ID: order.ID, ReplaceLines: true,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 20, 9)},
	})
	purchaseErr(t, err, purchaseenums.ErrPurchaseLinesLocked)
	draft := mustPurchaseOrder(t, f, "PO-2026-002", src.ID, wh.ID, []purchasedto.PurchaseLineReq{
		purchaseLine(p1, v1, 2, 5),
	})
	updated, uerr := f.purchase.UpdatePurchaseOrder(ctx, &purchasedto.UpdatePurchaseOrderReq{
		ID: draft.ID, ReplaceLines: true,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 3, 5.5), purchaseLine(p2, v2, 1, 2)},
	})
	if uerr != nil {
		t.Fatalf("未入库的单应可整行替换：%v", uerr)
	}
	if len(updated.Lines) != 2 || updated.Lines[0].Quantity != 3 || updated.TotalQuantity != 4 {
		t.Fatalf("整行替换结果不正确：%+v", updated)
	}
	if updated.Status != purchaseenums.PurchaseStatusOrdered {
		t.Fatalf("替换后状态仍是 ordered，实际 %q", updated.Status)
	}
}

func TestPurchaseReceiptIncreasesStockAndWritesMovement(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-MOV-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 8, 9.9)})

	before := countMovements(t, f, v.ID)
	res := mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 5, "REQ-M1")
	if res.Kind != purchaseenums.ReceiptKindPurchase || res.OrderCode != "PO-MOV-1" {
		t.Fatalf("入库单应绑定采购单：%+v", res)
	}
	if res.MovementBatchID == "" {
		t.Fatalf("入库单应记下库存批次号")
	}
	if res.Status != purchaseenums.ReceiptStatusPosted {
		t.Fatalf("正常入库后单据状态应为 posted，实际 %q", res.Status)
	}

	if got := f.stockQty(t, v.ID, wh.ID); got != 5 {
		t.Fatalf("入库 5 后真源应为 5，实际 %d", got)
	}

	if got := countMovements(t, f, v.ID); got != before+1 {
		t.Fatalf("一次入库应写一条流水，实际 %d（原 %d）", got, before)
	}
	mv := latestMovement(t, f, v.ID)
	if mv.Direction != purchaseenums.DirectionIn || mv.Quantity != 5 || mv.Delta != 5 {
		t.Fatalf("流水方向 / 数量不正确：%+v", mv)
	}
	if mv.QuantityBefore != 0 || mv.QuantityAfter != 5 {
		t.Fatalf("流水前后值不正确：%+v", mv)
	}
	if mv.ReasonCode != "purchase_in" {
		t.Fatalf("入库流水原因应为采购入库（purchase_in），实际 %q", mv.ReasonCode)
	}
	if mv.SourceType != purchaseenums.MovementSourcePurchaseOrder || mv.SourceRef != "PO-MOV-1" {
		t.Fatalf("流水来源引用应指向采购单：%+v", mv)
	}
	if mv.BatchID != res.MovementBatchID {
		t.Fatalf("流水批次号应与入库单一致：流水 %s / 单据 %s", mv.BatchID, res.MovementBatchID)
	}

	if total := trueSourceTotal(t, f, v.ID); total != 5 {
		t.Fatalf("商品侧展示缓存应被同步为 5，实际 %d", total)
	}

	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "REQ-M2")
	if got := f.stockQty(t, v.ID, wh.ID); got != 8 {
		t.Fatalf("第二批入库后真源应为 8，实际 %d", got)
	}
	if got := countMovements(t, f, v.ID); got != before+2 {
		t.Fatalf("两批入库应各写一条流水，实际 %d（原 %d）", got, before)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusReceived {
		t.Fatalf("两批合计收满后状态应为 received，实际 %q", got)
	}
}

func TestPurchaseReceiptUpdatesVariantCost(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	p := mustProductPriced(t, f, "Tee", 199)
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-COST-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 6, 7.5)})

	res := mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "REQ-C1")
	if !res.CostUpdated || len(res.Items) != 1 || !res.Items[0].CostUpdated {
		t.Fatalf("入库后成本价应已写回：%+v", res)
	}
	if cost := variantCostIn(t, f, v.ID); cost == nil || *cost != 7.5 {
		t.Fatalf("成本价应为采购单价 7.5，实际 %v", cost)
	}

	if detail, derr := f.products.Get(ctx, &productdto.GetReq{ID: p.ID}); derr != nil ||
		detail.Variants[0].Price != 199 {
		t.Fatalf("入库不应改动售价：%v %+v", derr, detail)
	}

	override := 8.25
	res2, err := f.purchase.RegisterReceipt(ctx, &purchasedto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "REQ-C2",
		Lines: []purchasedto.ReceiptLineReq{{
			LineID: order.Lines[0].ID, Quantity: 3, UnitPrice: &override,
		}},
	})
	if err != nil {
		t.Fatalf("按本次到货价收货失败: %v", err)
	}
	if res2.Items[0].UnitPrice != 8.25 || !res2.Items[0].CostUpdated {
		t.Fatalf("本次到货价应写进入库行并回写成本：%+v", res2.Items[0])
	}
	if cost := variantCostIn(t, f, v.ID); cost == nil || *cost != 8.25 {
		t.Fatalf("成本价应被本次到货价覆盖为 8.25，实际 %v", cost)
	}

	history, herr := f.purchase.ListPurchaseHistory(ctx, &purchasedto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: bareSKU(v.SKUCode, wh.Code),
	})
	if herr != nil || len(history) != 2 {
		t.Fatalf("同一 SKU 应有两条进货记录：%v %+v", herr, history)
	}
}

func TestProductionInboundFromOwnFactory(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "HZ", "杭州仓", true)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", purchaseenums.SourceTypeInternal)
	external := mustPurchaseSource(t, f, "EXT_SUP", "外部供应商", purchaseenums.SourceTypeExternal)
	p := mustProductPriced(t, f, "Chair", 599)
	v := f.firstVariant(t, p.ID)

	cost := 128.5
	res, err := f.purchase.RegisterProductionInbound(ctx, &purchasedto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 7, UnitCost: &cost, RequestID: "PROD-1",
	})
	if err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	if res.Kind != purchaseenums.ReceiptKindProduction {
		t.Fatalf("生产入库的单据类型应为 production，实际 %q", res.Kind)
	}
	if res.OrderID != "" || res.OrderCode != "" {
		t.Fatalf("生产入库不应绑定采购单：%+v", res)
	}
	if purchaseOrderCount(t, f) != 0 {
		t.Fatalf("生产入库不应产生采购单")
	}

	if got := f.stockQty(t, v.ID, wh.ID); got != 7 {
		t.Fatalf("生产入库 7 后真源应为 7，实际 %d", got)
	}
	mv := latestMovement(t, f, v.ID)
	if mv.Direction != purchaseenums.DirectionIn || mv.ReasonCode != "production_in" ||
		mv.SourceType != purchaseenums.MovementSourceProduction || mv.SourceRef != res.Code {
		t.Fatalf("生产入库流水不正确：%+v（单据 %s）", mv, res.Code)
	}

	if got := variantCostIn(t, f, v.ID); got == nil || *got != 128.5 {
		t.Fatalf("生产入库成本价应为手工填写的 128.5，实际 %v", got)
	}

	_, err = f.purchase.RegisterProductionInbound(ctx, &purchasedto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: external.ID, VariantID: v.ID, Quantity: 1, UnitCost: &cost,
	})
	purchaseErr(t, err, purchaseenums.ErrProductionSourceNotInternal)

	_, err = f.purchase.RegisterProductionInbound(ctx, &purchasedto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, VariantID: v.ID, Quantity: 1,
	})
	purchaseErr(t, err, purchaseenums.ErrProductionCostInvalid)

	_, err = f.purchase.RegisterProductionInbound(ctx, &purchasedto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, Quantity: 1, UnitCost: &cost,
	})
	purchaseErr(t, err, purchaseenums.ErrProductionVariantRequired)

	if got := f.stockQty(t, v.ID, wh.ID); got != 7 {
		t.Fatalf("被拒绝的生产入库不应改动库存，实际 %d", got)
	}
}

func TestPurchaseHistoryBySKU(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", purchaseenums.SourceTypeInternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	other := mustProduct(t, f, "Mug")
	ov := f.firstVariant(t, other.ID)

	order := mustPurchaseOrder(t, f, "PO-HIS-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 5, 11.5)})
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 5, "REQ-H1")
	cost := 3.25
	if _, err := f.purchase.RegisterProductionInbound(ctx, &purchasedto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 2, UnitCost: &cost, RequestID: "PROD-H1",
	}); err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}

	order2 := mustPurchaseOrder(t, f, "PO-HIS-2", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(other, ov, 1, 2)})
	mustReceiveLine(t, f, order2.ID, order2.Lines[0].ID, 1, "REQ-H2")

	bare := bareSKU(v.SKUCode, wh.Code)
	history, err := f.purchase.ListPurchaseHistory(ctx, &purchasedto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: bare,
	})
	if err != nil {
		t.Fatalf("查进货历史失败: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("该 SKU 应有 2 条进货历史，实际 %d", len(history))
	}
	kinds := map[string]bool{}
	prices := map[float64]bool{}
	for _, h := range history {
		if h.SKUCode != bare || h.VariantID != v.ID {
			t.Fatalf("进货历史串了 SKU：%+v", h)
		}
		if h.SourceName == "" || h.WarehouseName != "苏州仓" {
			t.Fatalf("进货历史应带来源与收货仓：%+v", h)
		}
		kinds[h.Kind] = true
		prices[h.UnitPrice] = true
		if !h.CostUpdated {
			t.Fatalf("进货历史应标记成本价已写回：%+v", h)
		}
	}
	if !kinds[purchaseenums.ReceiptKindPurchase] || !kinds[purchaseenums.ReceiptKindProduction] {
		t.Fatalf("进货历史应同时覆盖采购收货与生产入库：%+v", kinds)
	}
	if !prices[11.5] || !prices[3.25] {
		t.Fatalf("进货历史应保留当时的单价快照：%+v", prices)
	}

	byVariant, err := f.purchase.ListPurchaseHistory(ctx, &purchasedto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, VariantID: v.ID,
	})
	if err != nil || len(byVariant) != 2 {
		t.Fatalf("按变体查进货历史结果不一致：%v %+v", err, byVariant)
	}
	bySource, err := f.purchase.ListPurchaseHistory(ctx, &purchasedto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: bare, SourceID: factory.ID,
	})
	if err != nil || len(bySource) != 1 || bySource[0].Kind != purchaseenums.ReceiptKindProduction {
		t.Fatalf("按货源收窄进货历史失败：%v %+v", err, bySource)
	}

	otherBare := bareSKU(ov.SKUCode, wh.Code)
	otherHistory, err := f.purchase.ListPurchaseHistory(ctx, &purchasedto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: otherBare,
	})
	if err != nil || len(otherHistory) != 1 || otherHistory[0].SKUCode != otherBare {
		t.Fatalf("另一个 SKU 的进货历史不正确：%v %+v", err, otherHistory)
	}
}

func TestPurchaseReceiptIdempotency(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-IDEM-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 10, 6)})

	first := mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 4, "IDEM-KEY-1")
	replay, err := receiveLine(t, f, order.ID, order.Lines[0].ID, 4, "IDEM-KEY-1")
	if err != nil {
		t.Fatalf("同一幂等键重放应命中既有入库单而不是失败：%v", err)
	}
	if !replay.Idempotent || replay.ID != first.ID || replay.Code != first.Code {
		t.Fatalf("重放应原样返回既有入库单：first=%+v replay=%+v", first, replay)
	}
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 4 {
		t.Fatalf("重放不应二次累加已入库数量，实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 4 {
		t.Fatalf("重放不应二次加库存，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("重放不应产生第二张入库单，实际 %d", got)
	}
	if got := countMovements(t, f, v.ID); got != 1 {
		t.Fatalf("重放不应产生第二条流水，实际 %d", got)
	}

	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "")
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "")
	if got := orderReceiptCount(t, f, order.ID); got != 3 {
		t.Fatalf("未给幂等键时应各记一张入库单，实际 %d", got)
	}
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 10 {
		t.Fatalf("三张单合计应收到 10，实际 %d", got)
	}

	_, err = receiveLine(t, f, order.ID, order.Lines[0].ID, 1, "bad key!")
	purchaseErr(t, err, purchaseenums.ErrReceiptRequestInvalid)

	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", purchaseenums.SourceTypeInternal)
	cost := 1.0
	makeProd := func() (*purchasedto.ReceiptResp, error) {
		return f.purchase.RegisterProductionInbound(context.Background(), &purchasedto.ProductionInboundReq{
			ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
			ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
			Quantity: 2, UnitCost: &cost, RequestID: "PROD-IDEM-1",
		})
	}
	prodFirst, err := makeProd()
	if err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	prodReplay, err := makeProd()
	if err != nil || !prodReplay.Idempotent || prodReplay.ID != prodFirst.ID {
		t.Fatalf("生产入库的幂等重放不正确：%v %+v", err, prodReplay)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 12 {
		t.Fatalf("生产入库重放后真源应为 12（10 + 2），实际 %d", got)
	}
}

func TestPurchaseReceiptConcurrentNoDoubleReceive(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", purchaseenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-RACE-1", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p, v, 10, 5)})
	lineID := order.Lines[0].ID

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
		errs    []error
	)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := receiveLine(t, f, order.ID, lineID, 10, fmt.Sprintf("RACE-%d", index))
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
				return
			}
			errs = append(errs, err)
		}(i)
	}
	wg.Wait()

	if success != 1 {
		t.Fatalf("并发收满应恰好成功一次，实际 %d 次（错误：%v）", success, errs)
	}
	for _, err := range errs {

		if err.Error() != purchaseenums.ErrReceiptOverReceive &&
			err.Error() != purchaseenums.ErrReceiptOrderDone {
			t.Fatalf("并发失败方的错误应为超收 / 已收满，实际 %v", err)
		}
	}
	if got := lineReceivedIn(t, f, lineID); got != 10 {
		t.Fatalf("并发后已入库数量应恰好为 10，实际 %d", got)
	}
	if got := f.stockQty(t, v.ID, wh.ID); got != 10 {
		t.Fatalf("并发后真源库存应恰好为 10，实际 %d", got)
	}
	if got := orderReceiptCount(t, f, order.ID); got != 1 {
		t.Fatalf("并发后应只有一张入库单，实际 %d", got)
	}
	if got := countMovements(t, f, v.ID); got != 1 {
		t.Fatalf("并发后应只有一条流水，实际 %d", got)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != purchaseenums.PurchaseStatusReceived {
		t.Fatalf("并发收满后状态应为 received，实际 %q", got)
	}
}

func TestFinishedAndCancelledPurchaseOrderAreNotEditable(t *testing.T) {
	f := newPurchaseFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	wh := f.createWarehouse(t, "TW", "终态测试仓", true)
	p1 := mustProduct(t, f, "终态采购商品")
	v1 := f.firstVariant(t, p1.ID)
	src := mustPurchaseSource(t, f, "EXT_TW", "终态测试货源", purchaseenums.SourceTypeExternal)

	cancelled := mustPurchaseOrder(t, f, "PO-2026-CXL", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 5, 10)})
	if _, err := f.purchase.CancelPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{ID: cancelled.ID}); err != nil {
		t.Fatalf("取消采购单失败: %v", err)
	}

	_, err := f.purchase.UpdatePurchaseOrder(ctx, &purchasedto.UpdatePurchaseOrderReq{
		ID: cancelled.ID, ReplaceLines: true,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 1, 10)},
	})
	if err == nil {
		t.Fatal("已取消的采购单不该还能整行替换 —— 那会让结案的单变回可流转的形状")
	}
	purchaseErr(t, err, purchaseenums.ErrPurchaseStatusInvalid)

	note := "偷偷改一下"
	if _, uerr := f.purchase.UpdatePurchaseOrder(ctx, &purchasedto.UpdatePurchaseOrderReq{
		ID: cancelled.ID, Remark: &note,
	}); uerr == nil {
		t.Fatal("已取消的采购单不该还能改备注")
	}

	finished := mustPurchaseOrder(t, f, "PO-2026-FIN", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 3, 10)})

	for i, line := range finished.Lines {
		mustReceiveLine(t, f, finished.ID, line.ID, line.Quantity, "FIN-REQ-"+string(rune('A'+i)))
	}
	if _, ferr := f.purchase.FinishPurchaseOrder(ctx, &purchasedto.PurchaseStatusReq{ID: finished.ID}); ferr != nil {
		t.Fatalf("结案失败: %v", ferr)
	}
	if _, uerr := f.purchase.UpdatePurchaseOrder(ctx, &purchasedto.UpdatePurchaseOrderReq{
		ID: finished.ID, Remark: &note,
	}); uerr == nil {
		t.Fatal("已结案的采购单不该还能改")
	}

	draft := mustPurchaseOrder(t, f, "PO-2026-DFT", src.ID, wh.ID,
		[]purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 2, 10)})
	if _, derr := f.purchase.UpdatePurchaseOrder(ctx, &purchasedto.UpdatePurchaseOrderReq{
		ID: draft.ID, ReplaceLines: true,
		Lines: []purchasedto.PurchaseLineReq{purchaseLine(p1, v1, 4, 10)},
	}); derr != nil {
		t.Fatalf("草稿单应可编辑，实际 %v", derr)
	}
}
