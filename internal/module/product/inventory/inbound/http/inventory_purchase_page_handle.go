// inventory_purchase_page_handle.go — 后台采购入库页（issue #18）。
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
package inventoryhttp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"go_wp/internal/middleware/builtin"
	productcontract "go_wp/internal/module/product/contract"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
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
//
// products 只用于 SKU 下拉：装配期传入 nil，装配收尾由 SetProductCatalog 补注
// （商品模块与本模块互为依赖，装配顺序上无法在此时拿到商品契约）。
func NewInventoryPurchasePageHandle(inventory inventorycontract.InventoryService,
	projects projectcontract.ProjectService, products productcontract.ProductService) *inventoryPurchasePageHandle {
	return &inventoryPurchasePageHandle{inventory: inventory, projects: projects, products: products}
}

// setProductCatalog 装配期补注商品契约（见 inventory_router.go 的 SetProductCatalog）。
func (h *inventoryPurchasePageHandle) setProductCatalog(products productcontract.ProductService) {
	h.products = products
}

// InventoryPurchasesPage 采购入库页：新建采购单 + 采购单列表（含逐行收货）+ 生产入库 + 进货历史。
func (h *inventoryPurchasePageHandle) InventoryPurchasesPage(c *gin.Context) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.String(http.StatusInternalServerError, shell.MsgInternalError)
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

	c.HTML(http.StatusOK, "admin/inventory_purchases.html", shell.Prepare(c, gin.H{
		"title":           inventoryenums.MsgInventoryPurchasesTitle,
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

// redirectPurchaseErr 回采购页并把业务错误经 ?err= 回显。
func redirectPurchaseErr(c *gin.Context, projectID string, err error) {
	c.Redirect(http.StatusFound, "/admin/inventory/purchases?project="+urlQueryEscape(projectID)+"&err="+url.QueryEscape(err.Error()))
}
