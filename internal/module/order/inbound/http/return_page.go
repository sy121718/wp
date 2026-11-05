package orderhttp

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

type returnPageHandle struct {
	orders     ordercontract.OrderService
	projects   projectcontract.ProjectService
	warehouses ordercontract.ReturnWarehouseSource
	dict       sysconfigcontract.DictReader
}

func NewReturnPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	warehouses ordercontract.ReturnWarehouseSource, dict sysconfigcontract.DictReader) *returnPageHandle {
	return &returnPageHandle{orders: orders, projects: projects, warehouses: warehouses, dict: dict}
}

type returnFilter struct {
	Status string

	Keyword string

	OrderID uint64

	ReturnID uint64
}

var returnStatusValues = []string{
	"", "requested", "approved", "received", "completed", "rejected", "cancelled",
}

func statusLabelFacts(values []string, resolve func(string) (string, string)) map[string]StatusLabelFact {
	facts := make(map[string]StatusLabelFact, len(values))
	for _, value := range values {
		key, fallback := resolve(value)
		facts[value] = StatusLabelFact{Key: key, Fallback: fallback}
	}
	return facts
}

var returnListQueryKeys = []string{"project", "keyword", "orderId", "page", "limit", "returnId"}

func (h *returnPageHandle) ReturnsPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)

	loadErrText := ""
	if loadErr != nil {
		projects = nil
		loadErrText = orderFacingError(c, loadErr)
	}

	selected := ""
	if loadErr == nil {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	page, limit := orderListWindow(c)
	filter := returnFilter{
		Status:   strings.TrimSpace(c.Query("status")),
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		OrderID:  orderQueryID(c.Query("orderId")),
		ReturnID: orderQueryID(c.Query("returnId")),
	}

	var returns []*orderdto.ReturnResp

	counts := countsFilled(nil, returnStatusValues)
	var total int64
	if selected != "" {
		list, lerr := h.orders.ListReturns(ctx, &orderdto.ReturnListReq{
			ProjectID: selected,
			Status:    filter.Status,
			Keyword:   filter.Keyword,
			OrderID:   filter.OrderID,
			Offset:    utils.PageOffset(page, limit),
			Limit:     limit,
		})
		if lerr != nil {
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, lerr))
		} else {
			returns, counts, total = list.List, countsFilled(list.Counts, returnStatusValues), list.Total
		}
	}

	var detail *orderdto.ReturnDetailResp
	if selected != "" && filter.ReturnID > 0 {
		det, derr := h.orders.GetReturn(ctx, filter.ReturnID)
		if derr != nil {
			loadErrText = firstNonEmpty(loadErrText, returnFacingError(c, derr))
		} else {
			detail = det
		}
	}

	view := &ReturnListPageResp{
		Title:           orderenums.ReturnsHeading,
		Menu:            "orders",
		Projects:        projects,
		SelectedProject: selected,

		Returns: returns,
		Counts:  counts,
		Total:   total,

		CountsAll: countsAll(counts),
		Detail:    detail,

		StatusTabs:         returnStatusTabs(),
		FilterStatus:       filter.Status,
		ReturnStatusLabels: returnStatusPageFacts(returnStatusValues, filter.Status, returns, detail),
		OrderStatusLabels:  orderStatusPageFacts(orderStatusValues, detail),
		FilterKeyword:      filter.Keyword,
		FilterOrderID:      filter.OrderID,

		LoadErr: loadErrText,

		ListQuery: returnListQuery(selected, filter, page, limit),

		Warehouses: toReturnWarehouses(h.enabledWarehouses(ctx, selected)),

		CountryLabel: countryLabelFunc(c, h.dict),
		Page:         page,
		Limit:        limit,
	}

	view.Pagination = pagination.Page{
		Total: total, Page: page, Limit: limit,
		Base: backpath.WithParams("/admin/returns", map[string]string{
			"project": selected, "status": filter.Status, "keyword": filter.Keyword,
		}),
	}
	pageview.RenderTemplate(c, ReturnListPageTemplate, view)
}

func returnListQuery(projectID string, filter returnFilter, page, limit int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("keyword", filter.Keyword)
	if filter.OrderID > 0 {
		set("orderId", strconv.FormatUint(filter.OrderID, 10))
	}
	if page > 0 {
		set("page", strconv.Itoa(page))
	}
	if limit > 0 {
		set("limit", strconv.Itoa(limit))
	}
	return q.Encode()
}

func returnStatusText(tr translate, status string) string {
	key, fallback := orderenums.ReturnStatusLabel(status)
	return orderLabelOf(tr, orderLabel{key, fallback})
}

func returnStatusPageFacts(values []string, filterStatus string, returns []*orderdto.ReturnResp, detail *orderdto.ReturnDetailResp) map[string]StatusLabelFact {
	statuses := append([]string(nil), values...)
	statuses = append(statuses, filterStatus)
	for _, item := range returns {
		if item != nil {
			statuses = append(statuses, item.Status)
		}
	}
	if detail != nil {
		if detail.Return != nil {
			statuses = append(statuses, detail.Return.Status)
		}
	}
	return statusLabelFacts(statuses, orderenums.ReturnStatusLabel)
}

func orderStatusPageFacts(values []string, detail *orderdto.ReturnDetailResp) map[string]StatusLabelFact {
	statuses := append([]string(nil), values...)
	if detail != nil && detail.Order != nil {
		statuses = append(statuses, detail.Order.Status)
	}
	return statusLabelFacts(statuses, orderenums.OrderStatusLabel)
}

func countsAll(counts map[string]int64) int64 {
	var all int64
	for _, v := range counts {
		all += v
	}
	return all
}

func orderStatusLabelFunc(c *gin.Context) func(string) string {
	tr := orderTr(c)
	return func(status string) string {
		key, fallback := orderenums.OrderStatusLabel(status)
		return tr(key, fallback)
	}
}

func (h *returnPageHandle) enabledWarehouses(ctx context.Context, projectID string) []ordercontract.ReturnWarehouse {
	if h.warehouses == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	list, err := h.warehouses.ListReturnWarehouses(ctx, projectID)
	if err != nil {

		return nil
	}
	out := make([]ordercontract.ReturnWarehouse, 0, len(list))
	for _, w := range list {
		if strings.TrimSpace(w.ID) == "" {
			continue
		}
		if w.Status != "" && w.Status != "enabled" {
			continue
		}
		out = append(out, w)
	}
	return out
}

var returnFacingExtras = []string{
	orderenums.MsgReturnApproved, orderenums.MsgReturnRejected, orderenums.MsgReturnReceived,
	orderenums.ErrReturnRejectReasonRequired, orderenums.ErrReturnNotReviewable, orderenums.ErrReturnNotReceivable,
	orderenums.ErrReturnNotFound,
}

func returnFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if key := returnFacingText(err.Error()); key != "" {
		return response.MsgText(c, facing.Msg{Key: key, Fallback: key})
	}
	logger.Scene("order-return").With("path", c.Request.URL.Path).Error(err, "退货页读取失败")
	return response.InternalText(c)
}

func returnFacingText(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if hit := orderFacingText(msg); hit != "" {
		return hit
	}
	for _, allowed := range returnFacingExtras {
		if msg == allowed {
			return msg
		}
	}
	return ""
}

func toReturnWarehouses(in []ordercontract.ReturnWarehouse) []ReturnWarehouseResp {
	out := make([]ReturnWarehouseResp, 0, len(in))
	for _, w := range in {
		out = append(out, ReturnWarehouseResp{ID: w.ID, Name: w.Name})
	}
	return out
}
