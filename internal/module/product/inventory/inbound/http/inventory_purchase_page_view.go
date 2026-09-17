package inventoryhttp

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	productdto "go_wp/internal/module/product/dto"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
)

// inventory_purchase_page_view.go - 采购入库页的视图构造（采购单/历史行、货源与仓库/变体下拉、状态文案）。

// purchaseOrderRows 采购单列表 + 逐行视图（含「还能收多少」与一次性幂等键）。
func (h *inventoryPurchasePageHandle) purchaseOrderRows(ctx context.Context, projectID, status, sourceID, keyword string,
	pageErr *string) (out []gin.H) {
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	orders, err := h.inventory.ListPurchaseOrders(ctx, &inventorydto.ListPurchaseOrderReq{
		ProjectID: projectID, Status: status, SourceID: sourceID, Keyword: keyword,
		Size: inventoryPurchasePageSize,
	})
	if err != nil {
		if *pageErr == "" {
			*pageErr = err.Error()
		}
		return out
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
	return out
}

// purchaseHistoryRows 按 SKU（可空 = 全部）取进货历史。
func (h *inventoryPurchasePageHandle) purchaseHistoryRows(ctx context.Context, projectID, sku string, pageErr *string) (out []gin.H) {
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := h.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: projectID, SKUCode: sku, Size: inventoryPurchaseHistorySize,
	})
	if err != nil {
		if *pageErr == "" {
			*pageErr = err.Error()
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

// sourceOptions 货源下拉（sourceType 非空时只看该类型：生产入库只列内部货源）。
func (h *inventoryPurchasePageHandle) sourceOptions(ctx context.Context, projectID, sourceType string) (out []gin.H) {
	out = []gin.H{}
	if projectID == "" {
		return out
	}
	rows, err := h.inventory.ListSources(ctx, &inventorydto.ListSourceReq{
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
		for _, v := range detail.Variants {
			cost := "未填"
			if v.CostPrice != nil {
				cost = strconv.FormatFloat(*v.CostPrice, 'f', 2, 64)
			}
			out = append(out, gin.H{
				"VariantID": v.ID, "ProductID": p.ID, "SKUCode": v.SKUCode,
				"Label": detail.Name + " · " + v.SKUCode + "（成本 " + cost + "）",
			})
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
