// 待发货页 + 发货表单片段 + 承运商配置页（迁移 627 / 625，S3）。
//
// 三屏都是「只读渲染 + 前端直调 JSON」，页面没有自己的写路径：
//   - 待发货页：GET /admin/orders/pending-ship（列表 + 「发货」入口）
//   - 发货表单：GET /admin/orders/ship-form?orderId=（抽屉片段，表单提交走
//     data-write-api → /api/order/shipment/create）
//   - 承运商页：GET /admin/order-carriers（列表 + 新建/编辑抽屉，提交走
//     /api/order/carrier/{create,update}）
//
// 为什么写动作不另开页面端点：sys_permission 每个权限点只有一列 api_path，页面端点
// 再挂一遍就是把同一份授权写第二处；而 JSON 端点本来就是给前端调的。
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
	pageview "go_wp/pkg/view"
)

// 三屏的地址常量（回跳与分页基址的唯一字面量处）。
const (
	pendingShipPath   = "/admin/orders/pending-ship"
	shipFormPath      = "/admin/orders/ship-form"
	orderCarriersPath = "/admin/order-carriers"
)

// orderShipmentPageHandle 待发货 / 发货表单 / 承运商三屏的页面处理器。
type orderShipmentPageHandle struct {
	svc      ordercontract.OrderService
	projects projectcontract.ProjectService
}

// NewOrderShipmentPageHandle 构造。
func NewOrderShipmentPageHandle(svc ordercontract.OrderService, projects projectcontract.ProjectService) *orderShipmentPageHandle {
	return &orderShipmentPageHandle{svc: svc, projects: projects}
}

// PendingShipPage 待发货列表（GET /admin/orders/pending-ship）。
//
// 一行 = 一张「paid 且还有未发数量」的订单；每个数字（件数 / 已发 / 剩余）都由 service
// 算好（模板不做减法，template_logic_test.go），连「这一单现在能不能发」也是算好的事实
// （B6-a 的守卫：不可发时按钮 disabled + 一行说明，对齐 dy_erp 仓库发货台）。
//
// 装载失败**不拿走整页**：空列表 + 归口提示 + 页壳（与订单页其它出口同一形状）——
// 页面 GET 失败时用户要能改地址栏 / 刷新，而不是看到一个 JSON 文档。
func (h *orderShipmentPageHandle) PendingShipPage(c *gin.Context) {
	query, ok := binding.Query[orderdto.ShipmentPageQueryReq](c)
	if !ok {
		return
	}
	data, _ := h.pendingShipView(c, query)
	pageview.Render(c, data)
}

// pendingShipWindow 分页窗口（与其它列表页同一口径：默认 20、上限 100）。
//
// 不用 orderListWindow：那个读的是 c.Query（本文件不在 pendingControllerQuery
// 台账里，也没有理由进台账）—— 分页参数已经在绑定过的 query 结构里了。
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

// pendingShipView 取齐待发货页的数据并组成视图。
//
// 装载失败**不拿走整页**：空列表 + 归口提示 + 页壳（与订单页其它出口同一形状）——
// 页面 GET 失败时用户要能改地址栏 / 刷新，而不是看到一个 JSON 文档。
func (h *orderShipmentPageHandle) pendingShipView(c *gin.Context, query *orderdto.ShipmentPageQueryReq) (*orderdto.PendingShipPageView, bool) {
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
			Offset:    (page - 1) * limit,
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
	// 分页基址在这里算全（与 reqparams.FilterBaseURL 同一实现）：视图实现 view.PageList，
	// 渲染出口据此自动挂分页条 —— 控制器不碰 PaginationKey 字面量，也不 import adminui。
	return &orderdto.PendingShipPageView{
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

// carrierView 取齐承运商页的数据并组成视图（装载失败与待发货页同一形状）。
func (h *orderShipmentPageHandle) carrierView(c *gin.Context, requested string) *orderdto.CarrierListPageView {
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
		// 配置页要看到停用的（否则「停用后不见了」会让人以为被删了）。
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
	return &orderdto.CarrierListPageView{
		Title:           tr(orderenums.OrderCarriersTitle, "承运商"),
		Menu:            "order-carriers",
		Projects:        projects,
		SelectedProject: selected,
		Carriers:        list,
		LoadFailed:      loadFailed,
		LoadError:       pageErr,
	}
}

// ShipFormFragment 发货表单（GET /admin/orders/ship-form?orderId=，抽屉片段）。
//
// 片段里带齐「每行的 订单数量 / 已发 / 剩余 / 默认仓」与承运商下拉 ——
// 「本次」输入框的 max 用 Remaining（service 算好的），前端不再算一遍。
func (h *orderShipmentPageHandle) ShipFormFragment(c *gin.Context) {
	req, ok := binding.Query[orderdto.ListShipmentReq](c)
	if !ok {
		return
	}
	form, err := h.svc.BuildShipForm(c.Request.Context(), req)
	if err != nil {
		// 片段失败给 JSON（抽屉里没有「整页」可渲染）：前端弹 toast，抽屉保留。
		response.ErrorAuto(c, 400, "order", err)
		return
	}
	tr := text.TranslateFunc(c)
	pageview.Render(c, gin.H{
		"Form":      form,
		"t":         tr,
		"CSRFToken": binding.CSRFToken(c),
	})
}

// OrderCarriersPage 承运商配置页（GET /admin/order-carriers）。
func (h *orderShipmentPageHandle) OrderCarriersPage(c *gin.Context) {
	query, ok := binding.Query[orderdto.ShipmentPageQueryReq](c)
	if !ok {
		return
	}
	pageview.Render(c, h.carrierView(c, query.ProjectID))
}
