// 采购单后台页：建单、登记入库、生产入库与单据行。
package inventoryhttp

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"strconv"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

// purchaseCreateFail 在抽屉内回显原始输入，采购行的空位也不压缩。
func (h *inventoryPurchasePageHandle) purchaseCreateFail(c *gin.Context, err error) {
	// 原生分支（无 JS）：失败渲染整页提示（取代原先的 302 + ?err=）。
	if !isHXRequest(c) {
		inventoryFail(c, err)
		return
	}
	// htmx 分支：4xx + JSON —— 抽屉不 swap，输入原样保留，admin.js 弹 toast。
	inventoryFail(c, err)
}

// inventory_purchase_page_form.go - 采购入库表单解析（收货明细行、草稿行与数值解析）。

// purchaseDraftLines 新建窗体里的空行（模板 range 用）。
func purchaseDraftLines() []inventorydto.DraftLineViewResp {
	out := make([]inventorydto.DraftLineViewResp, 0, inventoryPurchaseDraftLines)
	for i := 0; i < inventoryPurchaseDraftLines; i++ {
		out = append(out, inventorydto.DraftLineViewResp{Index: i})
	}
	return out
}

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
func (h *inventoryPurchasePageHandle) setProductCatalog(products catalogProduct) {
	h.products = products
}

// inventoryPurchasesPageData 采购入库页的模板数据（正常渲染与「装载失败降级渲染」共用一份拼装）。
//
// 与货源页同源的理由：模板的顶层 let（selectedProject / orders / filter* / receiptRequestID）
// 与页面直接读取的键都必须存在，缺一个就是整页中断（HTTP 200 + 后面整块 HTML 消失）。
type inventoryPurchasesPageData struct {
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Sources         []inventorydto.SourceOptionViewResp
	InternalSources []inventorydto.SourceOptionViewResp
	Warehouses      []inventorydto.WarehouseOptionViewResp
	VariantOptions  []inventorydto.VariantOptionViewResp
	Orders          []inventorydto.PurchaseOrderViewResp
	FilterStatus    string
	FilterSource    string
	FilterKeyword   string
	// Err 页面提示条：只剩一处来源 —— 本页取数失败（工程列表 / 采购单列表）。
	// 写动作的结论不再回带（走 shell.RenderJump，见 inventory_notice_handle.go）。
	Err string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）：与 order 域订单页 / 退货页同名，
	// 模板据此把「真的没有货源 / 没有采购单」与「这一次没读出来」分开 —— 前者引导去建一个，
	// 后者只能说明稍后重试，把人引向新建抽屉是错的。
	LoadFailed bool
	// Pagination 分页条事实（零值 = 单页 / 装载失败，HasPagination 为假，模板不渲染）。
	//
	// 放在结构体里而不是在渲染处临时拼：正常与降级两条路共用一份 templateMap，
	// 降级分支才不会「忘记」给某个键（Jet 缺 key 是整页中断）。
	Pagination pagination.Page
}

// templateMap 转 Jet 模板键（一次性幂等键在这里生成：每次渲染一个新的，双击提交即被挡住）。
//
// c 只用于取词：状态下拉文案按请求语言渲染。
func (d *inventoryPurchasesPageData) templateMap(c *gin.Context) gin.H {
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
		"StatusOptions":   purchaseStatusOptions(shell.TranslateFor(c)),
		"FilterStatus":    d.FilterStatus,
		"FilterSource":    d.FilterSource,
		"FilterKeyword":   d.FilterKeyword,
		// 一次性幂等键：每个写表单一次渲染一个，双击提交只会产生一张入库单 / 一次入库。
		// 两个入口**各用各的键**：幂等键在入库单上是全局唯一的，共用一个键会让
		// 「先收采购货，再做生产入库」的第二跳被判成同一张单的重放而静默丢弃。
		"ReceiptRequestID":    uuid.NewString(),
		"ProductionRequestID": uuid.NewString(),
		"Err":                 d.Err,
		"LoadFailed":          d.LoadFailed,
		// 写动作表单 action 上带的筛选上下文（见 inventory_notice_handle.go）。
		"ListQuery": inventoryQueryFromRequest(c, inventoryPurchaseBackKeys...),
	}
	// 分页条事实交给 partial 决定：零值（单页）与装载失败两条路 HasPagination 都为假，
	// 片段自然不渲染。
	m["Pagination"] = d.Pagination
	return m
}

// purchasesPageView 采购页的视图（正常 / 装载失败两条路共用）。
func (h *inventoryPurchasePageHandle) purchasesPageView(c *gin.Context, d *inventoryPurchasesPageData) any {
	return d.templateMap(c)
}

// InventoryPurchasesPage 采购入库页：新建采购单 + 采购单列表（含逐行收货）+ 生产入库 + 进货历史。
func (h *inventoryPurchasePageHandle) InventoryPurchasesPage(c *gin.Context) (any, error) {
	ctx := c.Request.Context()

	// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 inventory_notice_handle.go），
	// 所以提示条只剩一处来源：本页取数失败（工程列表 / 采购单列表）。
	pageErr := ""

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据与货源页 / order 域订单页 / project 域主题页一致）：
	// 空数据 + 归口提示 + HTTP 200，页头 / 筛选栏 / 侧栏全部保留。
	//
	// 原先这里是 `c.String(500, adminui.MsgInternalError)`：页面上就是 `MsgInternalError`
	// 这串英文 —— 归口 key 未经翻译直出，症状比脱壳更隐蔽（看起来像后台坏了）。
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

	sources := []inventorydto.SourceOptionViewResp{}
	internalSources := []inventorydto.SourceOptionViewResp{}
	warehouses := []inventorydto.WarehouseOptionViewResp{}
	variants := []inventorydto.VariantOptionViewResp{}
	orders := []inventorydto.PurchaseOrderViewResp{}
	var total int64
	// 分页（审计 D3）：?page= / ?limit=，缺省每页 100 张单据。原先这一页写死 100 且
	// 没有分页条 —— 第 101 张采购单静默消失，页面上没有任何提示。
	page, limit := inventoryPageParams(c, inventoryPurchasePageSize, inventoryPurchaseMaxPageSize)
	if !loadFailed {
		// 本页承载两个入库入口：采购单 → 逐行收货，以及自家工厂**生产入库**
		// （无采购单、成本价手工填，验收 5）。进货历史不在这里 —— 它就是「按 SKU 看库存流水」，
		// 已并入库存管理页的流水筛选（同一查询对象的另一个视角）。
		tr := shell.TranslateFor(c)
		sources = sourceOptions(ctx, h.inventory, selected, "", tr)
		// 生产入库的来源只列**内部货源**（自家工厂 / 集团内关联公司）：外部供应商走采购单，
		// 没有「无采购单的生产入库」这一说（服务端同样拒绝，见 ErrProductionSourceNotInternal）。
		internalSources = sourceOptions(ctx, h.inventory, selected, inventoryenums.SourceTypeInternal, tr)
		warehouses = h.purchaseWarehouseOptions(ctx, selected, tr)
		variants = h.purchaseVariantOptions(ctx, selected, tr)
		orders, total, page = h.purchaseOrderRows(c, selected, filterStatus, filterSource, filterKeyword,
			page, limit, &pageErr)
	}

	// 分页条（nil = 单页 / 空数据 / 装载失败，模板不渲染）：基址带当前全部筛选维度，
	// 翻页不丢条件。total 来自契约的 CountPurchaseOrders（与 ListPurchaseOrders
	// 同一份过滤条件），因此给的是页码窗口与「共 N 条」。
	// 装载失败时不构造分页条：工程上下文没定下来，URL 上的 page 不是有效页码。
	var pg pagination.Page
	if !loadFailed {
		pg = pagination.Facts(total, page, limit)
		pg.Base = reqparams.FilterBaseURL(inventoryPurchasesPath, map[string]string{
			"project":  selected,
			"status":   filterStatus,
			"sourceId": filterSource,
			"keyword":  filterKeyword,
		})
	}

	return h.purchasesPageView(c, &inventoryPurchasesPageData{
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
		LoadFailed:      loadFailed,
		Pagination:      pg,
	}), nil
}

// InventoryPurchaseCreate 新建采购单（单头 + 结构化行）。
func (h *inventoryPurchasePageHandle) InventoryPurchaseCreate(c *gin.Context) {
	form, ok := binding.Form[inventorydto.PurchaseCreateFormReq](c)
	if !ok {
		return
	}
	req := form.CreateReq(builtin.GetUsername(c))
	if _, err := h.inventory.CreatePurchaseOrder(c.Request.Context(), req); err != nil {
		h.purchaseCreateFail(c, err)
		return
	}
	inventoryDone(c, inventoryPurchaseBack(c))
}

// InventoryPurchaseReceipt 登记某一行（或多行）采购收货。
//
// 表单是「按行提交」：每行一个独立表单，因此这里只需处理一行；接口侧的批量收货运维用。
func (h *inventoryPurchasePageHandle) InventoryPurchaseReceipt(c *gin.Context) {
	form, ok := binding.Form[inventorydto.PurchaseReceiptFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.RegisterReceipt(c.Request.Context(), form.RegisterReq(builtin.GetUsername(c))); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryDone(c, inventoryPurchaseBack(c))
}

// InventoryPurchaseProduction 自家工厂生产入库（无采购单，成本价手工填写）。
//
// SKU 走本页统一的「三段值」选择器（purchaseLineSkuField）：一个原生下拉只能提交一个值，
// 而一行的变体 / 商品 / 仓库侧裸码必须**同源**（都取自候选列表的同一行），所以用
// "<变体ID>|<商品ID>|仓库侧裸码>" 拼成一个值 —— 与新建采购单的采购行逐字同一口径，
// 解析也用同一个 parsePurchaseSkuRef。字段名两边（模板 + 本文件）必须一起改。
func (h *inventoryPurchasePageHandle) InventoryPurchaseProduction(c *gin.Context) {
	form, ok := binding.Form[inventorydto.ProductionFormReq](c)
	if !ok {
		return
	}
	if _, err := h.inventory.RegisterProductionInbound(c.Request.Context(), form.ProductionReq(builtin.GetUsername(c))); err != nil {
		inventoryFail(c, err)
		return
	}
	inventoryDone(c, inventoryPurchaseBack(c))
}

// —— 页面取数（视图组装：模板不做逻辑）——

// inventory_purchase_page_view.go - 采购入库页的视图构造（采购单/历史行、货源与仓库/变体下拉、状态文案）。

// purchaseOrderRows 采购单列表 + 逐行视图（含「还能收多少」与一次性幂等键）。
//
// 取数失败回显走 inventoryErrText（业务 key → 当前语言文案，其余 → 归口文案 + 结构化日志），
// 与写路径同一口径：列表页的失败提示不允许比写路径多泄漏一个字符。
//
// total 是该过滤条件下的**真实总张数**（契约的 CountPurchaseOrders，与 ListPurchaseOrders
// 同一份过滤条件）；curPage 是**收敛后**的页码（page 越界时回落到末页）。out 只装本页数据。
func (h *inventoryPurchasePageHandle) purchaseOrderRows(c *gin.Context, projectID, status, sourceID, keyword string,
	page, limit int, pageErr *string) (out []inventorydto.PurchaseOrderViewResp, total int64, curPage int) {
	ctx := c.Request.Context()
	out = []inventorydto.PurchaseOrderViewResp{}
	if projectID == "" {
		return out, 0, 1
	}
	// 过滤条件只构造一次：列表与计数各自复制、只加各自的 Page/Size ——
	// 两处各写一份时，日后新增一个筛选维度只改到列表那一侧，「共 N 条」就会与实际条数矛盾。
	filterReq := &inventorydto.ListPurchaseOrderReq{
		ProjectID: projectID, Status: status, SourceID: sourceID, Keyword: keyword,
	}
	// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampInventoryPage）：
	// 反过来时 page 越界会让列表返回空页，而分页条按收敛后的页码渲染。
	n, cerr := h.inventory.CountPurchaseOrders(ctx, filterReq)
	if cerr != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, cerr)
		}
		return out, 0, page
	}
	total = n
	curPage = clampInventoryPage(page, limit, total)
	listReq := *filterReq
	listReq.Page, listReq.Size = curPage, limit
	orders, err := h.inventory.ListPurchaseOrders(ctx, &listReq)
	if err != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out, total, curPage
	}
	tr := shell.TranslateFor(c)
	for _, o := range orders {
		lines := make([]inventorydto.PurchaseOrderLineViewResp, 0, len(o.Lines))
		for _, l := range o.Lines {
			lines = append(lines, inventorydto.PurchaseOrderLineViewResp{
				ID: l.ID, SKUCode: l.SKUCode, VariantID: l.VariantID,
				Quantity: l.Quantity, ReceivedQuantity: l.ReceivedQuantity,
				OutstandingQuantity: l.OutstandingQuantity,
				UnitPrice:           strconv.FormatFloat(l.UnitPrice, 'f', 2, 64),
				Remark:              l.Remark,
				Open:                l.OutstandingQuantity > 0,
				OutstandingTone:     outstandingTone(l.OutstandingQuantity),
			})
		}
		out = append(out, inventorydto.PurchaseOrderViewResp{
			ID: o.ID, Code: o.Code,
			SourceName: o.SourceName, SourceType: o.SourceType,
			SourceTypeLabel: sourceTypeLabel(tr, o.SourceType),
			WarehouseName:   o.WarehouseName,
			Status:          o.Status, StatusLabel: purchaseStatusLabel(tr, o.Status),
			OrderedAt: o.OrderedAt, ExpectedAt: o.ExpectedAt,
			Remark: o.Remark, OperatorID: o.OperatorID,
			TotalQuantity: o.TotalQuantity, ReceivedQuantity: o.ReceivedQuantity,
			Lines:     lines,
			RequestID: uuid.NewString(),
		})
	}
	return out, total, curPage
}

// purchaseHistoryRows 按 SKU（可空 = 全部）取进货历史。
//
// 当前页面不再渲染这张表（「按 SKU 看过往入库」已并入库存管理页的流水筛选），
// 但取数口保留：接口侧与将来的页面入口共用同一投影。错误回显同样走 inventoryErrText——
// 留着 err.Error() 就等于在同一个文件里留了一条尚未被页面用到的泄漏路径。
func (h *inventoryPurchasePageHandle) purchaseHistoryRows(c *gin.Context, projectID, sku string, pageErr *string) (out []inventorydto.PurchaseHistoryViewResp) {
	ctx := c.Request.Context()
	out = []inventorydto.PurchaseHistoryViewResp{}
	if projectID == "" {
		return out
	}
	rows, err := h.inventory.ListPurchaseHistory(ctx, &inventorydto.ListPurchaseHistoryReq{
		ProjectID: projectID, SKUCode: sku, Size: inventoryPurchaseHistorySize,
	})
	if err != nil {
		if *pageErr == "" {
			*pageErr = inventoryErrText(c, err)
		}
		return out
	}
	tr := shell.TranslateFor(c)
	for _, r := range rows {
		out = append(out, inventorydto.PurchaseHistoryViewResp{
			ReceiptCode: r.ReceiptCode, Kind: r.Kind, KindLabel: receiptKindLabel(tr, r.Kind),
			OrderCode: r.OrderCode, SourceName: r.SourceName,
			SourceTypeLabel: sourceTypeLabel(tr, r.SourceType),
			WarehouseName:   r.WarehouseName, SKUCode: r.SKUCode,
			Quantity: r.Quantity, UnitPrice: strconv.FormatFloat(r.UnitPrice, 'f', 2, 64),
			CostUpdated: r.CostUpdated, Remark: r.Remark,
			OperatorID: r.OperatorID, ReceivedAt: r.ReceivedAt,
		})
	}
	return out
}

// sourceOptions 货源下拉（sourceType 非空时只看该类型：内部类型 = 自家工厂 / 集团内关联公司）。
//
// 包级函数而非某个 handler 的方法：库存管理与采购入库两个页面都要用 ——
// 「生产入库」已从采购页归位到库存管理（它与采购单无关，本质是「手动改库存 + 写成本价」）。
func sourceOptions(ctx context.Context, svc inventorycontract.InventoryService, projectID, sourceType string,
	tr func(key, fallback string) string) (out []inventorydto.SourceOptionViewResp) {
	out = []inventorydto.SourceOptionViewResp{}
	if projectID == "" {
		return out
	}
	rows, err := svc.ListSources(ctx, &inventorydto.ListSourceReq{
		ProjectID: projectID, Type: sourceType,
	})
	if err != nil {
		return out
	}
	for _, s := range rows {
		out = append(out, inventorydto.SourceOptionViewResp{
			ID: s.ID, Code: s.Code, Name: s.Name, Type: s.Type,
			Label: sourceOptionLabel(tr, s),
		})
	}
	return out
}

// sourceOptionLabel 货源下拉项的文案：「名称（编码）· 类型」。类型走取词（见 sourceTypeLabel）。
func sourceOptionLabel(tr func(key, fallback string) string, s *inventorydto.SourceResp) string {
	return s.Name + "（" + s.Code + "）· " + sourceTypeLabel(tr, s.Type)
}

// purchaseWarehouseOptions 收货仓下拉（默认仓标出来；未选即兜底默认仓）。
func (h *inventoryPurchasePageHandle) purchaseWarehouseOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []inventorydto.WarehouseOptionViewResp) {
	out = []inventorydto.WarehouseOptionViewResp{}
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
			label += " " + tr(inventoryenums.InventoryChangeWarehouseDefaultSuffix, "· 默认仓")
		}
		out = append(out, inventorydto.WarehouseOptionViewResp{ID: w.ID, Label: label, IsDefault: w.IsDefault})
	}
	return out
}

// purchaseVariantOptions 变体下拉（带商品名 / SKU / 当前成本价，选行时一眼可见成本口径）。
//
// 每一项额外带 **BareSKU（仓库侧裸码）**：采购行的 sku_code 是仓库侧快照，必须按裸码落库
// （仓库里的 SKU 永远不带仓码前缀，见 docs/14 §1.1 与迁移 262）。裸码取自本模块自己的
// 库存真源（inventory_stocks.sku_code）—— 它就是「这条货在仓库里叫什么」的权威答案，
// 商品侧的 v.SKUCode 只是它加了认领仓前缀的投影。没有库存行的变体退回商品侧编码，
// 由服务端在入库入口按目标仓短码归一（inventory_stock.go）。
func (h *inventoryPurchasePageHandle) purchaseVariantOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []inventorydto.VariantOptionViewResp) {
	out = []inventorydto.VariantOptionViewResp{}
	// 商品契约未注入（装配漏接）时下拉为空，页面照常渲染 —— 不因一处装配缺失 500。
	if projectID == "" || h.products == nil {
		return out
	}
	list, err := h.products.List(ctx, &productdto.ListReq{ProjectID: projectID, Size: 100})
	if err != nil {
		return out
	}
	// 成本后缀是一句带命名占位符的文案：取词后用 {value} 填充（词条被写坏时 FillTranslate
	// 自动回落下面那句中文兜底，页面上不会出现 {value} 这样的字面量）。
	costSuffix := func(cost string) string {
		return i18n.FillTranslate(tr, inventoryenums.InventoryPurchasesOptionCostLabel, "（成本 {value}）",
			map[string]string{"value": cost})
	}
	unsetCost := tr(inventoryenums.InventoryPurchasesOptionCostUnset, "未填")
	for _, p := range list {
		detail, derr := h.products.Get(ctx, &productdto.GetReq{ID: p.ID})
		if derr != nil {
			continue
		}
		bare := h.productBareSKUs(ctx, projectID, p.ID)
		for _, v := range detail.Variants {
			cost := unsetCost
			if v.CostPrice != nil {
				cost = strconv.FormatFloat(*v.CostPrice, 'f', 2, 64)
			}
			sku := bare[v.ID]
			if sku == "" {
				sku = v.SKUCode
			}
			out = append(out, inventorydto.VariantOptionViewResp{
				VariantID: v.ID, ProductID: p.ID, SKUCode: v.SKUCode, BareSKU: sku,
				Label: detail.Name + " · " + v.SKUCode + costSuffix(cost),
			})
		}
	}
	return out
}

// productBareSKUs 某商品全部变体的**仓库侧裸码**（变体 id → inventory_stocks.sku_code）。
//
// 一次取回该商品在各仓的库存行（按商品过滤，与页面既有的「逐商品取详情」同量级），
// 变体在哪个仓有行都算 —— 裸码是变体的仓库侧身份，与具体哪个仓无关。
// 读失败不抛给页面（返回空 map，调用方退回商品侧编码）：采购页不该因为一次展示取数
// 失败而 500，真正的守门在入库入口的归一路径上。
func (h *inventoryPurchasePageHandle) productBareSKUs(ctx context.Context, projectID, productID string) map[string]string {
	out := map[string]string{}
	rows, err := h.inventory.ListStocks(ctx, &inventorydto.ListStockReq{
		ProjectID: projectID, ProductID: productID, Size: inventoryPurchaseStockProbeSize,
	})
	if err != nil {
		return out
	}
	for _, r := range rows {
		if r.VariantID != "" && r.SKUCode != "" {
			out[r.VariantID] = r.SKUCode
		}
	}
	return out
}

// —— 表单与文案工具 ——

// purchaseStatusOptions 状态筛选下拉（三种推导值）。
func purchaseStatusOptions(tr func(key, fallback string) string) []inventorydto.OptionResp {
	return []inventorydto.OptionResp{
		{Value: inventoryenums.PurchaseStatusPending, Label: purchaseStatusLabel(tr, inventoryenums.PurchaseStatusPending)},
		{Value: inventoryenums.PurchaseStatusPartial, Label: purchaseStatusLabel(tr, inventoryenums.PurchaseStatusPartial)},
		{Value: inventoryenums.PurchaseStatusReceived, Label: purchaseStatusLabel(tr, inventoryenums.PurchaseStatusReceived)},
	}
}

// purchaseStatusLabel 推导状态 → 当前语言展示文案。
func purchaseStatusLabel(tr func(key, fallback string) string, status string) string {
	switch status {
	case inventoryenums.PurchaseStatusPending:
		return tr(inventoryenums.InventoryPurchasesStatusPending, "未入库")
	case inventoryenums.PurchaseStatusPartial:
		return tr(inventoryenums.InventoryPurchasesStatusPartial, "部分入库")
	case inventoryenums.PurchaseStatusReceived:
		// 已入库：与同页列头同一个词（英文都是 Received），复用它的词条。
		return tr(inventoryenums.InventoryPurchasesColReceived, "已入库")
	default:
		return status
	}
}

// receiptKindLabel 入库单类型 → 当前语言展示文案。
func receiptKindLabel(tr func(key, fallback string) string, kind string) string {
	switch kind {
	case inventoryenums.ReceiptKindPurchase:
		return tr(inventoryenums.InventoryPurchasesKindPurchase, "采购收货")
	case inventoryenums.ReceiptKindProduction:
		// 生产入库：与采购页的「生产入库」入口同一个词，复用它的词条。
		return tr(inventoryenums.InventoryPurchasesProductionOpen, "生产入库")
	default:
		return kind
	}
}

// sourceStatusFilterAll 状态筛选的「全部（含停用）」取值。
const sourceStatusFilterAll = "all"

// inventorySourcePageSize 货源页一次列出的条数（每页条数；完整清单靠分页翻）。
//
// 原先它同时充当「硬编码上限」—— 第 201 个货源静默消失且页面上没有任何提示。
// 现在它是**每页条数**（可用 ?limit= 调小，上限见 inventorySourceMaxPageSize），
// 页面下方给分页条，第 201 个之后靠翻页看到。
const inventorySourcePageSize = 200

// inventorySourceMaxPageSize 货源页每页条数的上限（?limit= 的封顶值）。
//
// 与 service 的 maxSourcePageSize 同口径：超过它 service 会自己截到 200，
// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 「翻页少一截」这类缺陷
// 页面不报错，只是数据对不上，最难查。
const inventorySourceMaxPageSize = 200
