package orderhttp

import (
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
	"go_wp/pkg/pagination"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

type orderPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService

	dict sysconfigcontract.DictReader
}

func NewOrderPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	dict sysconfigcontract.DictReader) *orderPageHandle {
	return &orderPageHandle{orders: orders, projects: projects, dict: dict}
}

type orderFilter struct {
	Status string

	Keyword string

	PaymentMethod string

	CreatedVia string

	Fulfillment string

	OrderID uint64
}

func orderFilterOf(c *gin.Context) orderFilter {
	return orderFilter{
		Status:        strings.TrimSpace(c.Query("status")),
		Keyword:       strings.TrimSpace(c.Query("keyword")),
		PaymentMethod: strings.TrimSpace(c.Query("paymentMethod")),
		CreatedVia:    strings.TrimSpace(c.Query("createdVia")),
		Fulfillment:   strings.TrimSpace(c.Query("fulfillment")),
		OrderID:       orderQueryID(c.Query("orderId")),
	}
}

var orderStatusValues = []string{
	"", "pending", "paid", "shipped", "completed", "cancelled", "refunded",
}

func (h *orderPageHandle) OrdersPage(c *gin.Context) {
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
	filter := orderFilterOf(c)
	data := h.ordersPageData(c, selected, filter, page, limit, loadErrText)
	orders := data.Orders

	view := &OrderListPageResp{

		Title:           orderenums.OrdersHeading,
		Menu:            "orders",
		Projects:        projects,
		SelectedProject: selected,

		Orders: orders,
		Counts: data.Counts,
		Total:  data.Total,
		Detail: data.Detail,

		StatusTabs: orderStatusTabs(),

		CountsAll: countsAll(data.Counts),

		OrderStatusLabel: orderStatusLabelFunc(c),
		FilterStatus:     filter.Status,

		FulfillmentTabs:   fulfillmentTabs(),
		FulfillmentLabels: fulfillmentPageFacts(filter.Fulfillment, orders),
		FilterFulfillment: filter.Fulfillment,
		CreatedViaValues:  createdViaValues,
		FilterCreatedVia:  filter.CreatedVia,
		CreatedViaLabels:  statusLabelFacts(createdViaValues, orderenums.CreatedViaLabel),

		FilterKeyword:     filter.Keyword,
		FilterPayment:     filter.PaymentMethod,
		FulfillmentCounts: data.FulfillmentCounts,

		LoadErr: data.LoadErr,

		ListQuery: orderListQuery(selected, filter, page, limit),

		CountryLabel: countryLabelFunc(c, h.dict),
		Page:         page,
		Limit:        limit,
	}

	view.Pagination = pagination.Page{
		Total: data.Total, Page: page, Limit: limit,
		Base: backpath.WithParams("/admin/orders", map[string]string{
			"project": selected, "status": filter.Status, "keyword": filter.Keyword,
			"paymentMethod": filter.PaymentMethod, "createdVia": filter.CreatedVia,
			"fulfillment": filter.Fulfillment,
		}),
	}
	pageview.RenderTemplate(c, OrderListPageTemplate, view)
}

func (h *orderPageHandle) ordersPageData(c *gin.Context, selected string, filter orderFilter,
	page, limit int, loadErrText string) *OrderListPageResp {
	ctx := c.Request.Context()

	out := &OrderListPageResp{
		LoadErr:           loadErrText,
		Counts:            countsFilled(nil, orderStatusValues),
		FulfillmentCounts: countsFilled(nil, orderenums.FulfillmentStatuses),
	}
	if selected == "" {
		return out
	}
	list, lerr := h.orders.ListOrders(ctx, &orderdto.ListOrderReq{
		ProjectID:         selected,
		Status:            filter.Status,
		Keyword:           filter.Keyword,
		PaymentMethod:     filter.PaymentMethod,
		CreatedVia:        filter.CreatedVia,
		FulfillmentStatus: filter.Fulfillment,
		Offset:            utils.PageOffset(page, limit),
		Limit:             limit,
	})
	if lerr != nil {
		out.LoadErr = firstNonEmpty(out.LoadErr, orderFacingError(c, lerr))
		return out
	}

	out.Orders, out.Total = list.List, list.Total
	out.Counts = countsFilled(list.Counts, orderStatusValues)
	out.FulfillmentCounts = countsFilled(list.FulfillmentCounts, orderenums.FulfillmentStatuses)
	if filter.OrderID == 0 {
		return out
	}
	det, derr := h.orders.GetOrder(ctx, &orderdto.GetOrderReq{ProjectID: selected, OrderID: filter.OrderID})
	if derr != nil {
		out.LoadErr = firstNonEmpty(out.LoadErr, orderFacingError(c, derr))
		return out
	}
	out.Detail = det
	return out
}

func orderListQuery(projectID string, filter orderFilter, page, limit int) string {
	q := url.Values{}
	set := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	set("project", projectID)
	set("keyword", filter.Keyword)
	set("paymentMethod", filter.PaymentMethod)
	set("createdVia", filter.CreatedVia)
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

func countryLabelFunc(c *gin.Context, dict sysconfigcontract.DictReader) func(string) string {
	label := countryLabelFn(c, dict)
	if label == nil {
		return func(code string) string { return strings.TrimSpace(code) }
	}
	return label
}
