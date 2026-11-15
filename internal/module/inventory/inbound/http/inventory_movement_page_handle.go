package inventoryhttp

import (
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/money"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const inventoryMovementsPath = "/admin/inventory/movements"

func (h *inventoryPageHandle) InventoryMovementsPage(c *gin.Context) {
	query, err := binding.ParseQuery[inventorydto.MovementPageReq](c)
	if err != nil {
		response.ParamError(c)
		return
	}
	data, err := h.movementsData(c, query)
	if err != nil {
		pageview.RenderTemplate(c, InventoryMovementsPageTemplate, inventoryPageErrorData(c, inventoryMetaMovements, err))
		return
	}
	pageview.RenderTemplate(c, InventoryMovementsPageTemplate, data)
}

func (h *inventoryPageHandle) movementsData(c *gin.Context, query *inventorydto.MovementPageReq) (*InventoryPageResp, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	selected := strings.TrimSpace(query.ProjectID)
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		return nil, err
	}
	reasons, err := h.listReasons(c, selected)
	if err != nil {
		return nil, err
	}
	page, limit := inventoryPageParams(c, inventoryMovementPageSize, inventoryMovementMaxPageSize)
	rows, total, page, err := h.listMovements(c, selected, strings.TrimSpace(query.SKUCode),
		strings.TrimSpace(query.WarehouseID), strings.TrimSpace(query.Direction),
		strings.TrimSpace(query.ReasonCode), strings.TrimSpace(query.TimeFrom),
		strings.TrimSpace(query.TimeTo), page, limit)
	if err != nil {
		return nil, err
	}
	pg := pagination.Page{Total: total, Page: page, Limit: limit}
	pg.Base = reqparams.FilterBaseURL(inventoryMovementsPath, map[string]string{
		"project":     selected,
		"sku":         query.SKUCode,
		"warehouseId": query.WarehouseID,
		"direction":   query.Direction,
		"reasonCode":  query.ReasonCode,
		"timeFrom":    query.TimeFrom,
		"timeTo":      query.TimeTo,
	})
	return &InventoryPageResp{
		Menu:            "inventory-movements",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Warehouses:      warehouses,
		Reasons:         reasons,
		Directions:      directionOptions(),
		Movements:       rows,
		FilterSKU:       strings.TrimSpace(query.SKUCode),
		FilterWarehouse: strings.TrimSpace(query.WarehouseID),
		FilterDirection: strings.TrimSpace(query.Direction),
		FilterReason:    strings.TrimSpace(query.ReasonCode),
		FilterTimeFrom:  strings.TrimSpace(query.TimeFrom),
		FilterTimeTo:    strings.TrimSpace(query.TimeTo),
		ListQuery:       binding.ListQuery(c, inventoryMovementsBackKeys...),
		Pagination:      pg,
	}, nil
}

func movementRowResp(m *inventorydto.MovementResp) MovementRowResp {
	return MovementRowResp{
		ID: m.ID, StockID: m.StockID, SKUCode: m.SKUCode,
		StockName: m.StockName, StockImageURL: m.StockImageURL,
		WarehouseName: m.WarehouseName, WarehouseCode: m.WarehouseCode,
		Direction:      m.Direction,
		Quantity:       m.Quantity,
		Delta:          m.Delta,
		QuantityBefore: m.QuantityBefore, QuantityAfter: m.QuantityAfter,
		ReasonName: m.ReasonName, ReasonCode: m.ReasonCode,
		SourceType: m.SourceType, SourceRef: m.SourceRef, Remark: m.Remark,
		OperatorID: m.OperatorID, BatchID: m.BatchID,
		UnitCostText: money.FormatYuanOrDash(m.UnitCost),
		CreatedAt:    m.CreatedAt,
	}
}
