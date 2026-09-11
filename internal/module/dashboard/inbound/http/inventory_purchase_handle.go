// inventory_purchase_handle.go — 后台采购入库页（issue #18）。
//
// 与库存管理页 / 货源管理页同一模式：GET 渲染完整页，POST 处理完 302 回列表
// （原生表单 + csrf_token 隐藏域），错误经 ?err= 回显。
//
// 页面承担六条验收：
//
//	· 验收 1：建采购单与采购行（SKU、数量、单价），行上的「登记入库」按钮提交后
//	  已入库数量累加（服务端在行锁内原子递增）；
//	· 验收 2：单据状态直接读服务端推导出的 pending / partial / received；
//	· 验收 3：登记入库即增加库存并写流水（原因是字典里的采购入库）；
//	· 验收 4：入库以采购单价更新 SKU 成本价（表单可填「本次到货价」，缺省用采购单价）；
//	· 验收 5：自家工厂生产入库表单 —— 无采购单、来源只列内部货源、成本价手工填写；
//	· 验收 6：按 SKU 查进货历史（单据 / 数量 / 单价 / 来源 / 仓库 / 时间）。
//
// 幂等：每个收货表单渲染时带一个一次性 requestId（隐藏域），双击提交只会产生一张入库单。
package dashboardhttp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/middleware/builtin"
)

const (
	// inventoryPurchasePageSize 采购单页一次列出的单据数。
	inventoryPurchasePageSize = 100
	// inventoryPurchaseHistorySize 进货历史一次列出的条数。
	inventoryPurchaseHistorySize = 50
	// inventoryPurchaseDraftLines 新建采购单表单的行数（原生表单：固定几行）。
	inventoryPurchaseDraftLines = 3
)

// inventoryPurchasePageHandle 采购入库页处理器。
type inventoryPurchasePageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService
	// products 只用于 SKU 下拉（商品 → 变体），不参与任何库存 / 采购判断。
	products productcontract.ProductService
}

// NewInventoryPurchasePageHandle 构造。
func NewInventoryPurchasePageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService, products productcontract.ProductService) *inventoryPurchasePageHandle {
	return &inventoryPurchasePageHandle{inventory: inventory, projects: projects, products: products}
}

// InventoryPurchasesPage 采购入库页：新建采购单 + 采购单列表（含逐行收货）+ 生产入库 + 进货历史。
func (h *inventoryPurchasePageHandle) InventoryPurchasesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	pageErr := strings.TrimSpace(c.Query("err"))
	filterStatus := strings.TrimSpace(c.Query("status"))
	filterSource := strings.TrimSpace(c.Query("sourceId"))
	filterKeyword := strings.TrimSpace(c.Query("keyword"))

	sources := h.sourceOptions(ctx, selected, "")
	internalSources := h.sourceOptions(ctx, selected, inventoryenums.SourceTypeInternal)
	warehouses := h.purchaseWarehouseOptions(ctx, selected)
	variants := h.purchaseVariantOptions(ctx, selected)
	orders := h.purchaseOrderRows(ctx, selected, filterStatus, filterSource, filterKeyword, &pageErr)

	historySKU := strings.TrimSpace(c.Query("sku"))
	history := h.purchaseHistoryRows(ctx, selected, historySKU, &pageErr)

	c.HTML(http.StatusOK, "admin/inventory_purchases.html", withCSRF(c, gin.H{
		"title":           dashboardenums.MsgInventoryPurchasesTitle,
		"menu":            "inventory-purchases",
		"Projects":        projects,
		"SelectedProject": selected,
		"Sources":         sources,
		"InternalSources": internalSources,
		"Warehouses":      warehouses,
		"VariantOptions":  variants,
		"DraftLines":      purchaseDraftLines(),
		"Orders":          orders,
		"StatusOptions":   purchaseStatusOptions(),
		"FilterStatus":    filterStatus,
		"FilterSource":    filterSource,
		"FilterKeyword":   filterKeyword,
		"HistorySKU":      historySKU,
		"History":         history,
		// 一次性幂等键：每个表单一次渲染一个，双击提交只会产生一张入库单。
		"ReceiptRequestID":    uuid.NewString(),
		"ProductionRequestID": uuid.NewString(),
		"Err":                 pageErr,
		"Ok":                  strings.TrimSpace(c.Query("ok")),
	}))
}

// InventoryPurchaseCreate 新建采购单（单头 + 结构化行）。
func (h *inventoryPurchasePageHandle) InventoryPurchaseCreate(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.CreatePurchaseOrderReq{
		ProjectID:   projectID,
		Code:        strings.TrimSpace(c.PostForm("code")),
		SourceID:    strings.TrimSpace(c.PostForm("sourceId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		Remark:      strings.TrimSpace(c.PostForm("remark")),
		OperatorID:  builtin.GetUsername(c),
		Lines:       purchaseLinesForm(c),
	}
	if _, err := h.inventory.CreatePurchaseOrder(c.Request.Context(), req); err != nil {
		redirectPurchaseErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/purchases?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventoryPurchaseReceipt 登记某一行（或多行）采购收货。
//
// 表单是「按行提交」：每行一个独立表单，因此这里只需处理一行；接口侧的批量收货运维用。
func (h *inventoryPurchasePageHandle) InventoryPurchaseReceipt(c *gin.Context) {
	projectID := c.PostForm("projectId")
	quantity := parseIntOr(c.PostForm("quantity"), 0)
	req := &inventorydto.RegisterReceiptReq{
		ProjectID:  projectID,
		OrderID:    c.PostForm("orderId"),
		RequestID:  strings.TrimSpace(c.PostForm("requestId")),
		Remark:     strings.TrimSpace(c.PostForm("remark")),
		OperatorID: builtin.GetUsername(c),
		Lines: []inventorydto.ReceiptLineReq{{
			LineID:   strings.TrimSpace(c.PostForm("lineId")),
			Quantity: quantity,
		}},
	}
	if price, ok := parseFloatOK(c.PostForm("unitPrice")); ok {
		req.Lines[0].UnitPrice = &price
	}
	if _, err := h.inventory.RegisterReceipt(c.Request.Context(), req); err != nil {
		redirectPurchaseErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/purchases?project="+urlQueryEscape(projectID)+"&ok=1")
}

// InventoryPurchaseProduction 自家工厂生产入库（无采购单，成本价手工填写）。
func (h *inventoryPurchasePageHandle) InventoryPurchaseProduction(c *gin.Context) {
	projectID := c.PostForm("projectId")
	req := &inventorydto.ProductionInboundReq{
		ProjectID:   projectID,
		SourceID:    strings.TrimSpace(c.PostForm("sourceId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		VariantID:   strings.TrimSpace(c.PostForm("variantId")),
		ProductID:   strings.TrimSpace(c.PostForm("productId")),
		SKUCode:     strings.TrimSpace(c.PostForm("skuCode")),
		Quantity:    parseIntOr(c.PostForm("quantity"), 0),
		RequestID:   strings.TrimSpace(c.PostForm("requestId")),
		Remark:      strings.TrimSpace(c.PostForm("remark")),
		OperatorID:  builtin.GetUsername(c),
	}
	if cost, ok := parseFloatOK(c.PostForm("unitCost")); ok {
		req.UnitCost = &cost
	}
	if _, err := h.inventory.RegisterProductionInbound(c.Request.Context(), req); err != nil {
		redirectPurchaseErr(c, projectID, err)
		return
	}
	c.Redirect(http.StatusFound, "/admin/inventory/purchases?project="+urlQueryEscape(projectID)+"&ok=1")
}

// —— 页面取数（视图组装：模板不做逻辑）——

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
	if projectID == "" {
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

// purchaseLinesForm 解析新建表单里的固定几行（空行跳过）。
func purchaseLinesForm(c *gin.Context) (lines []inventorydto.PurchaseLineReq) {
	variantIDs := c.PostFormArray("lineVariantId")
	productIDs := c.PostFormArray("lineProductId")
	skuCodes := c.PostFormArray("lineSKUCode")
	quantities := c.PostFormArray("lineQuantity")
	prices := c.PostFormArray("lineUnitPrice")
	lines = make([]inventorydto.PurchaseLineReq, 0, len(variantIDs))
	for i, raw := range variantIDs {
		variantID := strings.TrimSpace(raw)
		if variantID == "" {
			continue
		}
		line := inventorydto.PurchaseLineReq{
			VariantID: variantID,
			ProductID: formArrayAt(productIDs, i),
			SKUCode:   formArrayAt(skuCodes, i),
			Quantity:  parseIntOr(formArrayAt(quantities, i), 0),
			Sort:      i,
		}
		if price, ok := parseFloatOK(formArrayAt(prices, i)); ok {
			line.UnitPrice = price
		}
		lines = append(lines, line)
	}
	return lines
}

// formArrayAt 取数组第 i 项（越界返回空串，表单行数与数组长度不一致时不 panic）。
func formArrayAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

// parseFloatOK 解析可空小数（空串 / 非法返回 ok=false，交给服务端报参数错误）。
func parseFloatOK(raw string) (value float64, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

// purchaseDraftLines 新建窗体里的空行（模板 range 用）。
func purchaseDraftLines() []gin.H {
	out := make([]gin.H, 0, inventoryPurchaseDraftLines)
	for i := 0; i < inventoryPurchaseDraftLines; i++ {
		out = append(out, gin.H{"Index": i})
	}
	return out
}

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

// redirectPurchaseErr 回采购页并把业务错误经 ?err= 回显。
func redirectPurchaseErr(c *gin.Context, projectID string, err error) {
	c.Redirect(http.StatusFound, "/admin/inventory/purchases?project="+urlQueryEscape(projectID)+"&err="+url.QueryEscape(err.Error()))
}
