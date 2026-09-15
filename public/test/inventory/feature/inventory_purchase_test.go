// Package feature inventory 模块 feature 测试 —— 采购单与入库（issue #18）。
//
// 覆盖本票六条验收（真实 PostgreSQL + 生产迁移与 seed + 真实 service + 真实商品模块）：
//  1. 可建采购单与采购行（SKU、数量、单价），已入库数量可原子递增；
//  2. 采购单状态由「已入库数量 与 采购数量」推导；
//  3. 登记入库后对应 SKU 库存增加并写流水（原因为采购入库）；
//  4. 入库时以采购单价更新 SKU 成本价；
//  5. 自家工厂走生产入库，无采购单，成本价手工填写；
//  6. 后台可查某 SKU 的进货历史。
//
// 另覆盖三组容易踩的边界：
//
//	· 幂等 / 可重放保护 —— 同一个 requestId 重复登记只产生一张入库单，库存只加一次；
//	· 超收与并发 —— 守卫写在 UPDATE 的 WHERE 里（不是先读后写），并发两次收满只成功一次；
//	· 死线 —— 入库走的是 #16 的 ChangeStock（真源行锁 + 流水 + 原因字典 + 来源引用），
//	  入库后可用量仍是 inventory_stocks 的真值，断言一律直查真源与流水，不读展示缓存。
package feature

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
)

// —— 小工具 ——

// mustPurchaseSource 建一个货源（采购单的来源就是 #17 的货源）。
func mustPurchaseSource(t *testing.T, f *invFixture, code, name, sourceType string) *inventorydto.SourceResp {
	t.Helper()
	res, err := f.inventory.CreateSource(context.Background(), &inventorydto.CreateSourceReq{
		ProjectID: f.projectID, Code: code, Name: name, Type: sourceType,
	})
	if err != nil {
		t.Fatalf("建货源 %s 失败: %v", code, err)
	}
	return res
}

// mustProductPriced 建一个带售价的商品（用于断言「成本价更新不动售价」）。
func mustProductPriced(t *testing.T, f *invFixture, name string, price float64) *productdto.ProductResp {
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

// purchaseLine 造一条采购行（SKU × 数量 × 单价）。
func purchaseLine(p *productdto.ProductResp, v *productdto.VariantResp, quantity int, unitPrice float64) inventorydto.PurchaseLineReq {
	return inventorydto.PurchaseLineReq{
		VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode,
		Quantity: quantity, UnitPrice: unitPrice,
	}
}

// mustPurchaseOrder 建采购单并要求成功。
func mustPurchaseOrder(t *testing.T, f *invFixture, code, sourceID, warehouseID string,
	lines []inventorydto.PurchaseLineReq) *inventorydto.PurchaseOrderResp {
	t.Helper()
	res, err := f.inventory.CreatePurchaseOrder(context.Background(), &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: code, SourceID: sourceID, WarehouseID: warehouseID, Lines: lines,
	})
	if err != nil {
		t.Fatalf("建采购单 %s 失败: %v", code, err)
	}
	return res
}

// getPurchaseOrder 读采购单详情（失败即 fail）。
func getPurchaseOrder(t *testing.T, f *invFixture, id string) *inventorydto.PurchaseOrderResp {
	t.Helper()
	res, err := f.inventory.GetPurchaseOrder(context.Background(), &inventorydto.GetPurchaseOrderReq{ID: id})
	if err != nil {
		t.Fatalf("读采购单失败: %v", err)
	}
	return res
}

// receiveLine 登记一条采购行的收货（返回原始错误，供拒绝路径断言）。
func receiveLine(t *testing.T, f *invFixture, orderID, lineID string, quantity int, requestID string) (
	*inventorydto.ReceiptResp, error) {
	t.Helper()
	return f.inventory.RegisterReceipt(context.Background(), &inventorydto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: orderID, RequestID: requestID,
		Lines: []inventorydto.ReceiptLineReq{{LineID: lineID, Quantity: quantity}},
	})
}

// mustReceiveLine 登记收货并要求成功。
func mustReceiveLine(t *testing.T, f *invFixture, orderID, lineID string, quantity int,
	requestID string) *inventorydto.ReceiptResp {
	t.Helper()
	res, err := receiveLine(t, f, orderID, lineID, quantity, requestID)
	if err != nil {
		t.Fatalf("登记入库 %d 失败: %v", quantity, err)
	}
	return res
}

// purchaseErr 断言业务错误（service 层用 enums 消息键当错误文本）。
func purchaseErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("应返回错误 %s，实际成功", want)
	}
	if err.Error() != want {
		t.Fatalf("应返回错误 %s，实际 %v", want, err)
	}
}

// lineReceivedIn 直读采购行的已入库数量（列真值，不走 service 推导）。
func lineReceivedIn(t *testing.T, f *invFixture, lineID string) int {
	t.Helper()
	var qty int
	if err := f.db.Raw("SELECT received_quantity FROM inventory_purchase_order_lines WHERE id = ?", lineID).
		Scan(&qty).Error; err != nil {
		t.Fatalf("读采购行已入库数量失败: %v", err)
	}
	return qty
}

// purchaseStatusIn 直读采购单状态列。
func purchaseStatusIn(t *testing.T, f *invFixture, orderID string) string {
	t.Helper()
	var status string
	if err := f.db.Raw("SELECT status FROM inventory_purchase_orders WHERE id = ?", orderID).
		Scan(&status).Error; err != nil {
		t.Fatalf("读采购单状态失败: %v", err)
	}
	return status
}

// orderReceiptCount 某采购单已开的入库单数。
func orderReceiptCount(t *testing.T, f *invFixture, orderID string) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_purchase_receipts WHERE order_id = ?", orderID).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计入库单失败: %v", err)
	}
	return n
}

// purchaseOrderCount 本工程采购单总数。
func purchaseOrderCount(t *testing.T, f *invFixture) int {
	t.Helper()
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM inventory_purchase_orders WHERE project_id = ?", f.projectID).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计采购单失败: %v", err)
	}
	return n
}

// variantCostIn 直读商品侧成本价（跨模块列，断言必须直查）。
func variantCostIn(t *testing.T, f *invFixture, variantID string) *float64 {
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

// purchaseMovement 一条入库流水（只取本票关心的列）。
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

// latestMovement 某变体最近一条流水。
func latestMovement(t *testing.T, f *invFixture, variantID string) purchaseMovement {
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

// TestPurchaseOrderCreateWithAtomicReceive 验收 1 + 验收 2：
// 可建采购单与采购行，已入库数量原子递增；采购单状态由「已入库数量 与 采购数量」推导。
func TestPurchaseOrderCreateWithAtomicReceive(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	p1 := mustProduct(t, f, "Tee")
	v1 := f.firstVariant(t, p1.ID)
	p2 := mustProduct(t, f, "Mug")
	v2 := f.firstVariant(t, p2.ID)

	// 入参校验：空行 / 重复 SKU / 数量 / 单价 / 来源。
	_, err := f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-EMPTY", SourceID: src.ID, Lines: nil,
	})
	purchaseErr(t, err, inventoryenums.ErrPurchaseLinesRequired)
	_, err = f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-DUP", SourceID: src.ID,
		Lines: []inventorydto.PurchaseLineReq{
			purchaseLine(p1, v1, 1, 1), purchaseLine(p1, v1, 2, 1),
		},
	})
	purchaseErr(t, err, inventoryenums.ErrPurchaseLineDuplicate)
	_, err = f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-QTY", SourceID: src.ID,
		Lines: []inventorydto.PurchaseLineReq{purchaseLine(p1, v1, 0, 1)},
	})
	purchaseErr(t, err, inventoryenums.ErrPurchaseQuantityInvalid)
	_, err = f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-PRICE", SourceID: src.ID,
		Lines: []inventorydto.PurchaseLineReq{purchaseLine(p1, v1, 1, 0)},
	})
	purchaseErr(t, err, inventoryenums.ErrPurchasePriceInvalid)
	_, err = f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-NOSRC", Lines: []inventorydto.PurchaseLineReq{purchaseLine(p1, v1, 1, 1)},
	})
	purchaseErr(t, err, inventoryenums.ErrPurchaseSourceRequired)

	// 正常下单：单头 + 结构化行；行合计与状态（未入库）。
	order := mustPurchaseOrder(t, f, "po-2026-001", src.ID, wh.ID, []inventorydto.PurchaseLineReq{
		purchaseLine(p1, v1, 10, 12.5),
		purchaseLine(p2, v2, 4, 3.25),
	})
	if order.Code != "PO-2026-001" {
		t.Fatalf("采购单号应归一为大写，实际 %q", order.Code)
	}
	if order.Status != inventoryenums.PurchaseStatusPending {
		t.Fatalf("新建采购单状态应为 pending，实际 %q", order.Status)
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
	// 单号工程内唯一（大小写不敏感）。
	_, err = f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO-2026-001", SourceID: src.ID,
		Lines: []inventorydto.PurchaseLineReq{purchaseLine(p1, v1, 1, 1)},
	})
	purchaseErr(t, err, inventoryenums.ErrPurchaseCodeTaken)
	// 单号非法。
	_, err = f.inventory.CreatePurchaseOrder(ctx, &inventorydto.CreatePurchaseOrderReq{
		ProjectID: f.projectID, Code: "PO 2026/001", SourceID: src.ID,
		Lines: []inventorydto.PurchaseLineReq{purchaseLine(p1, v1, 1, 1)},
	})
	purchaseErr(t, err, inventoryenums.ErrPurchaseCodeInvalid)

	line1, line2 := order.Lines[0], order.Lines[1]
	// 同一请求里重复提交同一采购行必须拒绝，避免一张入库单出现重复行并让单价 / 数量语义不明确。
	_, err = f.inventory.RegisterReceipt(ctx, &inventorydto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "REQ-DUP-LINE",
		Lines: []inventorydto.ReceiptLineReq{
			{LineID: line1.ID, Quantity: 1}, {LineID: line1.ID, Quantity: 1},
		},
	})
	purchaseErr(t, err, inventoryenums.ErrReceiptLineDuplicate)

	// 分批入库：先收 6（部分入库），已入库数量落库、状态跟着变。
	mustReceiveLine(t, f, order.ID, line1.ID, 6, "REQ-A1")
	if got := lineReceivedIn(t, f, line1.ID); got != 6 {
		t.Fatalf("已入库数量应为 6（原子递增的结果），实际 %d", got)
	}
	got := getPurchaseOrder(t, f, order.ID)
	if got.Status != inventoryenums.PurchaseStatusPartial {
		t.Fatalf("收 6/10 后状态应为 partial，实际 %q", got.Status)
	}
	if got.Lines[0].ReceivedQuantity != 6 || got.Lines[0].OutstandingQuantity != 4 {
		t.Fatalf("部分入库后行数量不正确：%+v", got.Lines[0])
	}
	if got.ReceivedQuantity != 6 || got.TotalQuantity != 14 {
		t.Fatalf("单据汇总不正确：收到 %d / 合计 %d", got.ReceivedQuantity, got.TotalQuantity)
	}

	// 超收拒绝：余量只有 4，收 5 被拒（守卫在 UPDATE 的 WHERE 里），且记账与库存都不动。
	beforeStock := f.stockQty(t, v1.ID, wh.ID)
	_, err = receiveLine(t, f, order.ID, line1.ID, 5, "REQ-A2")
	purchaseErr(t, err, inventoryenums.ErrReceiptOverReceive)
	if got := lineReceivedIn(t, f, line1.ID); got != 6 {
		t.Fatalf("超收被拒后已入库数量不应变化，实际 %d", got)
	}
	if got := f.stockQty(t, v1.ID, wh.ID); got != beforeStock {
		t.Fatalf("超收被拒后库存不应变化，实际 %d（原 %d）", got, beforeStock)
	}
	// 同一次提交里行不存在 / 数量非法。
	_, err = receiveLine(t, f, order.ID, "00000000-0000-0000-0000-000000000000", 1, "REQ-A3")
	purchaseErr(t, err, inventoryenums.ErrPurchaseLineNotFound)
	_, err = receiveLine(t, f, order.ID, line1.ID, 0, "REQ-A4")
	purchaseErr(t, err, inventoryenums.ErrReceiptQuantityInvalid)

	// 收满两张行 → 状态推导为 received。
	mustReceiveLine(t, f, order.ID, line1.ID, 4, "REQ-A5")
	if got := purchaseStatusIn(t, f, order.ID); got != inventoryenums.PurchaseStatusPartial {
		t.Fatalf("还有一行没收满时状态应保持 partial，实际 %q", got)
	}
	mustReceiveLine(t, f, order.ID, line2.ID, 4, "REQ-A6")
	if got := purchaseStatusIn(t, f, order.ID); got != inventoryenums.PurchaseStatusReceived {
		t.Fatalf("全部收满后状态应为 received，实际 %q", got)
	}
	// 已收满的单不再受理。
	_, err = receiveLine(t, f, order.ID, line2.ID, 1, "REQ-A7")
	purchaseErr(t, err, inventoryenums.ErrReceiptOrderDone)

	// 状态筛选维度可用。
	list, lerr := f.inventory.ListPurchaseOrders(ctx, &inventorydto.ListPurchaseOrderReq{
		ProjectID: f.projectID, Status: inventoryenums.PurchaseStatusReceived,
	})
	if lerr != nil || len(list) != 1 || list[0].ID != order.ID {
		t.Fatalf("按 received 筛选应命中本单：%v %+v", lerr, list)
	}
	_, lerr = f.inventory.ListPurchaseOrders(ctx, &inventorydto.ListPurchaseOrderReq{
		ProjectID: f.projectID, Status: "shipped",
	})
	purchaseErr(t, lerr, inventoryenums.ErrPurchaseStatusInvalid)

	// 数据层兜底：已入库数量不可能超过采购数量（绕过 service 直写也写不进去）。
	if werr := f.db.Exec("UPDATE inventory_purchase_order_lines SET received_quantity = quantity + 1 WHERE id = ?",
		line1.ID).Error; werr == nil {
		t.Fatalf("已入库数量超过采购数量的直写应被 CHECK 约束拒绝")
	}

	// 已入库的行不可再改（改了状态推导就不成立）；未入库的单可以整行替换。
	_, err = f.inventory.UpdatePurchaseOrder(ctx, &inventorydto.UpdatePurchaseOrderReq{
		ID: order.ID, ReplaceLines: true,
		Lines: []inventorydto.PurchaseLineReq{purchaseLine(p1, v1, 20, 9)},
	})
	purchaseErr(t, err, inventoryenums.ErrPurchaseLinesLocked)
	draft := mustPurchaseOrder(t, f, "PO-2026-002", src.ID, wh.ID, []inventorydto.PurchaseLineReq{
		purchaseLine(p1, v1, 2, 5),
	})
	updated, uerr := f.inventory.UpdatePurchaseOrder(ctx, &inventorydto.UpdatePurchaseOrderReq{
		ID: draft.ID, ReplaceLines: true,
		Lines: []inventorydto.PurchaseLineReq{purchaseLine(p1, v1, 3, 5.5), purchaseLine(p2, v2, 1, 2)},
	})
	if uerr != nil {
		t.Fatalf("未入库的单应可整行替换：%v", uerr)
	}
	if len(updated.Lines) != 2 || updated.Lines[0].Quantity != 3 || updated.TotalQuantity != 4 {
		t.Fatalf("整行替换结果不正确：%+v", updated)
	}
	if updated.Status != inventoryenums.PurchaseStatusPending {
		t.Fatalf("替换后状态仍是 pending，实际 %q", updated.Status)
	}
}

// TestPurchaseReceiptIncreasesStockAndWritesMovement 验收 3：
// 登记入库后对应 SKU 库存增加并写流水（原因为采购入库，来源引用 = 采购单号）。
func TestPurchaseReceiptIncreasesStockAndWritesMovement(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-MOV-1", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 8, 9.9)})

	before := countMovements(t, f, v.ID)
	res := mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 5, "REQ-M1")
	if res.Kind != inventoryenums.ReceiptKindPurchase || res.OrderCode != "PO-MOV-1" {
		t.Fatalf("入库单应绑定采购单：%+v", res)
	}
	if res.MovementBatchID == "" {
		t.Fatalf("入库单应记下库存批次号")
	}
	if res.Status != inventoryenums.ReceiptStatusPosted {
		t.Fatalf("正常入库后单据状态应为 posted，实际 %q", res.Status)
	}

	// 真源加上了（直查 inventory_stocks，不读展示缓存）。
	if got := f.stockQty(t, v.ID, wh.ID); got != 5 {
		t.Fatalf("入库 5 后真源应为 5，实际 %d", got)
	}
	// 流水：方向 in + 原因 purchase_in + 来源 purchase_order/采购单号 + 批次号一致。
	if got := countMovements(t, f, v.ID); got != before+1 {
		t.Fatalf("一次入库应写一条流水，实际 %d（原 %d）", got, before)
	}
	mv := latestMovement(t, f, v.ID)
	if mv.Direction != inventoryenums.DirectionIn || mv.Quantity != 5 || mv.Delta != 5 {
		t.Fatalf("流水方向 / 数量不正确：%+v", mv)
	}
	if mv.QuantityBefore != 0 || mv.QuantityAfter != 5 {
		t.Fatalf("流水前后值不正确：%+v", mv)
	}
	if mv.ReasonCode != "purchase_in" {
		t.Fatalf("入库流水原因应为采购入库（purchase_in），实际 %q", mv.ReasonCode)
	}
	if mv.SourceType != inventoryenums.MovementSourcePurchaseOrder || mv.SourceRef != "PO-MOV-1" {
		t.Fatalf("流水来源引用应指向采购单：%+v", mv)
	}
	if mv.BatchID != res.MovementBatchID {
		t.Fatalf("流水批次号应与入库单一致：流水 %s / 单据 %s", mv.BatchID, res.MovementBatchID)
	}
	// 商品侧库存展示缓存（提交之后的独立同步）也跟上了。
	if total := trueSourceTotal(t, f, v.ID); total != 5 {
		t.Fatalf("商品侧展示缓存应被同步为 5，实际 %d", total)
	}
	// 第二批入库：同一行继续累加（已入库数量是累加值，不是覆盖值）。
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "REQ-M2")
	if got := f.stockQty(t, v.ID, wh.ID); got != 8 {
		t.Fatalf("第二批入库后真源应为 8，实际 %d", got)
	}
	if got := countMovements(t, f, v.ID); got != before+2 {
		t.Fatalf("两批入库应各写一条流水，实际 %d（原 %d）", got, before)
	}
	if got := purchaseStatusIn(t, f, order.ID); got != inventoryenums.PurchaseStatusReceived {
		t.Fatalf("两批合计收满后状态应为 received，实际 %q", got)
	}
}

// TestPurchaseReceiptUpdatesVariantCost 验收 4：
// 入库时以采购单价更新 SKU 成本价（本次到货价可覆盖采购价；售价不动）。
func TestPurchaseReceiptUpdatesVariantCost(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	p := mustProductPriced(t, f, "Tee", 199)
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-COST-1", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 6, 7.5)})

	// 采购单价写进成本价。
	res := mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "REQ-C1")
	if !res.CostUpdated || len(res.Items) != 1 || !res.Items[0].CostUpdated {
		t.Fatalf("入库后成本价应已写回：%+v", res)
	}
	if cost := variantCostIn(t, f, v.ID); cost == nil || *cost != 7.5 {
		t.Fatalf("成本价应为采购单价 7.5，实际 %v", cost)
	}
	// 售价不被动（成本口径与售价口径互不干扰）。
	if detail, derr := f.products.Get(ctx, &productdto.GetReq{ID: p.ID}); derr != nil ||
		detail.Variants[0].Price != 199 {
		t.Fatalf("入库不应改动售价：%v %+v", derr, detail)
	}

	// 本批实际到货价覆盖采购单价（同一张单的第一行快照仍是原采购价）。
	override := 8.25
	res2, err := f.inventory.RegisterReceipt(ctx, &inventorydto.RegisterReceiptReq{
		ProjectID: f.projectID, OrderID: order.ID, RequestID: "REQ-C2",
		Lines: []inventorydto.ReceiptLineReq{{
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
	// 进货历史里的单价是「当时快照」，两行不同价可分别回溯（验收 6 的数据基础）。
	history, herr := f.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: v.SKUCode,
	})
	if herr != nil || len(history) != 2 {
		t.Fatalf("同一 SKU 应有两条进货记录：%v %+v", herr, history)
	}
}

// TestProductionInboundFromOwnFactory 验收 5：
// 自家工厂走生产入库，无采购单，成本价手工填写。
func TestProductionInboundFromOwnFactory(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "HZ", "杭州仓", true)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", inventoryenums.SourceTypeInternal)
	external := mustPurchaseSource(t, f, "EXT_SUP", "外部供应商", inventoryenums.SourceTypeExternal)
	p := mustProductPriced(t, f, "Chair", 599)
	v := f.firstVariant(t, p.ID)

	// 无采购单 ✓ / 成本价手工填写 ✓ / 来源必须是内部货源。
	cost := 128.5
	res, err := f.inventory.RegisterProductionInbound(ctx, &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 7, UnitCost: &cost, RequestID: "PROD-1",
	})
	if err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	if res.Kind != inventoryenums.ReceiptKindProduction {
		t.Fatalf("生产入库的单据类型应为 production，实际 %q", res.Kind)
	}
	if res.OrderID != "" || res.OrderCode != "" {
		t.Fatalf("生产入库不应绑定采购单：%+v", res)
	}
	if purchaseOrderCount(t, f) != 0 {
		t.Fatalf("生产入库不应产生采购单")
	}
	// 库存 + 流水（原因 = 生产入库，来源引用 = 本入库单号）。
	if got := f.stockQty(t, v.ID, wh.ID); got != 7 {
		t.Fatalf("生产入库 7 后真源应为 7，实际 %d", got)
	}
	mv := latestMovement(t, f, v.ID)
	if mv.Direction != inventoryenums.DirectionIn || mv.ReasonCode != "production_in" ||
		mv.SourceType != inventoryenums.MovementSourceProduction || mv.SourceRef != res.Code {
		t.Fatalf("生产入库流水不正确：%+v（单据 %s）", mv, res.Code)
	}
	// 成本价用手工填的那个数。
	if got := variantCostIn(t, f, v.ID); got == nil || *got != 128.5 {
		t.Fatalf("生产入库成本价应为手工填写的 128.5，实际 %v", got)
	}

	// 外部供应商不能走生产入库（它的路径是采购单）。
	_, err = f.inventory.RegisterProductionInbound(ctx, &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: external.ID, VariantID: v.ID, Quantity: 1, UnitCost: &cost,
	})
	purchaseErr(t, err, inventoryenums.ErrProductionSourceNotInternal)
	// 成本价必须手工填写（缺了就写不出成本口径）。
	_, err = f.inventory.RegisterProductionInbound(ctx, &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, VariantID: v.ID, Quantity: 1,
	})
	purchaseErr(t, err, inventoryenums.ErrProductionCostInvalid)
	// 变体必填。
	_, err = f.inventory.RegisterProductionInbound(ctx, &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, Quantity: 1, UnitCost: &cost,
	})
	purchaseErr(t, err, inventoryenums.ErrProductionVariantRequired)
	// 库存在失败路径上没有被加上去。
	if got := f.stockQty(t, v.ID, wh.ID); got != 7 {
		t.Fatalf("被拒绝的生产入库不应改动库存，实际 %d", got)
	}
}

// TestPurchaseHistoryBySKU 验收 6：
// 后台可查某 SKU 的进货历史（采购收货与生产入库都在同一张历史里）。
func TestPurchaseHistoryBySKU(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", inventoryenums.SourceTypeInternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	other := mustProduct(t, f, "Mug")
	ov := f.firstVariant(t, other.ID)

	order := mustPurchaseOrder(t, f, "PO-HIS-1", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 5, 11.5)})
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 5, "REQ-H1")
	cost := 3.25
	if _, err := f.inventory.RegisterProductionInbound(ctx, &inventorydto.ProductionInboundReq{
		ProjectID: f.projectID, SourceID: factory.ID, WarehouseID: wh.ID,
		ProductID: p.ID, VariantID: v.ID, SKUCode: v.SKUCode,
		Quantity: 2, UnitCost: &cost, RequestID: "PROD-H1",
	}); err != nil {
		t.Fatalf("生产入库失败: %v", err)
	}
	// 另一个 SKU 的进货不应混进来。
	order2 := mustPurchaseOrder(t, f, "PO-HIS-2", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(other, ov, 1, 2)})
	mustReceiveLine(t, f, order2.ID, order2.Lines[0].ID, 1, "REQ-H2")

	history, err := f.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: v.SKUCode,
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
		if h.SKUCode != v.SKUCode || h.VariantID != v.ID {
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
	if !kinds[inventoryenums.ReceiptKindPurchase] || !kinds[inventoryenums.ReceiptKindProduction] {
		t.Fatalf("进货历史应同时覆盖采购收货与生产入库：%+v", kinds)
	}
	if !prices[11.5] || !prices[3.25] {
		t.Fatalf("进货历史应保留当时的单价快照：%+v", prices)
	}
	// 按变体 id 查等价；按货源收窄只留对应来源。
	byVariant, err := f.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, VariantID: v.ID,
	})
	if err != nil || len(byVariant) != 2 {
		t.Fatalf("按变体查进货历史结果不一致：%v %+v", err, byVariant)
	}
	bySource, err := f.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: v.SKUCode, SourceID: factory.ID,
	})
	if err != nil || len(bySource) != 1 || bySource[0].Kind != inventoryenums.ReceiptKindProduction {
		t.Fatalf("按货源收窄进货历史失败：%v %+v", err, bySource)
	}
	// 另一个 SKU 的历史独立。
	otherHistory, err := f.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: f.projectID, SKUCode: ov.SKUCode,
	})
	if err != nil || len(otherHistory) != 1 || otherHistory[0].SKUCode != ov.SKUCode {
		t.Fatalf("另一个 SKU 的进货历史不正确：%v %+v", err, otherHistory)
	}
}

// TestPurchaseReceiptIdempotency 幂等 / 可重放保护：
// 同一个幂等键重复登记只产生一张入库单，库存与已入库数量都只变一次。
func TestPurchaseReceiptIdempotency(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-IDEM-1", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 10, 6)})

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

	// 不给幂等键时每次都是一张新单（幂等保护不是「一刀切拒绝重复提交」）。
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "")
	mustReceiveLine(t, f, order.ID, order.Lines[0].ID, 3, "")
	if got := orderReceiptCount(t, f, order.ID); got != 3 {
		t.Fatalf("未给幂等键时应各记一张入库单，实际 %d", got)
	}
	if got := lineReceivedIn(t, f, order.Lines[0].ID); got != 10 {
		t.Fatalf("三张单合计应收到 10，实际 %d", got)
	}
	// 幂等键非法直接拒绝。
	_, err = receiveLine(t, f, order.ID, order.Lines[0].ID, 1, "bad key!")
	purchaseErr(t, err, inventoryenums.ErrReceiptRequestInvalid)
	// 生产入库同样受幂等键保护。
	factory := mustPurchaseSource(t, f, "OWN_FACTORY", "自家杭州工厂", inventoryenums.SourceTypeInternal)
	cost := 1.0
	makeProd := func() (*inventorydto.ReceiptResp, error) {
		return f.inventory.RegisterProductionInbound(context.Background(), &inventorydto.ProductionInboundReq{
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

// TestPurchaseReceiptConcurrentNoDoubleReceive 并发的原子递增：
// 两个请求同时把同一行收满，只能有一个成功 —— 守卫写在 UPDATE 的 WHERE 里，
// 不是「先读余量再写」，因此不存在「都读到还有余量」的窗口。
func TestPurchaseReceiptConcurrentNoDoubleReceive(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "苏州仓", true)
	src := mustPurchaseSource(t, f, "EXT_SUP", "苏州通达电子", inventoryenums.SourceTypeExternal)
	p := mustProduct(t, f, "Tee")
	v := f.firstVariant(t, p.ID)
	order := mustPurchaseOrder(t, f, "PO-RACE-1", src.ID, wh.ID,
		[]inventorydto.PurchaseLineReq{purchaseLine(p, v, 10, 5)})
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
		// 输的那一方要么在锁内被超收守卫拒，要么在锁外看到单据已收满。
		if err.Error() != inventoryenums.ErrReceiptOverReceive &&
			err.Error() != inventoryenums.ErrReceiptOrderDone {
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
	if got := purchaseStatusIn(t, f, order.ID); got != inventoryenums.PurchaseStatusReceived {
		t.Fatalf("并发收满后状态应为 received，实际 %q", got)
	}
}
