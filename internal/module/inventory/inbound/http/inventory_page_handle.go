package inventoryhttp

import (
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	pageview "go_wp/pkg/view"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

const (
	inventoryPagePath         = "/admin/inventory"
	inventoryWarehousesPath   = "/admin/inventory/warehouses"
	inventoryReasonsPath      = "/admin/inventory/reasons"
	inventorySourcesPath      = "/admin/inventory/sources"
	inventoryMovementPageSize = 50

	inventoryMovementMaxPageSize = 200
)

type inventoryPageHandle struct {
	inventory inventorycontract.InventoryService
	projects  projectcontract.ProjectService

	products productcontract.VariantOptionsReader
}

func NewInventoryPageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService, products productcontract.VariantOptionsReader) *inventoryPageHandle {
	return &inventoryPageHandle{inventory: inventory, projects: projects, products: products}
}

func (h *inventoryPageHandle) setProductCatalog(products productcontract.VariantOptionsReader) {
	h.products = products
}

func (h *inventoryPageHandle) InventoryPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		inventoryFail(c, err)
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		inventoryFail(c, err)
		return
	}
	options, err := h.variantOptions(ctx, selected)
	if err != nil {
		inventoryFail(c, err)
		return
	}

	sku := strings.TrimSpace(c.Query("sku"))
	variantID := strings.TrimSpace(c.Query("variantId"))
	if variantID != "" && sku == "" {
		sku = skuOfVariant(options, variantID)
	}

	stockRows := []StockByWarehouseRowResp{}
	if sku != "" {
		rows, serr := h.inventory.ListStocksBySKU(ctx, &inventorydto.ListStockBySKUReq{
			ProjectID: selected, SKUCode: sku,
		})
		if serr != nil {
			inventoryFail(c, serr)
			return
		}
		byWarehouse := make(map[string]*inventorydto.StockResp, len(rows))
		for _, r := range rows {
			byWarehouse[r.WarehouseID] = r
		}
		for _, w := range warehouses {
			r := byWarehouse[w.ID]
			if r == nil {

				stockRows = append(stockRows, StockByWarehouseRowResp{
					WarehouseID: w.ID, Present: false,
					WarehouseName: w.Name, WarehouseCode: w.Code,
					SKUCode: sku,
				})
				continue
			}

			stockRows = append(stockRows, StockByWarehouseRowResp{
				WarehouseID: w.ID, Present: true,
				WarehouseName: r.WarehouseName, WarehouseCode: r.WarehouseCode,
				VariantID: r.VariantID, SKUCode: r.SKUCode,
				ExternalSKU:   r.ExternalSKU,
				TrackQuantity: r.TrackQuantity, Quantity: r.Quantity,

				ReservedQuantity:  r.ReservedQuantity,
				AvailableQuantity: r.AvailableQuantity,
				UpdatedAt:         r.UpdatedAt,
			})
		}
	}

	reasons, err := h.listReasons(c, selected)
	if err != nil {
		inventoryFail(c, err)
		return
	}

	filterWarehouse := strings.TrimSpace(c.Query("warehouseId"))
	filterDirection := strings.TrimSpace(c.Query("direction"))
	filterReason := strings.TrimSpace(c.Query("reasonCode"))
	filterTimeFrom := strings.TrimSpace(c.Query("timeFrom"))
	filterTimeTo := strings.TrimSpace(c.Query("timeTo"))

	page, limit := inventoryPageParams(c, inventoryMovementPageSize, inventoryMovementMaxPageSize)
	movements, total, page, err := h.listMovements(c, selected, sku, filterWarehouse, filterDirection,
		filterReason, filterTimeFrom, filterTimeTo, page, limit)
	if err != nil {
		inventoryFail(c, err)
		return
	}

	pageData := &InventoryPageResp{
		Menu:            "inventory",
		Projects:        projectOptions(projects),
		SelectedProject: selected,
		Warehouses:      warehouses,
		VariantOptions:  options,
		SelectedSKU:     sku,
		SelectedVariant: variantID,
		StockRows:       stockRows,
		Reasons:         reasons,

		AdjustReasons:    adjustReasonOptions(reasons),
		AdjustDirections: adjustDirectionOptions(),
		Directions:       directionOptions(),
		Movements:        movements,

		FilterWarehouse: filterWarehouse,
		FilterDirection: filterDirection,
		FilterReason:    filterReason,
		FilterTimeFrom:  filterTimeFrom,
		FilterTimeTo:    filterTimeTo,

		ListQuery: binding.ListQuery(c, inventoryStockBackKeys...),
	}

	pg := pagination.Page{Total: total, Page: page, Limit: limit}
	pg.Base = reqparams.FilterBaseURL(inventoryPagePath, map[string]string{
		"project":     selected,
		"sku":         sku,
		"variantId":   variantID,
		"warehouseId": filterWarehouse,
		"direction":   filterDirection,
		"reasonCode":  filterReason,
		"timeFrom":    filterTimeFrom,
		"timeTo":      filterTimeTo,
	})
	pageData.Pagination = pg
	pageview.RenderTemplate(c, InventoryListPageTemplate, pageData)
}

func (h *inventoryPageHandle) listMovements(c *gin.Context, projectID, sku, warehouseID,
	direction, reasonCode, timeFrom, timeTo string, page, limit int) (out []MovementRowResp, total int64, curPage int, err error) {
	ctx := c.Request.Context()
	out = []MovementRowResp{}
	if projectID == "" {
		return out, 0, 1, nil
	}

	filterReq := &inventorydto.ListMovementReq{
		ProjectID: projectID, SKUCode: sku, WarehouseID: warehouseID,
		Direction: direction, ReasonCode: reasonCode,
		TimeFrom: timeFrom, TimeTo: timeTo,
	}

	total, err = h.inventory.CountMovements(ctx, filterReq)
	if err != nil {
		return nil, 0, 0, err
	}
	curPage = clampInventoryPage(page, limit, total)
	listReq := *filterReq
	listReq.Page, listReq.Size = curPage, limit
	rows, err := h.inventory.ListMovements(ctx, &listReq)
	if err != nil {
		return nil, 0, 0, err
	}

	for _, m := range rows {
		out = append(out, movementRowResp(m))
	}
	return out, total, curPage, nil
}

func inventoryPageParams(c *gin.Context, defaultSize, maxSize int) (page, limit int) {
	page = parseIntOr(c.Query("page"), 1)
	if page < 1 {
		page = 1
	}
	limit = parseIntOr(c.Query("limit"), defaultSize)
	if limit < 1 {
		limit = defaultSize
	}
	if limit > maxSize {
		limit = maxSize
	}
	return page, limit
}

func clampInventoryPage(page, limit int, total int64) int {
	if limit < 1 {
		limit = inventoryMovementPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		return 1
	}
	if page > pages {
		return pages
	}
	return page
}

func outstandingTone(outstanding int) string {
	if outstanding > 0 {
		return "warn"
	}
	return "ok"
}

func parseIntOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}

func isHXRequest(c *gin.Context) bool {
	return reqparams.IsHXRequest(c)
}
