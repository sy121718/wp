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
	// inventoryPurchasePageSize 采购单页一次列出的单据数（每页条数；完整清单靠分页翻）。
	//
	// 原先它同时充当「硬编码上限」—— 第 101 张采购单静默消失且页面上没有任何提示。
	// 现在它是**每页条数**（可用 ?limit= 调小，上限见 inventoryPurchaseMaxPageSize），
	// 页面下方给分页条。
	inventoryPurchasePageSize = 100
	// inventoryPurchaseMaxPageSize 采购页每页条数的上限（?limit= 的封顶值）。
	//
	// 与 service 的 maxPurchasePageSize 同口径：超过它 service 会自己截到 200，
	// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 「翻页少一截」这类缺陷
	// 页面不报错，只是数据对不上。
	inventoryPurchaseMaxPageSize = 200
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

// inventoryPurchasesPageData 采购入库页的模板数据（正常渲染与「装载失败降级渲染」共用一份拼装）。
//
// 与货源页同源的理由：模板的顶层 let（selectedProject / orders / filter* / receiptRequestID）
// 与页面直接读取的键都必须存在，缺一个就是整页中断（HTTP 200 + 后面整块 HTML 消失）。
type inventoryPurchasesPageData struct {
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Sources         []gin.H
	InternalSources []gin.H
	Warehouses      []gin.H
	VariantOptions  []gin.H
	Orders          []gin.H
	FilterStatus    string
	FilterSource    string
	FilterKeyword   string
	Err             string
	Ok              string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）：与 order 域订单页 / 退货页同名，
	// 模板据此把「真的没有货源 / 没有采购单」与「这一次没读出来」分开 —— 前者引导去建一个，
	// 后者只能说明稍后重试，把人引向新建抽屉是错的。
	LoadFailed bool
	// Pagination 分页条数据（nil = 单页 / 装载失败，模板不渲染分页条）。
	//
	// 放在结构体里而不是在渲染处临时拼：正常与降级两条路共用一份 templateMap，
	// 降级分支才不会「忘记」给某个键（Jet 缺 key 是整页中断）。
	Pagination *shell.PaginationData
}

// templateMap 转 Jet 模板键（一次性幂等键在这里生成：每次渲染一个新的，双击提交即被挡住）。
func (d *inventoryPurchasesPageData) templateMap() gin.H {
	m := gin.H{
		"title":           inventoryenums.MsgInventoryPurchasesTitle,
		"menu":            "inventory-purchases",
		"Projects":        d.Projects,
		"SelectedProject": d.SelectedProject,
		"Sources":         d.Sources,
		"InternalSources": d.InternalSources,
		"Warehouses":      d.Warehouses,
		"VariantOptions":  d.VariantOptions,
		"DraftLines":      purchaseDraftLines(),
		"Orders":          d.Orders,
		"StatusOptions":   purchaseStatusOptions(),
		"FilterStatus":    d.FilterStatus,
		"FilterSource":    d.FilterSource,
		"FilterKeyword":   d.FilterKeyword,
		// 一次性幂等键：每个写表单一次渲染一个，双击提交只会产生一张入库单 / 一次入库。
		// 两个入口**各用各的键**：幂等键在入库单上是全局唯一的，共用一个键会让
		// 「先收采购货，再做生产入库」的第二跳被判成同一张单的重放而静默丢弃。
		"ReceiptRequestID":    uuid.NewString(),
		"ProductionRequestID": uuid.NewString(),
		"Err":                 d.Err,
		"Ok":                  d.Ok,
		"LoadFailed":          d.LoadFailed,
	}
	// 分页条键（PaginationInfo / PaginationLinks）：nil 时给空 map，模板的
	// {{if .["PaginationLinks"]}} 自然跳过 —— 单页与装载失败两条路都不渲染分页条。
	for k, v := range d.Pagination.TemplateKeys() {
		m[k] = v
	}
	return m
}

// renderPurchasesPage 采购页的唯一渲染出口（正常 / 装载失败两条路共用）。
func (h *inventoryPurchasePageHandle) renderPurchasesPage(c *gin.Context, d *inventoryPurchasesPageData) {
	c.HTML(http.StatusOK, "admin/inventory/inventory_purchases.html", shell.Prepare(c, d.templateMap()))
}

// InventoryPurchasesPage 采购入库页：新建采购单 + 采购单列表（含逐行收货）+ 生产入库 + 进货历史。
func (h *inventoryPurchasePageHandle) InventoryPurchasesPage(c *gin.Context) {
	ctx := c.Request.Context()

	// 回显文案先过读侧白名单（见 inventory_page_handle.go 的 inventoryPageErr）：查询参数不是可信边界。
	pageErr := inventoryPageErr(c)

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据与货源页 / order 域订单页 / project 域主题页一致）：
	// 空数据 + 归口提示 + HTTP 200，页头 / 筛选栏 / 侧栏全部保留。
	//
	// 原先这里是 `c.String(500, shell.MsgInternalError)`：页面上就是 `MsgInternalError`
	// 这串英文 —— 归口 key 未经翻译直出，症状比脱壳更隐蔽（看起来像后台坏了）。
	//
	// 装载失败**压过 ?err=**：它是这次请求真实发生的事，URL 里那条是上一次写失败的旧提示。
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = inventoryErrText(c, loadErr)
	}

	// 装载失败时不再去读列表与各下拉候选：工程上下文没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查采购单，查出来的是哪个工程的单据都说不清。
	selected := ""
	if !loadFailed {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	filterStatus := strings.TrimSpace(c.Query("status"))
	filterSource := strings.TrimSpace(c.Query("sourceId"))
	filterKeyword := strings.TrimSpace(c.Query("keyword"))

	sources := []gin.H{}
	internalSources := []gin.H{}
	warehouses := []gin.H{}
	variants := []gin.H{}
	orders := []gin.H{}
	var total int64
	// 分页（审计 D3）：?page= / ?limit=，缺省每页 100 张单据。原先这一页写死 100 且
	// 没有分页条 —— 第 101 张采购单静默消失，页面上没有任何提示。
	page, limit := inventoryPageParams(c, inventoryPurchasePageSize, inventoryPurchaseMaxPageSize)
	if !loadFailed {
		// 本页承载两个入库入口：采购单 → 逐行收货，以及自家工厂**生产入库**
		// （无采购单、成本价手工填，验收 5）。进货历史不在这里 —— 它就是「按 SKU 看库存流水」，
		// 已并入库存管理页的流水筛选（同一查询对象的另一个视角）。
		sources = sourceOptions(ctx, h.inventory, selected, "")
		// 生产入库的来源只列**内部货源**（自家工厂 / 集团内关联公司）：外部供应商走采购单，
		// 没有「无采购单的生产入库」这一说（服务端同样拒绝，见 ErrProductionSourceNotInternal）。
		internalSources = sourceOptions(ctx, h.inventory, selected, inventoryenums.SourceTypeInternal)
		warehouses = h.purchaseWarehouseOptions(ctx, selected)
		variants = h.purchaseVariantOptions(ctx, selected)
		orders, total, page = h.purchaseOrderRows(c, selected, filterStatus, filterSource, filterKeyword,
			page, limit, &pageErr)
	}

	// 分页条（nil = 单页 / 空数据 / 装载失败，模板不渲染）：基址带当前全部筛选维度，
	// 翻页不丢条件。total 来自契约的 CountPurchaseOrders（与 ListPurchaseOrders
	// 同一份过滤条件），因此给的是页码窗口与「共 N 条」。
	// 装载失败时不构造分页条：工程上下文没定下来，URL 上的 page 不是有效页码。
	var pagination *shell.PaginationData
	if !loadFailed {
		pagination = shell.BuildPagination(total, page, limit, shell.FilterBaseURL(
			inventoryPurchasesPath, map[string]string{
				"project":  selected,
				"status":   filterStatus,
				"sourceId": filterSource,
				"keyword":  filterKeyword,
			}), shell.TranslateFor(c))
	}

	h.renderPurchasesPage(c, &inventoryPurchasesPageData{
		Projects:        projects,
		SelectedProject: selected,
		Sources:         sources,
		InternalSources: internalSources,
		Warehouses:      warehouses,
		VariantOptions:  variants,
		Orders:          orders,
		FilterStatus:    filterStatus,
		FilterSource:    filterSource,
		FilterKeyword:   filterKeyword,
		Err:             pageErr,
		Ok:              inventoryPageOk(c),
		LoadFailed:      loadFailed,
		Pagination:      pagination,
	})
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
		h.purchaseCreateFail(c, projectID, err)
		return
	}
	purchaseCreateSuccess(c, projectID)
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
