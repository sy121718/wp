package orderhttp

// sales_page.go — 销售概览页（GET /admin/orders/overview）的控制器。
//
// 整页只读：**没有任何写动作**（无表单 POST、无 csrf 隐藏域），所以也不需要提示页与回跳。
//
// 分工（与订单页、退货页、优惠码页一致）：控制器只做「绑定 → 调 service → 渲染」，
// dto 直接交给模板；**卡片的拼装与趋势图的几何计算都在模板里**（Jet 支持算术、
// 三目、循环内累加与字符串拼接，已由 internal/templates 的方言测试钉住）。
// Go 侧因此没有 salesCardViews / salesTrendView / salesCompareView / salesChangeText 这一层。
//
// 一条与「模板算术」有关的取舍：**图上的坐标由模板算**（x/y 是纯几何，不是业务口径），
// 而**业务数字一律来自 service**（销售额、AOV、环比基期）。两者的界线是
// 「这个数算错了会不会静默误导人」—— 几何错了看得见（线歪了），口径错了看不出来。
//
// 默认区间是**本月 1 号到今天**（UTC 日界，与订单模块所有按天口径一致）。
// 不给「最近 30 天」：报表按自然月对齐才好与上个月的环比对照，
// 而「最近 30 天」的环比基期是一段跨两个月边界的区间，读的人对不上日历。

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

// orderSalesPageHandle 销售概览页的 handler。
type orderSalesPageHandle struct {
	orders   ordercontract.OrderSalesOverviewReader
	projects projectcontract.ProjectService
	// shipments 待发货列表（迁移 624，S2/S3）：总览页的待办块要「还要发几张单」。
	shipments orderShipmentCounter
	// stockAlerts 库存预警（迁移 621 / 623，W4）：总览页的「共 N 项待办」块要它。
	//
	// 用**函数**而不是接口：库存的预警形状（dto.StockAlertResp）在 inventory/dto 里，
	// 订单侧 import 它就是 module_boundary_test.go 判的「绕过契约层」（order→inventory/dto）。
	// 装配层把「调库存的 StockAlert 并取 Alert 数」包成一个函数传进来 ——
	// 订单侧只认「这个工程有多少项库存预警」这一个数字。
	//
	// 允许为 nil：未注入时那一行不渲染（缺一个待办数字不该让整页打不开）。
	stockAlerts func(ctx context.Context, projectID string) (int, error)
}

// orderShipmentCounter 总览页需要的发货能力（一条只读）：待发货单数。
type orderShipmentCounter interface {
	ListPendingShip(ctx context.Context, projectID string) ([]*orderdto.PendingShipRow, error)
}

// NewOrderSalesPageHandle 构造。svc 传全量 OrderService 即可 ——
// 它天然嵌入 OrderSalesOverviewReader，本页只用到其中一条只读方法。
func NewOrderSalesPageHandle(svc ordercontract.OrderService, projects projectcontract.ProjectService) *orderSalesPageHandle {
	return &orderSalesPageHandle{orders: svc, projects: projects}
}

// SetStockAlerts 注入库存预警计数（装配期调用；迁移 621 / 623，W4）。
//
// fn 返回「这个工程有多少项库存预警」（OutOfStock + LowStock，由库存侧算好）。
// 订单侧不 import 库存的 dto：形状由装配层的闭包承载。
func (h *orderSalesPageHandle) SetStockAlerts(fn func(ctx context.Context, projectID string) (int, error)) {
	h.stockAlerts = fn
}

// SetShipmentCounter 注入待发货计数（装配期调用；迁移 624，W4 的待办块）。
func (h *orderSalesPageHandle) SetShipmentCounter(counter orderShipmentCounter) {
	h.shipments = counter
}

// salesStatusQuickValues 状态快捷项的取值（含空串 = 「全部计入消费的状态」）。
//
// 顺序 = 快捷项顺序；文案由模板取词：空值那一项走自己的词条（「全部」不是订单状态），
// 其余走 orderenums.OrderStatusLabel 的真源（与订单列表页同一处）。
var salesStatusQuickValues = []string{"", "paid", "shipped", "completed"}

// SalesOverviewPage 销售概览页（GET /admin/orders/overview）。
func (h *orderSalesPageHandle) SalesOverviewPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**：空列表 + 归口提示 + HTTP 200，页头与筛选壳全部保留。
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

	// 默认区间：本月 1 号 → 今天（UTC）。两个方向的默认值必须来自同一处，
	// 否则页面标题写的区间与实际查询的区间会不一致。
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
			// 取数失败时**不渲染半张报表**：一半真一半 0 的页面比空态危险得多
			//（读的人会以为「这段时间只卖了一点点」）。
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, serr))
		} else {
			overview = res
			// service 归一化后的生效值才是页面上该显示的区间（请求可能给了未来日期）。
			fromRaw, toRaw, status, monthly = res.From, res.To, res.Status, res.Monthly
		}
	}

	view := &orderdto.OrderSalesPageResp{
		Title:           orderenums.OrderSalesTitle,
		Menu:            "orders",
		Projects:        projects,
		SelectedProject: selected,
		// 取数结果：dto 原样交给模板（卡片与趋势图都由模板从它算出来）。
		Overview: overview,
		// 生效区间与筛选回显（service 归一化后的值）。
		FilterFrom:   fromRaw,
		FilterTo:     toRaw,
		FilterStatus: status,
		Monthly:      monthly,
		// 状态快捷项的取值 + 取词函数（空值项走「全部」自己的词条）。
		StatusQuickValues: salesStatusQuickValues,
		OrderStatusLabel:  orderStatusLabelFunc(c),
		// 这一次没读出来的原因（空串 = 正常）：模板据此把「装载失败」与「这个区间没有销售」分开。
		LoadErr: loadErrText,
		// 快捷项链接要保留当前区间与工程：点一下状态就把区间丢回默认，
		// 是本页最容易让人困惑的交互（「我明明选的是上个月」）。
		ListQuery: salesQuery(selected, fromRaw, toRaw, monthly),
		// 待办块（迁移 621 / 623 / 624，W4）：三个键一次给全 ——
		// 取不到的那一块是 -1，模板据此不渲染那一行；TodoTotal 的求和是
		// **展示口径**（一个总数），不是业务计算。
		Todo: h.buildTodoView(c.Request.Context(), selected),
	}
	pageview.Render(c, view)
}

// todoView 待办块的三个数字（装配三个 map 键，handler 侧只留一条语句）。
// 待办块的三个数字直接用 DTO 的导出类型：模板读 .Todo.alertCount（小写字段名），
// 而 dto 层不得 import 本包，所以结构定义留在这里、字段名与模板逐字对齐。
type todoView = orderdto.SalesTodoResp

// buildTodoView 取齐待办块并算好合计（迁移 621 / 623 / 624，W4）。
func (h *orderSalesPageHandle) buildTodoView(ctx context.Context, projectID string) todoView {
	alertCount, pendingShip := h.todoCounts(ctx, projectID)
	return orderdto.SalesTodoResp{
		AlertCount: alertCount, PendingShip: pendingShip,
		Total: todoTotal(alertCount, pendingShip),
	}
}

// todoCounts 待办块的三个数字（迁移 621 / 623 / 624，W4）。
//
// 取不到的项返回 -1（模板据此不渲染那一行）：缺一个数字不该让整页打不开 ——
// 库存模块没装配、或那一次查询失败，另外两个数字仍然是有效信息。
func (h *orderSalesPageHandle) todoCounts(ctx context.Context, projectID string) (
	alertCount int, pendingShip int) {
	alertCount, pendingShip = -1, -1
	// 库存预警（装配层注入的闭包：订单侧只认「有多少项」这个数字）。
	if h.stockAlerts != nil && projectID != "" {
		if n, err := h.stockAlerts(ctx, projectID); err == nil {
			alertCount = n
		}
	}
	// 待发货：走订单契约的同一个读模型（ListPendingShip），不另写一份聚合。
	if projectID != "" && h.shipments != nil {
		if rows, err := h.shipments.ListPendingShip(ctx, projectID); err == nil {
			pendingShip = len(rows)
		}
	}
	return alertCount, pendingShip
}

// todoTotal 「共 N 项待办」的总数（只累加取到的项；负数表示那一项没取到）。
//
// 求和是**展示口径**：三个数字各自来自不同的 service，而「一共几件事要处理」
// 是运营读这一屏的第一个问题 —— 让模板把三个数字相加会把它变成视图侧的业务计算。
func todoTotal(alertCount, pendingShip int) int {
	total := 0
	if alertCount > 0 {
		total += alertCount
	}
	if pendingShip > 0 {
		total += pendingShip
	}
	// 调拨待出货：用 service 的列表计数（status=pending），不重复实现过滤条件。
	return total
}

// salesQuery 快捷项链接的上下文（工程 + 区间 + 回看月数；**不含 status**，由每个链接自己覆盖）。
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

// salesQueryInt 解析查询参数里的整数（空 / 非法 → 0，由 service 回落默认值）。
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
