// 仓库商品 SKU 台账页（迁移 621）与它的三个写动作：认领 / 解除认领 / 新建未认领存货。
package inventoryhttp

// 为什么单独一张台账页而不是塞进 inventory.html：
//
//	inventory.html 的库存表是「某个 SKU 在各仓的展开」（必须先选变体 / 输 SKU 才有行），
//	而未认领行**根本没有 variant_id**，进不了那条查询 —— 于是「仓库里已有的货」
//	在这一页永远不出现。台账页按库存行本身列，未认领行才是它的主角。
//
// 三个写动作全部**复用** /api/inventory/stock/change 的权限点（与外部编码、跟踪开关同口径）：
// 能改这个仓库存的人，才该能认领它 / 给它建档。不新增权限点，也不新增 authorizedAPI 路由。

import (
	"strings"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// inventorySKUsPath 台账页地址（回跳与分页基址的唯一字面量处）。
const inventorySKUsPath = "/admin/inventory/skus"

// InventorySKUsPage 仓库商品 SKU 台账页：仓库 / 短码 / SKU / 外码 / 认领 / 预占 / 可售 / 操作。
//
// 数据全部来自 service 的读模型（ListStockLedger）：一行 = 一条库存行，
// 「认领到谁」的文案与「可售 = 数量 − 预占」的减法都在那边算好，
// 这里的职责只有「绑定 → 调用例 → 交出视图」。
func (h *inventoryPageHandle) InventorySKUsPage(c *gin.Context) {
	query, ok := binding.Query[inventorydto.StockLedgerQueryReq](c)
	if !ok {
		return
	}
	data, err := h.skuLedgerData(c, query)
	if err != nil {
		pageview.Render(c, inventoryPageErrorData(c, inventoryMetaSKUs, err))
		return
	}
	pageview.Render(c, data)
}

// skuLedgerData 取齐台账页的数据并组成视图（三步取数 + 视图装配都在这里，
// handler 侧只留「绑定 → 调用 → 出口」）。
func (h *inventoryPageHandle) skuLedgerData(c *gin.Context,
	query *inventorydto.StockLedgerQueryReq) (gin.H, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	selected := query.ProjectID
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	warehouses, err := h.listWarehouses(c, selected)
	if err != nil {
		return nil, err
	}
	options, err := h.variantOptions(ctx, selected)
	if err != nil {
		return nil, err
	}
	if selected != "" {
		query.ProjectID = selected
	}
	ledger, err := h.inventory.ListStockLedger(ctx, query, options)
	if err != nil {
		return nil, err
	}
	return h.skuLedgerView(c, query, selected, projects, warehouses, ledger), nil
}

// skuLedgerView 把三步取数组成页面视图（装配在辅助函数里，handler 保持 ≤6 条语句）。
func (h *inventoryPageHandle) skuLedgerView(c *gin.Context, query *inventorydto.StockLedgerQueryReq,
	selected string, projects []projectcontract.ProjectResp, warehouses []inventorydto.WarehouseRowResp,
	ledger *inventorydto.StockLedgerPage) gin.H {
	tr := text.TranslateFunc(c)
	claimed := normalizeClaimedQuery(query.Claimed)
	pg := pagination.Page{Total: int64(ledger.Total), Page: query.Page, Limit: query.Limit}
	pg.Base = reqparams.FilterBaseURL(inventorySKUsPath, map[string]string{
		"project": selected, "warehouseId": query.WarehouseID,
		"keyword": query.Keyword, "claimed": claimed,
	})
	return gin.H{
		"title":           tr(inventoryenums.InventorySKUsTitle, "仓库商品 SKU"),
		"menu":            "inventory-skus",
		"Projects":        projects,
		"SelectedProject": selected,
		"Warehouses":      warehouses,
		"VariantOptions":  ledger.Options,
		"Rows":            ledger.Rows,
		"Keyword":         query.Keyword,
		"FilterWarehouse": query.WarehouseID,
		"FilterClaimed":   claimed,
		"ClaimedOptions":  claimedFilterOptions(tr),
		"ListQuery":       binding.ListQuery(c, inventorySKUBackKeys...),
		"Pagination":      pg,
	}
}

// InventoryStockClaim 认领：把一条未认领存货挂到某个商品变体上。
func (h *inventoryPageHandle) InventoryStockClaim(c *gin.Context) {
	form, ok := binding.Form[inventorydto.ClaimStockFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.ClaimStock(c.Request.Context(), form.ClaimReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryStockUnclaim 解除认领（仅当数量与预占都为 0）。
func (h *inventoryPageHandle) InventoryStockUnclaim(c *gin.Context) {
	form, ok := binding.Form[inventorydto.UnclaimStockFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.UnclaimStock(c.Request.Context(), form.UnclaimReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryPurchaseFinish 采购单提前关单（迁移 623, W2）：received / partial → finished。
//
// 与取消分开两个入口而不是一个「改状态」接口：两个动作的前置状态不同
// （finished 要求已入库过、cancelled 要求还没收满），合成一个只会让调用方各自判一次
// 该传哪个值 —— 而判错的表现是「已收满的单被取消，货却已经进仓」。
func (h *inventoryPurchasePageHandle) InventoryPurchaseFinish(c *gin.Context) {
	form, ok := binding.Form[inventorydto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.FinishPurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryPurchaseCancel 采购单取消（迁移 623, W2）：pending / partial → cancelled。
func (h *inventoryPurchasePageHandle) InventoryPurchaseCancel(c *gin.Context) {
	form, ok := binding.Form[inventorydto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.CancelPurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// InventoryStockUnclaimedCreate 人工建档一条未认领存货（仓库 + 裸码 + 数量 + 成本）。
func (h *inventoryPageHandle) InventoryStockUnclaimedCreate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.UnclaimedStockFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.CreateUnclaimedStock(c.Request.Context(), form.CreateReq()); err != nil {
		inventoryFail(c, err)
		return
	}
	response.Success(c, nil)
}

// normalizeClaimedQuery 归一只读筛选值（只认两个白名单，其余当「全部」）。
//
// 与 service 的 claimedFilterOf 同一份口径，但这一份只服务「页面回显选中项」：
// 回显与过滤各判一次不同值会出现「下拉选中「未认领」而列表显示全部」。
func normalizeClaimedQuery(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "claimed":
		return "claimed"
	case "unclaimed":
		return "unclaimed"
	}
	return ""
}

// claimedFilterOptions 认领状态筛选的三个选项（全部 / 已认领 / 未认领）。
func claimedFilterOptions(tr func(key, fallback string) string) []inventorydto.OptionResp {
	return []inventorydto.OptionResp{
		{Value: "", Label: tr(inventoryenums.InventoryFilterAll, "（全部）")},
		{Value: "claimed", Label: tr(inventoryenums.InventoryStockClaimed, "已认领")},
		{Value: "unclaimed", Label: tr(inventoryenums.InventoryStockUnclaimed, "未认领")},
	}
}

// StockAlert 库存预警（GET /api/inventory/stock/alert）。
//
// 复用 stock/list 的权限点：它是同一个真源的另一种读法（「哪些行要补货」），
// 不是新能力 —— 能看库存的人就能看预警。
func (h *Handle) StockAlert(c *gin.Context) {
	query, ok := binding.Query[inventorydto.StockAlertQueryReq](c)
	if !ok {
		return
	}
	res, err := h.svc.StockAlert(c.Request.Context(), query.ProjectID)
	if err != nil {
		response.ErrorAuto(c, 400, "inventory", err)
		return
	}
	response.Success(c, res)
}
