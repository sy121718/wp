// 采购单后台页：建单、登记入库、生产入库与单据行。
package purchasehttp

import (
	"context"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"go_wp/internal/middleware/builtin"
	projectcontract "go_wp/internal/module/project/contract"
	purchasecontract "go_wp/internal/module/purchase/contract"
	purchasedto "go_wp/internal/module/purchase/dto"
	purchaseenums "go_wp/internal/module/purchase/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

const (
	// purchasePageSize 采购单页一次列出的单据数（每页条数；完整清单靠分页翻）。
	purchasePageSize = 100
	// purchaseMaxPageSize 采购页每页条数的上限（?limit= 的封顶值）。
	//
	// 与 service 的 maxPurchasePageSize 同口径：超过它 service 会自己截到 200，
	// 页面若允许更大的值，URL 上写着 500 而实际只回 200 —— 「翻页少一截」这类缺陷
	// 页面不报错，只是数据对不上。
	purchaseMaxPageSize = 200
	// purchasesPath 采购入库页路径（回跳与分页基址的唯一定义处）。
	purchasesPath = "/admin/inventory/purchases"
	// purchaseHistorySize 进货历史一次列出的条数（页面当前不再渲染这张表，
	// 取数口保留，见 purchaseHistoryRows）。
	purchaseHistorySize = 50
	// purchaseDraftLineCount 新建采购单表单的行数（原生表单：固定几行）。
	purchaseDraftLineCount = 3
)

// purchasePageHandle 采购入库页处理器。
type purchasePageHandle struct {
	purchase purchasecontract.PurchaseService
	stock    purchasecontract.StockPort
	projects projectcontract.ProjectService
	// catalog 只用于 SKU 下拉（商品 → 变体 + 仓库侧裸码），不参与任何库存 / 采购判断。
	catalog purchasecontract.VariantCatalog
}

// NewPurchasePageHandle 构造。
//
// catalog 只用于 SKU 下拉：装配期传入 nil，装配收尾由 SetVariantCatalog 补注
// （商品模块与本模块互为依赖，装配顺序上无法在此时拿到那条只读口）。
func NewPurchasePageHandle(purchase purchasecontract.PurchaseService, stock purchasecontract.StockPort,
	projects projectcontract.ProjectService) *purchasePageHandle {
	return &purchasePageHandle{purchase: purchase, stock: stock, projects: projects}
}

// setVariantCatalog 装配期补注商品/库存的候选只读口。
func (h *purchasePageHandle) setVariantCatalog(catalog purchasecontract.VariantCatalog) {
	h.catalog = catalog
}

// SetCatalog 直接给本处理器补注 SKU 候选只读口。
//
// 供「没走 SetupPurchasePages 的装配路径」使用：测试引擎自己 new 一个处理器，
// 走不到包级 pageCatalogConsumers 那条后置注入链。生产装配一律用包级 SetVariantCatalog。
func (h *purchasePageHandle) SetCatalog(catalog purchasecontract.VariantCatalog) {
	h.catalog = catalog
}

// purchasesPageData 采购入库页的模板数据（正常渲染与「装载失败降级渲染」共用一份拼装）。
//
// 与货源页同源的理由：模板的顶层 let（selectedProject / orders / filter* / receiptRequestID）
// 与页面直接读取的键都必须存在，缺一个就是整页中断（HTTP 200 + 后面整块 HTML 消失）。
type purchasesPageData struct {
	Title           string `view:"title"`
	Menu            string `view:"menu"`
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	Sources         []purchasedto.SourceOptionViewResp
	InternalSources []purchasedto.SourceOptionViewResp
	Warehouses      []purchasedto.WarehouseOptionViewResp
	VariantOptions  []purchasedto.VariantOptionViewResp
	Orders          []purchasedto.PurchaseOrderViewResp
	FilterStatus    string
	FilterSource    string
	FilterKeyword   string
	// Err 页面提示条：只剩一处来源 —— 本页取数失败（工程列表 / 采购单列表）。
	Err string
	// LoadFailed 本次请求的工程列表没读出来（降级渲染）。
	//
	// 模板据此把「真的没有采购单」与「这一次没读出来」分开 —— 前者引导去建一张，
	// 后者只能说明稍后重试，把人引向新建抽屉是错的。
	LoadFailed bool
	// Pagination 分页条事实（零值 = 单页 / 装载失败，HasPagination 为假，模板不渲染）。
	Pagination pagination.Page

	// DraftLines 新建窗体里的空行（模板 range 用）。每次渲染现算的常量形状，由装配处填。
	DraftLines []purchasedto.DraftLineViewResp
	// StatusOptions 状态下拉的文案（按请求语言）：取值需要 c，由装配处填。
	StatusOptions []purchasedto.OptionResp
	// ReceiptRequestID / ProductionRequestID 一次性幂等键：每个写表单一次渲染一个，
	// 双击提交只会产生一张入库单 / 一次入库。两个入口**各用各的键**：幂等键在入库单上
	// 是全局唯一的，共用一个键会让「先收采购货，再做生产入库」的第二跳被判成
	// 同一张单的重放而静默丢弃。
	ReceiptRequestID    string `view:"ReceiptRequestID"`
	ProductionRequestID string `view:"ProductionRequestID"`
	// ListQuery 写动作表单 action 上带的筛选上下文。
	ListQuery string
}

// draftLines 新建窗体里的空行列表。
func draftLines() []purchasedto.DraftLineViewResp {
	out := make([]purchasedto.DraftLineViewResp, 0, purchaseDraftLineCount)
	for i := 0; i < purchaseDraftLineCount; i++ {
		out = append(out, purchasedto.DraftLineViewResp{Index: i})
	}
	return out
}

// purchasesPageView 采购页的视图（正常 / 装载失败两条路共用）。
//
// 「两条路共用一份装配」由**同一个结构体**保证：字段是零值就是该分支的形态，
// 不再是两份 map 拼装互相抄（缺键在模板里是整页中断，而漏字段这里是零值）。
func (h *purchasePageHandle) purchasesPageView(c *gin.Context, d *purchasesPageData) any {
	if d.Title == "" {
		d.Title = purchaseenums.MsgInventoryPurchasesTitle
		d.Menu = "inventory-purchases"
	}
	d.DraftLines = draftLines()
	d.StatusOptions = purchaseStatusOptions(shell.TranslateFor(c))
	d.ReceiptRequestID = uuid.NewString()
	d.ProductionRequestID = uuid.NewString()
	d.ListQuery = binding.ListQuery(c, purchaseOrderBackKeys...)
	return d
}

// PurchasesPage 采购入库页：新建采购单 + 采购单列表（含逐行收货）+ 生产入库。
func (h *purchasePageHandle) PurchasesPage(c *gin.Context) {
	q, ok := binding.Query[purchasedto.PurchaseListQueryReq](c)
	if !ok {
		return
	}
	pageview.Render(c, h.purchasesPageView(c, h.purchasesPageFacts(c, q)))
}

// purchasesPageFacts 采集本页要展示的事实（列表 / 下拉候选 / 分页 / 装载失败）。
//
// 从 handler 里拆出来：门禁按「控制器只做绑定 → 调业务层 → 出口」卡语句数，
// 而这段是**取数与装配**，与出口同一份数据在两条渲染路径（正常 / 降级）间周转。
// 再往下拆一层（工程 → 下拉 → 列表）是因为方法行数门禁按 60 行卡，
// 而这三步的降级判据各不相同（工程失败连下拉都不读）。
func (h *purchasePageHandle) purchasesPageFacts(c *gin.Context, q *purchasedto.PurchaseListQueryReq) *purchasesPageData {
	d, selected, ok := h.purchasePageBasics(c, q)
	if !ok {
		return d
	}
	page, limit := purchasePageParams(q.Page, q.Limit, purchasePageSize, purchaseMaxPageSize)
	filter := purchaseOrderFilter{d: d, projectID: selected, page: page, limit: limit}
	filter.status, filter.sourceID, filter.keyword = strings.TrimSpace(q.Status),
		strings.TrimSpace(q.SourceID), strings.TrimSpace(q.Keyword)
	h.fillPurchaseOptions(c, d, selected)
	h.fillPurchaseOrders(c, &filter)
	return d
}

// purchasePageBasics 页头（工程上下文）与「装载失败」判定。
//
// 降级渲染的判据只有一处：工程列表读不出来就不再去读列表与下拉 ——
// 工程没定下来时查出来的单据属于哪个工程都说不清，比空列表更误导。
func (h *purchasePageHandle) purchasePageBasics(c *gin.Context, q *purchasedto.PurchaseListQueryReq) (
	d *purchasesPageData, selected string, ok bool) {
	projects, loadErr := h.projects.List(c.Request.Context())
	d = &purchasesPageData{
		Projects:        projects,
		Sources:         []purchasedto.SourceOptionViewResp{},
		InternalSources: []purchasedto.SourceOptionViewResp{},
		Warehouses:      []purchasedto.WarehouseOptionViewResp{},
		VariantOptions:  []purchasedto.VariantOptionViewResp{},
		Orders:          []purchasedto.PurchaseOrderViewResp{},
		FilterStatus:    strings.TrimSpace(q.Status),
		FilterSource:    strings.TrimSpace(q.SourceID),
		FilterKeyword:   strings.TrimSpace(q.Keyword),
	}
	if loadErr != nil {
		// 工程列表读不出来**不拿走整个页面**：空数据 + 归口提示 + HTTP 200，
		// 页头 / 筛选栏 / 侧栏全部保留。
		d.Projects, d.LoadFailed = nil, true
		d.Err = purchaseErrText(c, loadErr)
		return d, "", false
	}
	selected = strings.TrimSpace(q.ProjectID)
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	d.SelectedProject = selected
	return d, selected, true
}

// purchaseOrderFilter 采购单列表的筛选与分页（四项筛选 + 页码 + 每页条数）。
//
// 单独一个结构体而不是五个参数：这些值要同时喂给 Count 与 List（两者必须同源，
// 否则「共 N 条」与能翻出来的行数分叉，页面给出一个永远翻不到底的页数）。
type purchaseOrderFilter struct {
	d         *purchasesPageData
	projectID string
	status    string
	sourceID  string
	keyword   string
	page      int
	limit     int
}

// fillPurchaseOptions 三个下拉候选：货源 / 内部货源（生产入库）/ 收货仓 / SKU。
func (h *purchasePageHandle) fillPurchaseOptions(c *gin.Context, d *purchasesPageData, projectID string) {
	ctx, tr := c.Request.Context(), purchaseTranslate(c)
	d.Sources = h.sourceOptions(ctx, projectID, "", tr)
	// 生产入库的来源只列**内部货源**（自家工厂 / 集团内关联公司）：外部供应商走采购单，
	// 没有「无采购单的生产入库」这一说（服务端同样拒绝，见 ErrProductionSourceNotInternal）。
	d.InternalSources = h.sourceOptions(ctx, projectID, purchaseenums.SourceTypeInternal, tr)
	d.Warehouses = h.warehouseOptions(ctx, projectID, tr)
	d.VariantOptions = h.variantOptions(ctx, projectID, tr)
}

// fillPurchaseOrders 取当页采购单并装好分页条（基址带当前全部筛选维度，翻页不丢条件）。
func (h *purchasePageHandle) fillPurchaseOrders(c *gin.Context, f *purchaseOrderFilter) {
	var total int64
	f.d.Orders, total, f.page = h.purchaseOrderRows(c, f.projectID, f.status, f.sourceID, f.keyword,
		f.page, f.limit, &f.d.Err)
	// total 来自契约的 CountPurchaseOrders（与 ListPurchaseOrders 同一份过滤条件），
	// 因此这里给的是页码窗口与「共 N 条」。
	f.d.Pagination = pagination.Page{Total: total, Page: f.page, Limit: f.limit}
	f.d.Pagination.Base = reqparams.FilterBaseURL(purchasesPath, map[string]string{
		"project":  f.projectID,
		"status":   f.status,
		"sourceId": f.sourceID,
		"keyword":  f.keyword,
	})
}

// —— 写动作 ——

// PurchaseCreate 新建采购单（单头 + 结构化行）。
func (h *purchasePageHandle) PurchaseCreate(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseCreateFormReq](c)
	if !ok {
		return
	}
	req := form.CreateReq(builtin.GetUsername(c))
	if _, err := h.purchase.CreatePurchaseOrder(c.Request.Context(), req); err != nil {
		purchaseFail(c, err)
		return
	}
	response.Success(c, nil)
}

// PurchaseUpdate 修改采购单（行全量替换，仅未入库时；换货源 / 换收货仓）。
func (h *purchasePageHandle) PurchaseUpdate(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseCreateFormReq](c)
	if !ok {
		return
	}
	// 页面表单是「最终状态」语义：改后被清空的字段照样提交空串，
	// 因此这里把可空字段一律取地址传下去（service 判 nil = 不改、非 nil = 改成该值）。
	req := form.CreateReq(builtin.GetUsername(c))
	if _, err := h.purchase.UpdatePurchaseOrder(c.Request.Context(), &purchasedto.UpdatePurchaseOrderReq{
		ID:           strings.TrimSpace(form.ID),
		ProjectID:    req.ProjectID,
		SourceID:     &req.SourceID,
		WarehouseID:  &req.WarehouseID,
		Remark:       &req.Remark,
		OperatorID:   &req.OperatorID,
		ReplaceLines: true,
		Lines:        req.Lines,
	}); err != nil {
		purchaseFail(c, err)
		return
	}
	response.Success(c, nil)
}

// PurchaseReceipt 登记某一行（或多行）采购收货。
//
// 表单是「按行提交」：每行一个独立表单，因此这里只需处理一行；接口侧的批量收货运维用。
func (h *purchasePageHandle) PurchaseReceipt(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseReceiptFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.RegisterReceipt(c.Request.Context(), form.RegisterReq(builtin.GetUsername(c))); err != nil {
		purchaseFail(c, err)
		return
	}
	response.Success(c, nil)
}

// PurchaseProduction 自家工厂生产入库（无采购单，成本价手工填写）。
//
// SKU 走本页统一的「三段值」选择器（lineSku）：一个原生下拉只能提交一个值，
// 而一行的变体 / 商品 / 仓库侧裸码必须**同源**（都取自候选列表的同一行），所以用
// "<变体ID>|<商品ID>|仓库侧裸码>" 拼成一个值 —— 与新建采购单的采购行逐字同一口径，
// 解析也用同一个 parseSKURef。字段名两边（模板 + DTO）必须一起改。
func (h *purchasePageHandle) PurchaseProduction(c *gin.Context) {
	form, ok := binding.Form[purchasedto.ProductionFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.RegisterProductionInbound(c.Request.Context(), form.ProductionReq(builtin.GetUsername(c))); err != nil {
		purchaseFail(c, err)
		return
	}
	response.Success(c, nil)
}

// PurchaseFinish 采购单提前关单：received / partial → finished。
//
// 与取消分开两个入口而不是一个「改状态」接口：两个动作的前置状态不同
// （finished 要求已入库过、cancelled 要求还没收满），合成一个只会让调用方各自判一次
// 该传哪个值 —— 而判错的表现是「已收满的单被取消，货却已经进仓」。
func (h *purchasePageHandle) PurchaseFinish(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.FinishPurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		purchaseFail(c, err)
		return
	}
	response.Success(c, nil)
}

// PurchaseCancel 采购单取消：pending / partial → cancelled。
func (h *purchasePageHandle) PurchaseCancel(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.CancelPurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		purchaseFail(c, err)
		return
	}
	response.Success(c, nil)
}

// —— 页面取数（视图组装：模板不做逻辑）——

// purchaseOrderRows 采购单列表 + 逐行视图（含「还能收多少」与一次性幂等键）。
//
// 取数失败回显走 purchaseErrText（业务 key → 当前语言文案，其余 → 归口文案 + 结构化日志），
// 与写路径同一口径：列表页的失败提示不允许比写路径多泄漏一个字符。
//
// total 是该过滤条件下的**真实总张数**（契约的 CountPurchaseOrders，与 ListPurchaseOrders
// 同一份过滤条件）；curPage 是**收敛后**的页码（page 越界时回落到末页）。out 只装本页数据。
func (h *purchasePageHandle) purchaseOrderRows(c *gin.Context, projectID, status, sourceID, keyword string,
	page, limit int, pageErr *string) (out []purchasedto.PurchaseOrderViewResp, total int64, curPage int) {
	ctx := c.Request.Context()
	out = []purchasedto.PurchaseOrderViewResp{}
	if projectID == "" {
		return out, 0, 1
	}
	// 过滤条件只构造一次：列表与计数各自复制、只加各自的 Page/Size ——
	// 两处各写一份时，日后新增一个筛选维度只改到列表那一侧，「共 N 条」就会与实际条数矛盾。
	filterReq := &purchasedto.ListPurchaseOrderReq{
		ProjectID: projectID, Status: status, SourceID: sourceID, Keyword: keyword,
	}
	// 先计数、收敛页码，再取当页数据（顺序不能反，见 clampPurchasePage）。
	n, cerr := h.purchase.CountPurchaseOrders(ctx, filterReq)
	if cerr != nil {
		if *pageErr == "" {
			*pageErr = purchaseErrText(c, cerr)
		}
		return out, 0, page
	}
	total = n
	curPage = clampPurchasePage(page, limit, total)
	listReq := *filterReq
	listReq.Page, listReq.Size = curPage, limit
	orders, err := h.purchase.ListPurchaseOrders(ctx, &listReq)
	if err != nil {
		if *pageErr == "" {
			*pageErr = purchaseErrText(c, err)
		}
		return out, total, curPage
	}
	tr := purchaseTranslate(c)
	for _, o := range orders {
		lines := make([]purchasedto.PurchaseOrderLineViewResp, 0, len(o.Lines))
		for _, l := range o.Lines {
			lines = append(lines, purchasedto.PurchaseOrderLineViewResp{
				ID: l.ID, SKUCode: l.SKUCode, VariantID: l.VariantID,
				Quantity: l.Quantity, ReceivedQuantity: l.ReceivedQuantity,
				OutstandingQuantity: l.OutstandingQuantity,
				UnitPrice:           strconv.FormatFloat(l.UnitPrice, 'f', 2, 64),
				Remark:              l.Remark,
				Open:                l.OutstandingQuantity > 0,
				OutstandingTone:     outstandingTone(l.OutstandingQuantity),
			})
		}
		out = append(out, purchasedto.PurchaseOrderViewResp{
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
// 但取数口保留：接口侧与将来的页面入口共用同一投影。错误回显同样走 purchaseErrText ——
// 留着 err.Error() 就等于在同一个文件里留了一条尚未被页面用到的泄漏路径。
func (h *purchasePageHandle) purchaseHistoryRows(c *gin.Context, projectID, sku string, pageErr *string) (out []purchasedto.PurchaseHistoryViewResp) {
	ctx := c.Request.Context()
	out = []purchasedto.PurchaseHistoryViewResp{}
	if projectID == "" {
		return out
	}
	rows, err := h.purchase.ListPurchaseHistory(ctx, &purchasedto.ListPurchaseHistoryReq{
		ProjectID: projectID, SKUCode: sku, Size: purchaseHistorySize,
	})
	if err != nil {
		if *pageErr == "" {
			*pageErr = purchaseErrText(c, err)
		}
		return out
	}
	tr := purchaseTranslate(c)
	for _, r := range rows {
		out = append(out, purchasedto.PurchaseHistoryViewResp{
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
func (h *purchasePageHandle) sourceOptions(ctx context.Context, projectID, sourceType string,
	tr func(key, fallback string) string) (out []purchasedto.SourceOptionViewResp) {
	out = []purchasedto.SourceOptionViewResp{}
	if projectID == "" {
		return out
	}
	rows, err := h.purchase.ListSources(ctx, &purchasedto.ListSourceReq{
		ProjectID: projectID, Type: sourceType,
	})
	if err != nil {
		return out
	}
	for _, s := range rows {
		out = append(out, purchasedto.SourceOptionViewResp{
			ID: s.ID, Code: s.Code, Name: s.Name, Type: s.Type,
			Label: sourceOptionLabel(tr, s),
		})
	}
	return out
}

// sourceOptionLabel 货源下拉项的文案：「名称（编码）· 类型」。类型走取词（见 sourceTypeLabel）。
func sourceOptionLabel(tr func(key, fallback string) string, s *purchasedto.SourceResp) string {
	return s.Name + "（" + s.Code + "）· " + sourceTypeLabel(tr, s.Type)
}

// warehouseOptions 收货仓下拉（默认仓标出来；未选即兜底默认仓）。
func (h *purchasePageHandle) warehouseOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []purchasedto.WarehouseOptionViewResp) {
	out = []purchasedto.WarehouseOptionViewResp{}
	if projectID == "" || h.stock == nil {
		return out
	}
	rows, err := h.stock.Warehouses(ctx, projectID)
	if err != nil {
		return out
	}
	for _, w := range rows {
		label := w.Name + "（" + w.Code + "）"
		if w.IsDefault {
			label += " " + tr(purchaseenums.KeyWarehouseDefaultSuffix, "· 默认仓")
		}
		out = append(out, purchasedto.WarehouseOptionViewResp{ID: w.ID, Label: label, IsDefault: w.IsDefault})
	}
	return out
}

// variantOptions 变体下拉（带商品名 / SKU / 当前成本价，选行时一眼可见成本口径）。
//
// 每一项额外带 **BareSKU（仓库侧裸码）**：采购行的 sku_code 是仓库侧快照，必须按裸码落库
// （仓库里的 SKU 永远不带仓码前缀）。裸码取自库存真源（inventory_stocks.sku_code）——
// 它就是「这条货在仓库里叫什么」的权威答案，商品侧的 SKUCode 只是它加了前缀仓前缀的投影。
// 没有库存行的变体退回商品侧编码，由服务端在入库入口按目标仓短码归一。
//
// 查询次数与商品数无关：变体清单一次（商品契约的只读口），裸码映射一次（库存真源）。
func (h *purchasePageHandle) variantOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []purchasedto.VariantOptionViewResp) {
	out = []purchasedto.VariantOptionViewResp{}
	// 候选只读口未注入（装配漏接）时下拉为空，页面照常渲染 —— 不因一处装配缺失 500。
	if projectID == "" || h.catalog == nil {
		return out
	}
	options, err := h.catalog.ListAllVariantOptions(ctx, projectID)
	if err != nil {
		return out
	}
	// 裸码读失败只降级（退回商品侧编码），不把整页打空：下拉仍然可用，
	// 入库入口会按目标仓短码归一。
	bare, berr := h.catalog.BareSKUsByVariant(ctx, projectID)
	if berr != nil {
		bare = map[string]string{}
	}
	// 成本后缀是一句带命名占位符的文案：取词后用 {value} 填充（词条被写坏时 FillTranslate
	// 自动回落下面那句中文兜底，页面上不会出现 {value} 这样的字面量）。
	costSuffix := func(cost string) string {
		return i18n.FillTranslate(tr, purchaseenums.KeyPurchasesOptionCostLabel, "（成本 {value}）",
			map[string]string{"value": cost})
	}
	unsetCost := tr(purchaseenums.KeyPurchasesOptionCostUnset, "未填")
	for _, o := range options {
		cost := unsetCost
		if o.CostPrice != nil {
			cost = strconv.FormatFloat(*o.CostPrice, 'f', 2, 64)
		}
		sku := bare[o.VariantID]
		if sku == "" {
			sku = o.SKUCode
		}
		out = append(out, purchasedto.VariantOptionViewResp{
			VariantID: o.VariantID, ProductID: o.ProductID, SKUCode: o.SKUCode, BareSKU: sku,
			Label: o.ProductName + " · " + o.SKUCode + costSuffix(cost),
		})
	}
	return out
}

// —— 表单与文案工具 ——

// purchaseStatusOptions 状态筛选下拉（五个值：三个推导态 + 两个人工终态）。
func purchaseStatusOptions(tr func(key, fallback string) string) []purchasedto.OptionResp {
	return []purchasedto.OptionResp{
		{Value: purchaseenums.PurchaseStatusPending, Label: purchaseStatusLabel(tr, purchaseenums.PurchaseStatusPending)},
		{Value: purchaseenums.PurchaseStatusPartial, Label: purchaseStatusLabel(tr, purchaseenums.PurchaseStatusPartial)},
		{Value: purchaseenums.PurchaseStatusReceived, Label: purchaseStatusLabel(tr, purchaseenums.PurchaseStatusReceived)},
		{Value: purchaseenums.PurchaseStatusFinished, Label: purchaseStatusLabel(tr, purchaseenums.PurchaseStatusFinished)},
		{Value: purchaseenums.PurchaseStatusCancelled, Label: purchaseStatusLabel(tr, purchaseenums.PurchaseStatusCancelled)},
	}
}

// purchaseStatusLabel 推导状态 → 当前语言展示文案。
func purchaseStatusLabel(tr func(key, fallback string) string, status string) string {
	switch status {
	case purchaseenums.PurchaseStatusPending:
		return tr(purchaseenums.KeyPurchasesStatusPending, "未入库")
	case purchaseenums.PurchaseStatusPartial:
		return tr(purchaseenums.KeyPurchasesStatusPartial, "部分入库")
	case purchaseenums.PurchaseStatusReceived:
		// 已入库：与同页列头同一个词（英文都是 Received），复用它的词条。
		return tr(purchaseenums.KeyPurchasesColReceived, "已入库")
	case purchaseenums.PurchaseStatusFinished:
		return tr(purchaseenums.KeyPurchasesStatusFinished, "已关单")
	case purchaseenums.PurchaseStatusCancelled:
		return tr(purchaseenums.KeyPurchasesStatusCancelled, "已取消")
	default:
		return status
	}
}

// receiptKindLabel 入库单类型 → 当前语言展示文案。
func receiptKindLabel(tr func(key, fallback string) string, kind string) string {
	switch kind {
	case purchaseenums.ReceiptKindPurchase:
		return tr(purchaseenums.KeyPurchasesKindPurchase, "采购收货")
	case purchaseenums.ReceiptKindProduction:
		// 生产入库：与采购页的「生产入库」入口同一个词，复用它的词条。
		return tr(purchaseenums.KeyPurchasesProductionOpen, "生产入库")
	default:
		return kind
	}
}
