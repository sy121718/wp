package orderhttp

import (
	"strings"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
	pageview "go_wp/pkg/view"
)

const (
	pendingShipPath   = "/admin/orders/pending-ship"
	shipFormPath      = "/admin/orders/ship-form"
	orderCarriersPath = "/admin/order-carriers"
)

type orderShipmentPageHandle struct {
	svc      ordercontract.OrderService
	projects projectcontract.ProjectService
}

func NewOrderShipmentPageHandle(svc ordercontract.OrderService, projects projectcontract.ProjectService) *orderShipmentPageHandle {
	return &orderShipmentPageHandle{svc: svc, projects: projects}
}

func (h *orderShipmentPageHandle) PendingShipPage(c *gin.Context) {
	query, ok := binding.Query[orderdto.ShipmentPageQueryReq](c)
	if !ok {
		return
	}
	data, _ := h.pendingShipView(c, query)
	pageview.RenderTemplate(c, PendingShipPageTemplate, data)
}

func pendingShipWindow(query *orderdto.ShipmentPageQueryReq) (page, limit int) {
	page, limit = query.Page, query.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

func (h *orderShipmentPageHandle) pendingShipView(c *gin.Context, query *orderdto.ShipmentPageQueryReq) (*PendingShipPageView, bool) {
	ctx := c.Request.Context()
	page, limit := pendingShipWindow(query)
	projects, loadErr := h.projects.List(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
	}
	selected := strings.TrimSpace(query.ProjectID)
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	rows := []*orderdto.PendingShipRow{}
	total := int64(0)
	pageErr := ""
	if loadFailed {
		pageErr = orderFacingError(c, loadErr)
	} else if selected != "" {
		res, err := h.svc.ListPendingShip(ctx, &orderdto.PendingShipReq{
			ProjectID: selected,
			Keyword:   strings.TrimSpace(query.Keyword),
			Offset:    utils.PageOffset(page, limit),
			Limit:     limit,
		})
		if err != nil {
			loadFailed = true
			pageErr = orderFacingError(c, err)
		} else if res != nil {
			rows, total = res.List, res.Total
			if rows == nil {
				rows = []*orderdto.PendingShipRow{}
			}
		}
	}
	tr := text.TranslateFunc(c)

	return &PendingShipPageView{
		Title:           tr(orderenums.PendingShipTitle, "待发货"),
		Menu:            "orders-pending-ship",
		Projects:        projects,
		SelectedProject: selected,
		Rows:            rows,
		Total:           total,
		FilterKeyword:   strings.TrimSpace(query.Keyword),
		LoadFailed:      loadFailed,
		LoadError:       pageErr,
		Page:            page,
		Limit:           limit,
		Base: reqparams.FilterBaseURL(pendingShipPath,
			map[string]string{"project": selected, "keyword": strings.TrimSpace(query.Keyword)}),
	}, loadFailed
}

func (h *orderShipmentPageHandle) carrierView(c *gin.Context, requested string) *CarrierListPageView {
	ctx := c.Request.Context()
	projects, loadErr := h.projects.List(ctx)
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
	}
	selected := strings.TrimSpace(requested)
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	list := []*orderdto.CarrierResp{}
	pageErr := ""
	if loadFailed {
		pageErr = orderFacingError(c, loadErr)
	} else if selected != "" {

		got, err := h.svc.ListCarriers(ctx, &orderdto.ListCarrierReq{
			ProjectID: selected, IncludeDisabled: true,
		})
		if err != nil {
			loadFailed = true
			pageErr = orderFacingError(c, err)
		} else {
			list = got
		}
	}
	tr := text.TranslateFunc(c)
	return &CarrierListPageView{
		Title:           tr(orderenums.OrderCarriersTitle, "承运商"),
		Menu:            "order-carriers",
		Projects:        projects,
		SelectedProject: selected,
		Carriers:        list,
		LoadFailed:      loadFailed,
		LoadError:       pageErr,
	}
}

func (h *orderShipmentPageHandle) ShipFormFragment(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListShipmentReq](c)
	if !ok {
		return
	}
	form, err := h.svc.BuildShipForm(c.Request.Context(), req)
	if err != nil {

		response.ErrorAuto(c, 400, "order", err)
		return
	}
	tr := text.TranslateFunc(c)
	pageview.RenderTemplate(c, ShipFormPageTemplate, gin.H{
		"Form":      form,
		"t":         tr,
		"CSRFToken": binding.CSRFToken(c),
	})
}

func (h *orderShipmentPageHandle) OrderCarriersPage(c *gin.Context) {
	query, ok := binding.Query[orderdto.ShipmentPageQueryReq](c)
	if !ok {
		return
	}
	pageview.RenderTemplate(c, OrderCarriersPageTemplate, h.carrierView(c, query.ProjectID))
}
