// Package feature order 模块 feature 测试 —— 订单行成本快照改取「归属仓当前成本」
// （2026-09-19 商品域评审收口项，docs/14 §1.3 / §9.3）。
//
// 口径：
//
//	· 订单行成本 = 该变体在**该行归属仓**的当前成本。订单不指定仓库（出库由库存域按
//	  归属仓解析：默认仓），所以这条链路走的就是默认仓那一条；
//	· 未核算（inventory_stocks.cost_price IS NULL）时订单行成本**留空**——
//	  绝不用 0 冒充（0 是合法的显式成本：赠品 / 内部划拨）；
//	· 成本是下单那一刻的快照：之后改库存行成本不影响历史订单（与出库流水的
//	  unit_cost 留痕同向）。
//
// 断言读订单详情（经 service 的对外形状），并直查订单项真源列佐证 NULL 与 0 的区别。
package feature

import (
	"context"
	"database/sql"
	"testing"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	orderdto "go_wp/internal/module/order/dto"
	productdto "go_wp/internal/module/product/dto"
)

// setOwningWarehouseCost 把某变体在默认仓的当前成本写成 cost（走真实入库路径的显式成本）。
func setOwningWarehouseCost(t *testing.T, f *orderFixture, productID, variantID, skuCode string, cost float64) {
	t.Helper()
	if _, err := f.inventory.ChangeStock(context.Background(), &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		SourceType: "manual", SourceRef: "PO-COST",
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: variantID, ProductID: productID, SKUCode: skuCode,
			WarehouseID: f.warehouse, Quantity: 1, CostPrice: &cost,
		}},
	}); err != nil {
		t.Fatalf("写默认仓成本（%v）失败: %v", cost, err)
	}
}

// addFlavour 建一个「口味」变体（同一商品、独立 SKU）并把成本与货写进默认仓。
//
// docs/14 §9.3 的常见形态是「一个商品十几个口味，仓库侧其实是一个价」——
// 成本记录可以共用（十来行写同样的值），但**聚合键仍是 variant_id**：本 helper 让
// 每个口味各有一行库存、各写同一个成本值。
func addFlavour(t *testing.T, f *orderFixture, productID, sku string, price, cost float64, stock int) string {
	t.Helper()
	ctx := context.Background()
	v, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: productID, Price: &price, CostPrice: &cost,
		SKUCode: sku, WarehouseID: f.warehouse,
	})
	if err != nil {
		t.Fatalf("建口味变体 %s 失败: %v", sku, err)
	}
	if _, err := f.inventory.ChangeStock(ctx, &inventorydto.ChangeStockReq{
		ProjectID: f.projectID, Direction: inventoryenums.DirectionIn, ReasonCode: "purchase_in",
		SourceType: "manual", SourceRef: "PO-" + sku,
		Lines: []inventorydto.StockChangeLineReq{{
			VariantID: v.ID, ProductID: productID, SKUCode: sku,
			WarehouseID: f.warehouse, Quantity: stock, CostPrice: &cost,
		}},
	}); err != nil {
		t.Fatalf("口味变体 %s 入库失败: %v", sku, err)
	}
	return v.ID
}

// rawItemCost 直读订单项真源列：NULL 与 0 必须能区分（这是本批口径的底线）。
func rawItemCost(t *testing.T, f *orderFixture, orderID uint64) (*int64, int) {
	t.Helper()
	var cost sql.NullInt64
	if err := f.db.Raw("SELECT cost_price FROM order_items WHERE order_id = ? ORDER BY id ASC LIMIT 1",
		orderID).Scan(&cost).Error; err != nil {
		t.Fatalf("读订单项成本失败: %v", err)
	}
	var n int
	if err := f.db.Raw("SELECT COUNT(*) FROM order_items WHERE order_id = ?", orderID).Scan(&n).Error; err != nil {
		t.Fatalf("统计订单项失败: %v", err)
	}
	if !cost.Valid {
		return nil, n
	}
	value := cost.Int64
	return &value, n
}

// TestOrderItemCostFromOwningWarehouse 验收 ①②：有 (仓库, SKU) 成本时订单行成本等于它；
// 改价之后历史订单行不漂移。
func TestOrderItemCostFromOwningWarehouse(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	pid, vid := f.addProduct(t, "成本仓", 99.50, 5)
	// 归属仓（默认仓）核算出 42.00 元：这时的变体级成本是 49.75，
	// 订单行必须取**仓库侧**的 42.00 —— 两个来源的值刻意不同，取错即失败。
	setOwningWarehouseCost(t, f, pid, vid, "SKU-成本仓", 42.00)

	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if len(detail.Items) != 1 || detail.Items[0].CostPrice == nil {
		t.Fatalf("订单行应带成本快照，实际 %+v", detail.Items)
	}
	if got := *detail.Items[0].CostPrice; got != 4200 {
		t.Fatalf("订单行成本应取默认仓的 4200 分，实际 %d", got)
	}

	// 改库存行成本 → 历史订单行是快照，必须原样不动。
	setOwningWarehouseCost(t, f, pid, vid, "SKU-成本仓", 88.00)
	after, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("再读订单详情失败: %v", err)
	}
	if after.Items[0].CostPrice == nil || *after.Items[0].CostPrice != 4200 {
		t.Fatalf("改价后历史订单行成本必须仍是 4200 分，实际 %v", after.Items[0].CostPrice)
	}
}

// TestOrderItemCostNullWhenNotCosted 验收 ②：该仓未核算 → 订单行成本**留空**（不是 0）。
func TestOrderItemCostNullWhenNotCosted(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// addProduct 只写变体级成本、入库不带显式成本 ⇒ 库存行 cost_price 为 NULL。
	_, vid := f.addProduct(t, "未核算", 50.00, 3)

	res, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if detail.Items[0].CostPrice != nil {
		t.Fatalf("未核算时订单行成本应为 null，实际 %d", *detail.Items[0].CostPrice)
	}
	// 真源佐证：列里是 NULL 而不是 0（迁移 256 去掉了 NOT NULL DEFAULT 0）。
	raw, n := rawItemCost(t, f, res.ID)
	if n != 1 || raw != nil {
		t.Fatalf("order_items.cost_price 应为 NULL，实际 %v（%d 行）", raw, n)
	}
}

// TestOrderItemCostSharedByFlavours 验收 ④：十来个口味共用同一个成本值（多行同值）时，
// 按变体汇总的利润口径正确。
func TestOrderItemCostSharedByFlavours(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p, err := f.products.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "go 35000"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	// 12 个口味：售价统一 35.00、仓库侧成本统一 12.00、各备 2 件。
	const flavours = 12
	const price, cost = 35.00, 12.00
	variants := make([]string, 0, flavours)
	items := make([]orderdto.OrderItemReq, 0, flavours)
	for i := 0; i < flavours; i++ {
		sku := "GO35-" + string(rune('A'+i))
		vid := addFlavour(t, f, p.ID, sku, price, cost, 2)
		variants = append(variants, vid)
		items = append(items, orderdto.OrderItemReq{VariantID: vid, Quantity: 1})
	}
	req := f.createBaseReq(variants[0], 1)
	req.Items = items
	res, err := f.orders.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	detail, err := f.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: f.projectID, OrderID: res.ID})
	if err != nil {
		t.Fatalf("读订单详情失败: %v", err)
	}
	if len(detail.Items) != flavours {
		t.Fatalf("应有 %d 行订单项，实际 %d", flavours, len(detail.Items))
	}
	// 按 variantId 聚合的利润口径：每行成本都是 1200 分（多行同值），
	// 毛利 = Σ(行实付) - Σ(成本 × 数量)。
	var revenue, totalCost int64
	for _, it := range detail.Items {
		if it.CostPrice == nil {
			t.Fatalf("口味 %s 的订单行成本不应为空", it.SKU)
		}
		if *it.CostPrice != 1200 {
			t.Fatalf("口味 %s 的订单行成本应为 1200 分，实际 %d", it.SKU, *it.CostPrice)
		}
		revenue += it.LineTotal
		totalCost += *it.CostPrice * int64(it.Quantity)
	}
	if revenue != flavours*3500 {
		t.Fatalf("应收 %d 分，实际 %d", flavours*3500, revenue)
	}
	if totalCost != flavours*1200 {
		t.Fatalf("总成本应为 %d 分，实际 %d", flavours*1200, totalCost)
	}
	if revenue-totalCost != flavours*2300 {
		t.Fatalf("毛利应为 %d 分，实际 %d", flavours*2300, revenue-totalCost)
	}
}
