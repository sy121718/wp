package inventoryhttp

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	pageview "go_wp/pkg/view"
)

const inventorySKUsPath = "/admin/inventory/skus"

const (
	stockSKUPageSize    = 50
	stockSKUMaxPageSize = 200
)

func (h *inventoryPageHandle) InventoryStockSKUsPage(c *gin.Context) {
	query, ok := binding.Query[inventorydto.StockSKUPageReq](c)
	if !ok {
		return
	}
	data, err := h.stockSKUsData(c, query)
	if err != nil {
		pageview.RenderTemplate(c, InventoryStockSKUsPageTemplate, inventoryPageErrorData(c, inventoryMetaStockSKUs, err))
		return
	}
	pageview.RenderTemplate(c, InventoryStockSKUsPageTemplate, data)
}

func (h *inventoryPageHandle) stockSKUsData(c *gin.Context, query *inventorydto.StockSKUPageReq) (*StockSKUListPageResp, error) {
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

		if total, err = h.inventory.CountWarehouseSKUsByFilter(ctx, filter); err != nil {
			return nil, err
		}
		page = clampInventoryPage(page, limit, total)
		filter.Page, filter.Size = page, limit
		if rows, err = h.stockSKURows(ctx, filter); err != nil {
			return nil, err
		}
	}

	pg := pagination.Page{Total: total, Page: page, Limit: limit}
	pg.Base = reqparams.FilterBaseURL(inventorySKUsPath, map[string]string{
		"project":     selected,
		"warehouseId": query.WarehouseID,
		"keyword":     query.Keyword,
		"status":      query.Status,
	})
	return &StockSKUListPageResp{
		Menu:            "inventory-skus",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Warehouses:      warehouses,
		Rows:            rows,
		FilterWarehouse: strings.TrimSpace(query.WarehouseID),
		FilterKeyword:   strings.TrimSpace(query.Keyword),
		FilterStatus:    strings.TrimSpace(query.Status),
		StatusOptions:   stockStatusOptions(),
		ListQuery:       binding.ListQuery(c, inventorySKUBackKeys...),
		Pagination:      pg,
	}, nil
}

func (h *inventoryPageHandle) stockSKURows(ctx context.Context, filter *inventorydto.ListWarehouseSKUReq) ([]inventorydto.StockSKURowResp, error) {
	return h.inventory.ListStockSKURows(ctx, filter)
}

func (h *inventoryPageHandle) InventoryStockCreate(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.WarehouseSKUFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.CreateStock(c.Request.Context(), form.CreateReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryStockUpdate(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.WarehouseSKUFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.UpdateStock(c.Request.Context(), form.UpdateReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

func (h *inventoryPageHandle) InventoryStockStatus(c *gin.Context) {
	form, err := binding.ParseForm[inventorydto.StockStatusFormReq](c)
	if err != nil {
		pageview.FailStatus(c, http.StatusBadRequest, inventoryErrScene, err)
		return
	}

	if _, err := h.inventory.UpdateStockStatus(c.Request.Context(), form.StatusReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryWriteOK(c)
}

// stockStatusOptions 只交值与顺序；标签由模板按请求语言取词。
func stockStatusOptions() []OptionResp {
	return []OptionResp{
		{Value: inventoryenums.StatusActive},
		{Value: inventoryenums.StatusDisabled},
	}
}
