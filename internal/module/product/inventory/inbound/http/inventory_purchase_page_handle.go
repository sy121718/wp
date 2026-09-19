// inventory_purchase_page_handle.go — 后台采购入库页（issue #18）。
//
// 与库存管理页 / 货源管理页同一模式：GET 渲染完整页，POST 处理完 302 回列表
// （原生表单 + csrf_token 隐藏域），错误经 inventoryErrText 翻成当前语言后再 ?err= 回显。
//
// 页面承担六条验收：
//
//	· 验收 1：建采购单与采购行（SKU、数量、单价），行上的「登记入库」按钮提交后
//	  已入库数量累加（服务端在行锁内原子递增）；
//	· 验收 2：单据状态直接读服务端推导出的 pending / partial / received；
//	· 验收 3：登记入库即增加库存并写流水（原因是字典里的采购入库）；
//	· 验收 4：入库以采购单价更新 SKU 成本价（表单可填「本次到货价」，缺省用采购单价）；
//	· 验收 5：自家工厂生产入库表单 —— 无采购单、来源只列内部货源、成本价手工填写
//	  （表单与路由 /admin/inventory/purchases/production、权限点 inventory:purchase_production
//	  一直在，本批把页面上缺失的表单补回，见模板 inventory_purchases.html）；
//	· 验收 6：按 SKU 查进货历史（单据 / 数量 / 单价 / 来源 / 仓库 / 时间）——
//	  已并入库存管理页的流水筛选（按 SKU / 原因 / 时间），本页不再挂那张表。
//
// 幂等：每个写表单渲染时带一个一次性 requestId（隐藏域），双击提交只会产生一张入库单；
// 收货与生产入库**各带一个键** —— 幂等键在入库单上全局唯一，共用一个键会让
// 「先收采购货、再做生产入库」的第二跳被当成同一张单的重放而静默丢弃。
package inventoryhttp

import (
	"net/http"
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
	// inventoryPurchasesPath 采购入库页路径（回跳地址的唯一定义处，与库存页的
	// inventoryPagePath 同形：handler 不手写字符串，抄错一次就是回跳到 404）。
	inventoryPurchasesPath = "/admin/inventory/purchases"
	// inventoryPurchaseHistorySize 进货历史一次列出的条数（页面当前不再渲染这张表，
	// 取数口保留，见 purchaseHistoryRows）。
	inventoryPurchaseHistorySize = 50
	// inventoryPurchaseDraftLines 新建采购单表单的行数（原生表单：固定几行）。
	inventoryPurchaseDraftLines = 3
	// inventoryPurchaseStockProbeSize 取「该商品各仓库存行」的上限（扒仓库侧裸码用）。
	// 与库存列表的 maxPageSize 同量级：它的用途只是给下拉的每一项配一个裸码，
	// 一个商品的变体数不会接近这个量级。
	inventoryPurchaseStockProbeSize = 200
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

	// 读侧一律经白名单出口（见 inventory_page_handle.go 的 inventoryPageErr）：查询参数不是可信边界。
	pageErr := inventoryPageErr(c)
	filterStatus := strings.TrimSpace(c.Query("status"))
	filterSource := strings.TrimSpace(c.Query("sourceId"))
	filterKeyword := strings.TrimSpace(c.Query("keyword"))

	// 本页承载两个入库入口：采购单 → 逐行收货，以及自家工厂**生产入库**
	// （无采购单、成本价手工填，验收 5）。进货历史不在这里 —— 它就是「按 SKU 看库存流水」，
	// 已并入库存管理页的流水筛选（同一查询对象的另一个视角）。
	sources := sourceOptions(ctx, h.inventory, selected, "")
	// 生产入库的来源只列**内部货源**（自家工厂 / 集团内关联公司）：外部供应商走采购单，
	// 没有「无采购单的生产入库」这一说（服务端同样拒绝，见 ErrProductionSourceNotInternal）。
	internalSources := sourceOptions(ctx, h.inventory, selected, inventoryenums.SourceTypeInternal)
	warehouses := h.purchaseWarehouseOptions(ctx, selected)
	variants := h.purchaseVariantOptions(ctx, selected)
	orders := h.purchaseOrderRows(c, selected, filterStatus, filterSource, filterKeyword, &pageErr)

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
		// 一次性幂等键：每个写表单一次渲染一个，双击提交只会产生一张入库单 / 一次入库。
		// 两个入口**各用各的键**：幂等键在入库单上是全局唯一的，共用一个键会让
		// 「先收采购货，再做生产入库」的第二跳被判成同一张单的重放而静默丢弃。
		"ReceiptRequestID":    uuid.NewString(),
		"ProductionRequestID": uuid.NewString(),
		"Err":                 pageErr,
		"Ok":                  inventoryPageOk(c),
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
//
// SKU 走本页统一的「三段值」选择器（purchaseLineSkuField）：一个原生下拉只能提交一个值，
// 而一行的变体 / 商品 / 仓库侧裸码必须**同源**（都取自候选列表的同一行），所以用
// "<变体ID>|<商品ID>|仓库侧裸码>" 拼成一个值 —— 与新建采购单的采购行逐字同一口径，
// 解析也用同一个 parsePurchaseSkuRef。字段名两边（模板 + 本文件）必须一起改。
func (h *inventoryPurchasePageHandle) InventoryPurchaseProduction(c *gin.Context) {
	projectID := c.PostForm("projectId")
	variantID, productID, skuCode := parsePurchaseSkuRef(c.PostForm(purchaseLineSkuField))
	req := &inventorydto.ProductionInboundReq{
		ProjectID:   projectID,
		SourceID:    strings.TrimSpace(c.PostForm("sourceId")),
		WarehouseID: strings.TrimSpace(c.PostForm("warehouseId")),
		VariantID:   variantID,
		ProductID:   productID,
		SKUCode:     skuCode,
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

// redirectPurchaseErr 回采购页并回显失败原因。
//
// 经 inventoryErrText 转成**当前语言的文案**再带回：业务错误（enums 常量值即 i18n key）
// 查词条 / 中文兜底，非业务错误（表名、SQL、约束名那类）只给归口文案，原文进结构化日志。
// 直接 url.QueryEscape(err.Error()) 会把裸 key 或驱动错误铺到页面上 —— 那与库存页已收口的
// 回显口径不一致，正是本批要消除的第三类泄漏面（重定向 query 形态）。
func redirectPurchaseErr(c *gin.Context, projectID string, err error) {
	c.Redirect(http.StatusFound, inventoryErrURL(c, inventoryPurchasesPath, projectID, err))
}
