// 仓库商品 SKU 台账页与货行主档的三条写动作（迁移 642 / B3）。
//
// 这一屏是「从仓库选」的反向视角：候选下拉只列能选的，台账页要把**全部**货行列出来
// （包括停用的、还没被任何商品引用的）—— 否则运营没法判断「这批货到底建过没有」。
// 页面路径 /admin/inventory/skus 在「去认领」（迁移 638）时被摘下过，这里按仓库主档重建。
//
// 三条写动作（新建 / 编辑 / 启停）都不碰数量：数量只能经变动契约改（有变动必有流水）。
package inventoryhttp

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// inventorySKUsPath 台账页地址（分页基址的唯一字面量处）。
const inventorySKUsPath = "/admin/inventory/skus"

// 台账页每页条数：默认一屏 50（与「从仓库选」的候选同量级），上限与 service 同口径。
const (
	stockSKUPageSize    = 50
	stockSKUMaxPageSize = 200
)

// InventoryStockSKUsPage 仓库商品 SKU 台账页（GET /admin/inventory/skus）。
func (h *inventoryPageHandle) InventoryStockSKUsPage(c *gin.Context) {
	query, ok := binding.Query[inventorydto.StockSKUPageReq](c)
	if !ok {
		return
	}
	data, err := h.stockSKUsData(c, query)
	if err != nil {
		pageview.Render(c, inventoryPageErrorData(c, inventoryMetaStockSKUs, err))
		return
	}
	pageview.Render(c, data)
}

// stockSKUsData 取齐台账页的数据并组成视图（强类型，见 StockSKUListPageResp）。
//
// 曾返回 gin.H，理由是"失败页与正常页共用一套键"；但失败页已是强类型
// inventoryLoadFailedView，那条理由不成立，gin.H 只是没迁完的残留。
func (h *inventoryPageHandle) stockSKUsData(c *gin.Context, query *inventorydto.StockSKUPageReq) (*inventorydto.StockSKUListPageResp, error) {
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
	page, limit := inventoryPageParams(c, stockSKUPageSize, stockSKUMaxPageSize)
	rows := []inventorydto.StockSKURowResp{}
	var total int64
	if selected != "" {
		filter := &inventorydto.ListWarehouseSKUReq{
			ProjectID:   selected,
			WarehouseID: strings.TrimSpace(query.WarehouseID),
			Keyword:     strings.TrimSpace(query.Keyword),
			Status:      strings.TrimSpace(query.Status),
		}
		// 总数必须在取数之前拿到：越界页码（手输 URL / 过期书签）要按收敛后的页取数，
		// 否则会出现「表格为空、分页条却显示第 2 页」这种自相矛盾的组合。
		if total, err = h.inventory.CountWarehouseSKUsByFilter(ctx, filter); err != nil {
			return nil, err
		}
		page = clampInventoryPage(page, limit, total)
		filter.Page, filter.Size = page, limit
		if rows, err = h.stockSKURows(ctx, filter); err != nil {
			return nil, err
		}
	}
	// 取词走 text.TranslateFunc（调拨页同一入口）：shell 是正在收敛的线，
	// 新页面不再往那边加调用点。
	tr := text.TranslateFunc(c)
	pg := pagination.Page{Total: total, Page: page, Limit: limit}
	pg.Base = reqparams.FilterBaseURL(inventorySKUsPath, map[string]string{
		"project":     selected,
		"warehouseId": query.WarehouseID,
		"keyword":     query.Keyword,
		"status":      query.Status,
	})
	return &inventorydto.StockSKUListPageResp{
		Title:           tr(inventoryenums.InventoryStockSKUsTitle, "仓库商品 SKU"),
		Menu:            "inventory-skus",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Warehouses:      warehouses,
		Rows:            rows,
		FilterWarehouse: strings.TrimSpace(query.WarehouseID),
		FilterKeyword:   strings.TrimSpace(query.Keyword),
		FilterStatus:    strings.TrimSpace(query.Status),
		StatusOptions:   stockStatusOptions(c),
		ListQuery:       binding.ListQuery(c, inventorySKUBackKeys...),
		Pagination:      pg,
	}, nil
}

// stockSKURows 一屏货行（金额 / 币对 / 数量 / 变体引用在 service 侧已成型）。
func (h *inventoryPageHandle) stockSKURows(ctx context.Context, filter *inventorydto.ListWarehouseSKUReq) ([]inventorydto.StockSKURowResp, error) {
	return h.inventory.ListStockSKURows(ctx, filter)
}

// InventoryStockCreate 新建一条货行（仓库 + 裸码 + 数量 + 主档）。
func (h *inventoryPageHandle) InventoryStockCreate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.WarehouseSKUFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.CreateStock(c.Request.Context(), form.CreateReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryStockUpdate 编辑货行主档（编码与所在仓不在入参里 —— 它们是这一行的身份）。
func (h *inventoryPageHandle) InventoryStockUpdate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.WarehouseSKUFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.UpdateStock(c.Request.Context(), form.UpdateReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryStockStatus 货行启停（停用不等于删除：引用与流水照旧可读）。
func (h *inventoryPageHandle) InventoryStockStatus(c *gin.Context) {
	form, ok := binding.Form[inventorydto.StockStatusFormReq](c)
	if !ok {
		return
	}
	// 合法值只接受 active / disabled：判定在 service（那里才有统一的错误 key），
	// 这里不再判一遍 —— 两处判据分叉的表现是「页面放行、服务端拒绝」。
	if _, err := h.inventory.UpdateStockStatus(c.Request.Context(), form.StatusReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// stockStatusOptions 货行状态下拉（空值 = 不限，只用于筛选条）。
func stockStatusOptions(c *gin.Context) []inventorydto.OptionResp {
	tr := text.TranslateFunc(c)
	return []inventorydto.OptionResp{
		{Value: inventoryenums.StatusActive, Label: statusLabel(tr, inventoryenums.StatusActive)},
		{Value: inventoryenums.StatusDisabled, Label: statusLabel(tr, inventoryenums.StatusDisabled)},
	}
}
