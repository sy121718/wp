package orderhttp

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"
	pageview "go_wp/pkg/view"
)

type orderSalesPageHandle struct {
	orders   ordercontract.OrderSalesOverviewReader
	projects projectcontract.ProjectService

	shipments orderShipmentCounter

	stockAlerts func(ctx context.Context, projectID string) (int, error)
}

type orderShipmentCounter interface {
	ListPendingShip(ctx context.Context, req *orderdto.PendingShipReq) (*orderdto.PendingShipPageResp, error)
}

func NewOrderSalesPageHandle(svc ordercontract.OrderService, projects projectcontract.ProjectService) *orderSalesPageHandle {
	return &orderSalesPageHandle{orders: svc, projects: projects}
}

func (h *orderSalesPageHandle) SetStockAlerts(fn func(ctx context.Context, projectID string) (int, error)) {
	h.stockAlerts = fn
}

func (h *orderSalesPageHandle) SetShipmentCounter(counter orderShipmentCounter) {
	h.shipments = counter
}

var salesStatusQuickValues = []string{"", "paid", "shipped", "completed"}

func (h *orderSalesPageHandle) SalesOverviewPage(c *gin.Context) {
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

	fromRaw := strings.TrimSpace(c.Query("from"))
	toRaw := strings.TrimSpace(c.Query("to"))
	if fromRaw == "" || toRaw == "" {
		today := time.Now().UTC()
		if toRaw == "" {
			toRaw = today.Format("2006-01-02")
		}
		if fromRaw == "" {
			fromRaw = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		}
	}
	status := strings.TrimSpace(c.Query("status"))
	monthly := salesQueryInt(c.Query("monthly"))

	var overview *orderdto.SalesOverviewResp
	if selected != "" {
		res, serr := h.orders.SalesOverview(ctx, &orderdto.SalesOverviewReq{
			ProjectID: selected,
			From:      fromRaw,
			To:        toRaw,
			Status:    status,
			Monthly:   monthly,
		})
		if serr != nil {

			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, serr))
		} else {
			overview = res

			fromRaw, toRaw, status, monthly = res.From, res.To, res.Status, res.Monthly
		}
	}

	view := &OrderSalesPageResp{
		Title:           orderenums.OrderSalesTitle,
		Menu:            "orders",
		Projects:        projects,
		SelectedProject: selected,

		Overview: overview,

		FilterFrom:   fromRaw,
		FilterTo:     toRaw,
		FilterStatus: status,
		Monthly:      monthly,

		StatusQuickValues: salesStatusQuickValues,
		OrderStatusLabel:  orderStatusLabelFunc(c),

		LoadErr: loadErrText,

		ListQuery: salesQuery(selected, fromRaw, toRaw, monthly),

		Todo: h.buildTodoView(c.Request.Context(), selected),
	}
	pageview.RenderTemplate(c, OrderSalesPageTemplate, view)
}

type todoView = SalesTodoResp

func (h *orderSalesPageHandle) buildTodoView(ctx context.Context, projectID string) todoView {
	alertCount, pendingShip := h.todoCounts(ctx, projectID)
	return SalesTodoResp{
		AlertCount: alertCount, PendingShip: pendingShip,
		Total: todoTotal(alertCount, pendingShip),
	}
}

func (h *orderSalesPageHandle) todoCounts(ctx context.Context, projectID string) (
	alertCount int, pendingShip int) {
	alertCount, pendingShip = -1, -1

	if h.stockAlerts != nil && projectID != "" {
		if n, err := h.stockAlerts(ctx, projectID); err == nil {
			alertCount = n
		}
	}

	if projectID != "" && h.shipments != nil {

		if page, err := h.shipments.ListPendingShip(ctx, &orderdto.PendingShipReq{ProjectID: projectID, Limit: 1}); err == nil && page != nil {
			pendingShip = int(page.Total)
		}
	}
	return alertCount, pendingShip
}

func todoTotal(alertCount, pendingShip int) int {
	total := 0
	if alertCount > 0 {
		total += alertCount
	}
	if pendingShip > 0 {
		total += pendingShip
	}

	return total
}

func salesQuery(projectID, from, to string, monthly int) string {
	q := url.Values{}
	if strings.TrimSpace(projectID) != "" {
		q.Set("project", projectID)
	}
	if strings.TrimSpace(from) != "" {
		q.Set("from", from)
	}
	if strings.TrimSpace(to) != "" {
		q.Set("to", to)
	}
	if monthly > 0 {
		q.Set("monthly", strconv.Itoa(monthly))
	}
	return q.Encode()
}

func salesQueryInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
