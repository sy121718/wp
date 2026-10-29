// 库存流水独立页（B4）：/admin/inventory/movements。
//
// 为什么从 /admin/inventory 拆出来：那一屏是「按 SKU 横看各仓」的只读视图 + 盘点调整入口，
// 流水只是它下面的第三块。而流水本身的读取维度是**跨 SKU 的时间轴**（谁在什么时候动了哪条货），
// 与「选一个 SKU 看它的库存」是两种查询目的 —— 挤在一页的后果是筛 SKU 时流水被同一个 sku
// 参数绑定，想看全局流水就得把 SKU 清空，而清空又同时清掉了库存卡的上下文。
//
// 本页只读：不提供任何写入口。入库 / 出库 / 盘点都在各自的单据页与调整入口里。
package inventoryhttp

import (
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	"go_wp/pkg/money"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	pageview "go_wp/pkg/view"
)

// inventoryMovementsPath 流水页地址（分页基址与重置链接的唯一字面量处）。
const inventoryMovementsPath = "/admin/inventory/movements"

// InventoryMovementsPage 库存流水页（GET /admin/inventory/movements）。
func (h *inventoryPageHandle) InventoryMovementsPage(c *gin.Context) {
	query, ok := binding.Query[inventorydto.MovementPageReq](c)
	if !ok {
		return
	}
	data, err := h.movementsData(c, query)
	if err != nil {
		pageview.Render(c, inventoryPageErrorData(c, inventoryMetaMovements, err))
		return
	}
	pageview.Render(c, data)
}

// movementsData 取齐流水页的数据并组成视图。
//
// 返回 InventoryPageResp（与其它库存页同一个视图类型）："键集齐全由类型保证" ——
// 漏一个字段是零值、而不是模板取不到键导致整页中断。此前这里返回 gin.H，理由是
// "失败页与正常页共用一套键"，但失败页早已是强类型 inventoryLoadFailedView，
// 那条理由不成立，gin.H 只是没跟着迁完的残留。
func (h *inventoryPageHandle) movementsData(c *gin.Context, query *inventorydto.MovementPageReq) (*inventorydto.InventoryPageResp, error) {
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
	tr := text.TranslateFunc(c)
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
	return &inventorydto.InventoryPageResp{
		Title:           tr(inventoryenums.InventoryMovementsTitle, "库存流水"),
		Menu:            "inventory-movements",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Warehouses:      warehouses,
		Reasons:         reasons,
		Directions:      directionOptions(tr),
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

// movementRowResp 一条流水的展示行。
//
// 与 listMovements 共用（库存管理页也调它）：两处各写一份映射时，日后新增一列只改一侧，
// 表现是「同一个流水在两页显示不同的成本」—— 这类分叉只在对比时才看得出来。
func movementRowResp(tr func(key, fallback string) string, m *inventorydto.MovementResp) inventorydto.MovementRowResp {
	return inventorydto.MovementRowResp{
		ID: m.ID, StockID: m.StockID, SKUCode: m.SKUCode,
		StockName: m.StockName, StockImageURL: m.StockImageURL,
		WarehouseName: m.WarehouseName, WarehouseCode: m.WarehouseCode,
		Direction: m.Direction, DirectionLabel: directionLabel(tr, m.Direction),
		Quantity:       m.Quantity,
		Delta:          m.Delta,
		QuantityBefore: m.QuantityBefore, QuantityAfter: m.QuantityAfter,
		ReasonName: tr(m.ReasonName, m.ReasonCode), ReasonCode: m.ReasonCode,
		SourceType: m.SourceType, SourceRef: m.SourceRef, Remark: m.Remark,
		OperatorID: m.OperatorID, BatchID: m.BatchID,
		UnitCostText: money.FormatYuanOrDash(m.UnitCost),
		CreatedAt:    m.CreatedAt,
	}
}
