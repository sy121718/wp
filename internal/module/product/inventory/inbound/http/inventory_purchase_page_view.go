package inventoryhttp

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
)

// inventory_purchase_page_view.go - 采购入库页的视图构造（采购单/历史行、货源与仓库/变体下拉、状态文案）。

// purchaseOrderRows 采购单列表 + 逐行视图（含「还能收多少」与一次性幂等键）。
//
// 取数失败回显走 inventoryErrText（业务 key → 当前语言文案，其余 → 归口文案 + 结构化日志），
// 与写路径同一口径：列表页的失败提示不允许比写路径多泄漏一个字符。
//
// total 是该过滤条件下的**真实总张数**（契约的 CountPurchaseOrders，与 ListPurchaseOrders
// 同一份过滤条件）；curPage 是**收敛后**的页码（page 越界时回落到末页）。out 只装本页数据。
func (h *inventoryPurchasePageHandle) purchaseOrderRows(c *gin.Context, projectID, status, sourceID, keyword string,
	page, limit int, pageErr *string) (out []gin.H, total int64, curPage int) {
	ctx := c.Request.Context()
	out = []gin.H{}
	if projectID == "" {
		return out, 0, 1
	}
	// 过滤条件只构造一次：列表与计数各自复制、只加各自的 Page/Size ——
	// 两处各写一份时，日后新增一个筛选维度只改到列表那一侧，「共 N 条」就会与实际条数矛盾。
	filterReq := &inventorydto.ListPurchaseOrderReq{
		ProjectID: projectID, Status: status, SourceID: sourceID, Keyword: keyword,
	}
	// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampInventoryPage）：
	// 反过来时 page 越界会让列表返回空页，而分页条按收敛后的页码渲染。
	n, cerr := h.inventory.CountPurchaseOrders(ctx, filterReq)
	if cerr != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, cerr)
		}
		return out, 0, page
	}
	total = n
	curPage = clampInventoryPage(page, limit, total)
	listReq := *filterReq
	listReq.Page, listReq.Size = curPage, limit
	orders, err := h.inventory.ListPurchaseOrders(ctx, &listReq)
	if err != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out, total, curPage
	}
	for _, o := range orders {
		lines := make([]gin.H, 0, len(o.Lines))
		for _, l := range o.Lines {
			lines = append(lines, gin.H{
				"ID": l.ID, "SKUCode": l.SKUCode, "VariantID": l.VariantID,
				"Quantity": l.Quantity, "ReceivedQuantity": l.ReceivedQuantity,
				"OutstandingQuantity": l.OutstandingQuantity,
				"UnitPrice":           strconv.FormatFloat(l.UnitPrice, 'f', 2, 64),
				"Remark":              l.Remark,
				// 还能收的行才给提交按钮：收满的行不再受理（服务端同样拒绝）。
				"Open": l.OutstandingQuantity > 0,
			})
		}
		out = append(out, gin.H{
			"ID": o.ID, "Code": o.Code,
			"SourceName": o.SourceName, "SourceType": o.SourceType,
			"SourceTypeLabel": sourceTypeLabel(o.SourceType),
			"WarehouseName":   o.WarehouseName,
			"Status":          o.Status, "StatusLabel": purchaseStatusLabel(o.Status),
			"OrderedAt": o.OrderedAt, "ExpectedAt": o.ExpectedAt,
			"Remark": o.Remark, "OperatorID": o.OperatorID,
			"TotalQuantity": o.TotalQuantity, "ReceivedQuantity": o.ReceivedQuantity,
			"Lines": lines,
			// 每张单一个一次性幂等键（表单渲染时生成，重复提交命中同一张入库单）。
			"RequestID": uuid.NewString(),
		})
	}
	return out, total, curPage
}

// purchaseHistoryRows 按 SKU（可空 = 全部）取进货历史。
//
// 当前页面不再渲染这张表（「按 SKU 看过往入库」已并入库存管理页的流水筛选），
// 但取数口保留：接口侧与将来的页面入口共用同一投影。错误回显同样走 inventoryErrText——
// 留着 err.Error() 就等于在同一个文件里留了一条尚未被页面用到的泄漏路径。
func (h *inventoryPurchasePageHandle) purchaseHistoryRows(c *gin.Context, projectID, sku string, pageErr *string) (out []gin.H) {
	ctx := c.Request.Context()
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := h.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: projectID, SKUCode: sku, Size: inventoryPurchaseHistorySize,
	})
	if err != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out
	}
	for _, r := range rows {
		out = append(out, gin.H{
			"ReceiptCode": r.ReceiptCode, "Kind": r.Kind, "KindLabel": receiptKindLabel(r.Kind),
			"OrderCode": r.OrderCode, "SourceName": r.SourceName,
			"SourceTypeLabel": sourceTypeLabel(r.SourceType),
			"WarehouseName":   r.WarehouseName, "SKUCode": r.SKUCode,
			"Quantity": r.Quantity, "UnitPrice": strconv.FormatFloat(r.UnitPrice, 'f', 2, 64),
			"CostUpdated": r.CostUpdated, "Remark": r.Remark,
			"OperatorID": r.OperatorID, "ReceivedAt": r.ReceivedAt,
		})
	}
	return out
}

// sourceOptions 货源下拉（sourceType 非空时只看该类型：内部类型 = 自家工厂 / 集团内关联公司）。
//
// 包级函数而非某个 handler 的方法：库存管理与采购入库两个页面都要用 ——
// 「生产入库」已从采购页归位到库存管理（它与采购单无关，本质是「手动改库存 + 写成本价」）。
func sourceOptions(ctx context.Context, svc inventorycontract.InventoryService, projectID, sourceType string) (out []gin.H) {
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := svc.ListSources(ctx, &inventorydto.ListSourceReq{
		ProjectID: projectID, Type: sourceType,
	})
	if err != nil {
		return out
	}
	for _, s := range rows {
		out = append(out, gin.H{
			"ID": s.ID, "Code": s.Code, "Name": s.Name, "Type": s.Type,
			"Label": s.Name + "（" + s.Code + "）· " + sourceTypeLabel(s.Type),
		})
	}
	return out
}

// purchaseWarehouseOptions 收货仓下拉（默认仓标出来；未选即兜底默认仓）。
func (h *inventoryPurchasePageHandle) purchaseWarehouseOptions(ctx context.Context, projectID string) (out []gin.H) {
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := h.inventory.ListWarehouses(ctx, &inventorydto.ListWarehouseReq{ProjectID: projectID})
	if err != nil {
		return out
	}
	for _, w := range rows {
		label := w.Name + "（" + w.Code + "）"
		if w.IsDefault {
			label += " · 默认仓"
		}
		out = append(out, gin.H{"ID": w.ID, "Label": label, "IsDefault": w.IsDefault})
	}
	return out
}

// purchaseVariantOptions 变体下拉（带商品名 / SKU / 当前成本价，选行时一眼可见成本口径）。
//
// 每一项额外带 **BareSKU（仓库侧裸码）**：采购行的 sku_code 是仓库侧快照，必须按裸码落库
// （仓库里的 SKU 永远不带仓码前缀，见 docs/14 §1.1 与迁移 262）。裸码取自本模块自己的
// 库存真源（inventory_stocks.sku_code）—— 它就是「这条货在仓库里叫什么」的权威答案，
// 商品侧的 v.SKUCode 只是它加了认领仓前缀的投影。没有库存行的变体退回商品侧编码，
// 由服务端在入库入口按目标仓短码归一（inventory_stock_sku.go）。
func (h *inventoryPurchasePageHandle) purchaseVariantOptions(ctx context.Context, projectID string) (out []gin.H) {
	out = []gin.H{}
	// 商品契约未注入（装配漏接）时下拉为空，页面照常渲染 —— 不因一处装配缺失 500。
	if projectID == "" || h.products == nil {
		return out
	}
	list, err := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Size: 100})
	if err != nil {
		return out
	}
	for _, p := range list {
		detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
		if derr != nil {
			continue
		}
		bare := h.productBareSKUs(ctx, projectID, p.ID)
		for _, v := range detail.Variants {
			cost := "未填"
			if v.CostPrice != nil {
				cost = strconv.FormatFloat(*v.CostPrice, 'f', 2, 64)
			}
			sku := bare[v.ID]
			if sku == "" {
				sku = v.SKUCode
			}
			out = append(out, gin.H{
				"VariantID": v.ID, "ProductID": p.ID, "SKUCode": v.SKUCode, "BareSKU": sku,
				"Label": detail.Name + " · " + v.SKUCode + "（成本 " + cost + "）",
			})
		}
	}
	return out
}

// productBareSKUs 某商品全部变体的**仓库侧裸码**（变体 id → inventory_stocks.sku_code）。
//
// 一次取回该商品在各仓的库存行（按商品过滤，与页面既有的「逐商品取详情」同量级），
// 变体在哪个仓有行都算 —— 裸码是变体的仓库侧身份，与具体哪个仓无关。
// 读失败不抛给页面（返回空 map，调用方退回商品侧编码）：采购页不该因为一次展示取数
// 失败而 500，真正的守门在入库入口的归一路径上。
func (h *inventoryPurchasePageHandle) productBareSKUs(ctx context.Context, projectID, productID string) map[string]string {
	out := map[string]string{}
	rows, err := h.inventory.ListStocks(ctx, &inventorydto.ListStockReq{
		ProjectID: projectID, ProductID: productID, Size: inventoryPurchaseStockProbeSize,
	})
	if err != nil {
		return out
	}
	for _, r := range rows {
		if r.VariantID != "" && r.SKUCode != "" {
			out[r.VariantID] = r.SKUCode
		}
	}
	return out
}

// —— 表单与文案工具 ——

// purchaseStatusOptions 状态筛选下拉（三种推导值）。
func purchaseStatusOptions() []gin.H {
	return []gin.H{
		{"Value": inventoryenums.PurchaseStatusPending, "Label": purchaseStatusLabel(inventoryenums.PurchaseStatusPending)},
		{"Value": inventoryenums.PurchaseStatusPartial, "Label": purchaseStatusLabel(inventoryenums.PurchaseStatusPartial)},
		{"Value": inventoryenums.PurchaseStatusReceived, "Label": purchaseStatusLabel(inventoryenums.PurchaseStatusReceived)},
	}
}

// purchaseStatusLabel 推导状态 → 展示文案。
func purchaseStatusLabel(status string) string {
	switch status {
	case inventoryenums.PurchaseStatusPending:
		return "未入库"
	case inventoryenums.PurchaseStatusPartial:
		return "部分入库"
	case inventoryenums.PurchaseStatusReceived:
		return "已入库"
	default:
		return status
	}
}

// receiptKindLabel 入库单类型 → 展示文案。
func receiptKindLabel(kind string) string {
	switch kind {
	case inventoryenums.ReceiptKindPurchase:
		return "采购收货"
	case inventoryenums.ReceiptKindProduction:
		return "生产入库"
	default:
		return kind
	}
}
