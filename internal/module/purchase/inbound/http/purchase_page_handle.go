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
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	pageview "go_wp/pkg/view"
)

const (
	purchasePageSize = 100

	purchaseMaxPageSize = 200

	purchasesPath = "/admin/inventory/purchases"

	purchaseHistorySize = 50

	purchaseDraftLineCount = 3
)

type purchasePageHandle struct {
	purchase purchasecontract.PurchaseService
	stock    purchasecontract.StockPort
	projects projectcontract.ProjectService

	catalog purchasecontract.VariantCatalog
}

func NewPurchasePageHandle(purchase purchasecontract.PurchaseService, stock purchasecontract.StockPort,
	projects projectcontract.ProjectService) *purchasePageHandle {
	return &purchasePageHandle{purchase: purchase, stock: stock, projects: projects}
}

func (h *purchasePageHandle) setVariantCatalog(catalog purchasecontract.VariantCatalog) {
	h.catalog = catalog
}

func (h *purchasePageHandle) SetCatalog(catalog purchasecontract.VariantCatalog) {
	h.catalog = catalog
}

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

	Err string

	LoadFailed bool

	Pagination pagination.Page

	DraftLines []purchasedto.DraftLineViewResp

	StatusOptions []purchasedto.OptionResp

	ReceiptRequestID    string `view:"ReceiptRequestID"`
	ProductionRequestID string `view:"ProductionRequestID"`

	ListQuery string
}

func draftLines() []purchasedto.DraftLineViewResp {
	out := make([]purchasedto.DraftLineViewResp, 0, purchaseDraftLineCount)
	for i := 0; i < purchaseDraftLineCount; i++ {
		out = append(out, purchasedto.DraftLineViewResp{Index: i})
	}
	return out
}

func (h *purchasePageHandle) purchasesPageView(c *gin.Context, d *purchasesPageData) any {
	if d.Title == "" {
		d.Title = purchaseenums.MsgInventoryPurchasesTitle
		d.Menu = "inventory-purchases"
	}
	d.DraftLines = draftLines()
	d.StatusOptions = purchaseStatusOptions()
	d.ReceiptRequestID = uuid.NewString()
	d.ProductionRequestID = uuid.NewString()
	d.ListQuery = binding.ListQuery(c, purchaseOrderBackKeys...)
	return d
}

func (h *purchasePageHandle) PurchasesPage(c *gin.Context) {
	q, ok := binding.Query[purchasedto.PurchaseListQueryReq](c)
	if !ok {
		return
	}
	pageview.Render(c, h.purchasesPageView(c, h.purchasesPageFacts(c, q)))
}

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

type purchaseOrderFilter struct {
	d         *purchasesPageData
	projectID string
	status    string
	sourceID  string
	keyword   string
	page      int
	limit     int
}

func (h *purchasePageHandle) fillPurchaseOptions(c *gin.Context, d *purchasesPageData, projectID string) {
	ctx, tr := c.Request.Context(), purchaseTranslate(c)
	d.Sources = h.sourceOptions(ctx, projectID, "", tr)

	d.InternalSources = h.sourceOptions(ctx, projectID, purchaseenums.SourceTypeInternal, tr)
	d.Warehouses = h.warehouseOptions(ctx, projectID, tr)
	d.VariantOptions = h.variantOptions(ctx, projectID, tr)
}

func (h *purchasePageHandle) fillPurchaseOrders(c *gin.Context, f *purchaseOrderFilter) {
	var total int64
	f.d.Orders, total, f.page = h.purchaseOrderRows(c, f.projectID, f.status, f.sourceID, f.keyword,
		f.page, f.limit, &f.d.Err)

	f.d.Pagination = pagination.Page{Total: total, Page: f.page, Limit: f.limit}
	f.d.Pagination.Base = reqparams.FilterBaseURL(purchasesPath, map[string]string{
		"project":  f.projectID,
		"status":   f.status,
		"sourceId": f.sourceID,
		"keyword":  f.keyword,
	})
}

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
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchaseUpdate(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseCreateFormReq](c)
	if !ok {
		return
	}

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
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchaseReceipt(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseReceiptFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.RegisterReceipt(c.Request.Context(), form.RegisterReq(builtin.GetUsername(c))); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchaseProduction(c *gin.Context) {
	form, ok := binding.Form[purchasedto.ProductionFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.RegisterProductionInbound(c.Request.Context(), form.ProductionReq(builtin.GetUsername(c))); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchaseFinish(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.FinishPurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchaseCancel(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.CancelPurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchasePlace(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.PlacePurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchaseException(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.MarkPurchaseOrderException(c.Request.Context(), form.StatusReq()); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *purchasePageHandle) PurchaseResume(c *gin.Context) {
	form, ok := binding.Form[purchasedto.PurchaseStatusFormReq](c)
	if !ok {
		return
	}
	if _, err := h.purchase.ResumePurchaseOrder(c.Request.Context(), form.StatusReq()); err != nil {
		purchaseFail(c, err)
		return
	}
	pageview.Success(c)
}

func (h *purchasePageHandle) purchaseOrderRows(c *gin.Context, projectID, status, sourceID, keyword string,
	page, limit int, pageErr *string) (out []purchasedto.PurchaseOrderViewResp, total int64, curPage int) {
	ctx := c.Request.Context()
	out = []purchasedto.PurchaseOrderViewResp{}
	if projectID == "" {
		return out, 0, 1
	}

	filterReq := &purchasedto.ListPurchaseOrderReq{
		ProjectID: projectID, Status: status, SourceID: sourceID, Keyword: keyword,
	}

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
		out = append(out, purchaseOrderRowView(o, tr))
	}
	return out, total, curPage
}

func purchaseOrderRowView(o *purchasedto.PurchaseOrderResp,
	tr func(key, fallback string) string) purchasedto.PurchaseOrderViewResp {
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
	return purchasedto.PurchaseOrderViewResp{
		ID: o.ID, Code: o.Code,
		SourceName: o.SourceName, SourceType: o.SourceType,
		SourceTypeLabel: sourceTypeLabel(tr, o.SourceType),
		WarehouseName:   o.WarehouseName,
		Status:          o.Status, StatusLabel: purchaseStatusLabel(tr, o.Status),
		OrderedAt: o.OrderedAt, ExpectedAt: o.ExpectedAt,
		Remark: o.Remark, OperatorID: o.OperatorID,
		TotalQuantity: o.TotalQuantity, ReceivedQuantity: o.ReceivedQuantity,
		OutstandingQuantity: o.OutstandingQuantity, ProgressPercent: o.ProgressPercent,
		OutstandingTone: outstandingTone(o.OutstandingQuantity),

		ReceivedLabel: tr(purchaseenums.KeyPurchasesColReceived, "已入库") +
			" " + strconv.Itoa(o.ReceivedQuantity) + " / " + strconv.Itoa(o.TotalQuantity),
		OutstandingLabel: tr(purchaseenums.KeyPurchasesColOutstanding, "未入库") +
			" " + strconv.Itoa(o.OutstandingQuantity),
		ProgressLabel: strconv.Itoa(o.ProgressPercent) + "%",
		Lines:         lines,
		RequestID:     uuid.NewString(),
	}
}

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

func sourceOptionLabel(tr func(key, fallback string) string, s *purchasedto.SourceResp) string {
	return s.Name + "（" + s.Code + "）· " + sourceTypeLabel(tr, s.Type)
}

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

func (h *purchasePageHandle) variantOptions(ctx context.Context, projectID string,
	tr func(key, fallback string) string) (out []purchasedto.VariantOptionViewResp) {
	out = []purchasedto.VariantOptionViewResp{}

	if projectID == "" || h.catalog == nil {
		return out
	}
	options, err := h.catalog.ListAllVariantOptions(ctx, projectID)
	if err != nil {
		return out
	}

	bare, berr := h.catalog.BareSKUsByVariant(ctx, projectID)
	if berr != nil {
		bare = map[string]string{}
	}

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

// purchaseStatusOptions 只交值与顺序；标签由模板按请求语言取词。
func purchaseStatusOptions() []purchasedto.OptionResp {
	out := make([]purchasedto.OptionResp, 0, len(purchaseenums.PurchaseStatuses))
	for _, status := range purchaseenums.PurchaseStatuses {
		out = append(out, purchasedto.OptionResp{Value: status})
	}
	return out
}

func purchaseStatusLabel(tr func(key, fallback string) string, status string) string {
	switch status {
	case purchaseenums.PurchaseStatusDraft:
		return tr(purchaseenums.KeyPurchasesStatusDraft, "草稿")
	case purchaseenums.PurchaseStatusOrdered:
		return tr(purchaseenums.KeyPurchasesStatusOrdered, "已下单")
	case purchaseenums.PurchaseStatusPartial:
		return tr(purchaseenums.KeyPurchasesStatusPartial, "部分到货")
	case purchaseenums.PurchaseStatusReceived:

		return tr(purchaseenums.KeyPurchasesStatusReceived, "已到货")
	case purchaseenums.PurchaseStatusException:
		return tr(purchaseenums.KeyPurchasesStatusException, "异常")
	case purchaseenums.PurchaseStatusFinished:
		return tr(purchaseenums.KeyPurchasesStatusFinished, "已完结")
	case purchaseenums.PurchaseStatusCancelled:
		return tr(purchaseenums.KeyPurchasesStatusCancelled, "已取消")
	default:
		return status
	}
}

func receiptKindLabel(tr func(key, fallback string) string, kind string) string {
	switch kind {
	case purchaseenums.ReceiptKindPurchase:
		return tr(purchaseenums.KeyPurchasesKindPurchase, "采购收货")
	case purchaseenums.ReceiptKindProduction:

		return tr(purchaseenums.KeyPurchasesProductionOpen, "生产入库")
	default:
		return kind
	}
}
