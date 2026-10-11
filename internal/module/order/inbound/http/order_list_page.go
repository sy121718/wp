package orderhttp

// order_list_page.go — 订单管理页（列表 / 详情 / 状态流转 / 取消 / 退款 / 批量）的控制器。
//
// 分工与优惠码页一致（样板见 coupon_page.go）：
//
//   - 控制器只做「绑定 → 调 service → 渲染」；文案、格式化、显示判断在模板里。
//   - **dto 直接交给模板**：Orders 是 []*orderdto.OrderResp、Detail 是 *orderdto.OrderDetailResp，
//     模板读 o.OrderNo / detail.Head.Total / it.UnitPrice 这些原始字段。Go 侧因此没有
//     orderListRow / orderDetailView / orderStatusCounters / orderBulkTargets 这一层。
//   - **写动作的结论既不走 URL 也不走提示页**：前端直接调 JSON 接口（task-98 S2），
//     页面侧只剩「按钮 / 表单声明调用事实 + 结果区承载回执」。于是 ?err= / ?ok= / ?done= 那套
//     读侧白名单、orderDoneTexts / orderPageDone / orderBulkActions / orderBulkExtraNotices，
//     以及写侧的 shell.RenderJump + shell.BackPath 机关都整批消失。
//
// 三条与状态有关的口径（模板按它们渲染，服务端仍是唯一裁决者）：
//
//  1. **通用流转的目标只有三个**：pending → paid → shipped → completed。取消与退款各有独立
//     用例（取消要归还库存、退款要记流水号），走通用入口必被拒绝，所以只出现在各自的表单里。
//  2. **可取消 = 未发货**（pending / paid）；**可退款 = 钱与货分开**（paid / shipped / completed，
//     退款不归还库存，退货入库是另一件事）。
//  3. 这三条在模板里是**显示投影**（决定按钮渲不渲染），合法性由服务端裁决 ——
//     选了不允许的边，服务端把原因经提示页回给用户。旧实现把同样的表抄在 Go 里（
//     orderNextStatuses / orderStatusCancellable / orderStatusRefundable），
//     与状态机是两份真源。

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/module/order/contract"
	"go_wp/internal/module/order/dto"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/sysconfig/contract"
	"go_wp/pkg/backpath"
	"go_wp/pkg/pagination"
)

// orderPageHandle 订单管理页处理器。
type orderPageHandle struct {
	orders   ordercontract.OrderService
	projects projectcontract.ProjectService
	// dict 系统字典只读口：只用来把订单快照里的国家/地区代码换成当前语言的显示名。
	// 可选依赖 —— 未注入时详情照常渲染、国家显示代码（见 countryLabelFunc）：
	// 一个展示标签的字典读不到，不该让整页失败。
	dict sysconfigcontract.DictReader
}

// NewOrderPageHandle 构造。
func NewOrderPageHandle(orders ordercontract.OrderService, projects projectcontract.ProjectService,
	dict sysconfigcontract.DictReader) *orderPageHandle {
	return &orderPageHandle{orders: orders, projects: projects, dict: dict}
}

// orderFilter 页面筛选条件（GET 参数，全部可选）。
type orderFilter struct {
	// Status 状态精确筛选（空 = 全部）。
	Status string
	// Keyword 关键词（订单号 / 客户邮箱 / 客户姓名，服务端决定匹配哪些列）。
	Keyword string
	// PaymentMethod 支付方式筛选（如 paypal）。
	PaymentMethod string
	// OrderID 非 0 时在列表下方渲染详情块（同一个页面，靠查询参数切换，不新开路由）。
	OrderID uint64
}

// orderStatusValues 状态维度的取值白名单（含空串 = 「全部」）。
//
// 顺序 = 徽章行里的顺序；文案不在这里（真源是 orderenums.OrderStatusLabel 的
// `site.fragment.order.status.*` 词条，模板按 key 前缀取词）。
var orderStatusValues = []string{
	"", "pending", "paid", "shipped", "completed", "cancelled", "refunded",
}

// OrdersPage 订单管理页（GET /admin/orders）。
func (h *orderPageHandle) OrdersPage(c *gin.Context) {
	ctx := c.Request.Context()

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**：空列表 + 归口提示 + HTTP 200，
	// 页头 / 筛选器 / 批量条 / 分页壳与侧栏全部保留。
	loadErrText := ""
	if loadErr != nil {
		projects = nil
		loadErrText = orderFacingError(c, loadErr)
	}

	// 装载失败时不再去读列表与详情：工程上下文都没定下来（selected 只能来自 URL），
	// 拿一个可能属于别的工程的 project 参数去查订单，查出来的是哪个工程的单都说不清。
	selected := ""
	if loadErr == nil {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	page, limit := orderListWindow(c)
	filter := orderFilter{
		Status:        strings.TrimSpace(c.Query("status")),
		Keyword:       strings.TrimSpace(c.Query("keyword")),
		PaymentMethod: strings.TrimSpace(c.Query("paymentMethod")),
		OrderID:       orderQueryID(c.Query("orderId")),
	}

	var orders []*orderdto.OrderResp
	// 计数表一开始就补齐零值（理由同退货页：模板会拿计数做数值比较，nil 参与比较会让整页 500）。
	counts := countsFilled(nil, orderStatusValues)
	var total int64
	if selected != "" {
		list, lerr := h.orders.ListOrders(ctx, &orderdto.ListOrderReq{
			ProjectID:     selected,
			Status:        filter.Status,
			Keyword:       filter.Keyword,
			PaymentMethod: filter.PaymentMethod,
			Offset:        (page - 1) * limit,
			Limit:         limit,
		})
		if lerr != nil {
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, lerr))
		} else {
			// 计数不受筛选影响（它回答「各状态各有多少单」这个全局问题），用服务端给的整份计数。
			orders, counts, total = list.List, countsFilled(list.Counts, orderStatusValues), list.Total
		}
	}

	var detail *orderdto.OrderDetailResp
	if selected != "" && filter.OrderID > 0 {
		det, derr := h.orders.GetOrder(ctx, &orderdto.GetOrderReq{
			ProjectID: selected, OrderID: filter.OrderID,
		})
		if derr != nil {
			loadErrText = firstNonEmpty(loadErrText, orderFacingError(c, derr))
		} else {
			detail = det
		}
	}

	data := adminui.Prepare(c, gin.H{
		// 标题是 i18n key：adminui.Prepare 按当前语言取词（模板不再看到裸 key）。
		"title":           "admin.orders.heading",
		"menu":            "orders",
		"Projects":        projects,
		"SelectedProject": selected,
		// 取数结果：dto 原样交给模板。
		"Orders": orders,
		"Counts": counts,
		"Total":  total,
		"Detail": detail,
		// 状态徽章行的「取值 + 分档」+ 当前生效值。
		// 分档与取值一起给（orderStatusTabs）：模板不再逐个判状态选 badge 类。
		"StatusTabs": orderStatusTabs(),
		// 「全部」徽章的计数（各状态之和）：模板里跨类型累加不值得押注，求和放这里一行。
		"CountsAll": countsAll(counts),
		// 状态 → 当前语言标签（enums 的 (key, fallback) 是两返回值，模板接不住，
		// 这里收敛成单值函数；**取词仍发生在模板调用点**，Go 不预先拼行视图）。
		"OrderStatusLabel": orderStatusLabelFunc(c),
		"FilterStatus":     filter.Status,
		// 筛选回显与空态判据都由模板自己算（FilterActive 那种布尔不再由 Go 预先决定）。
		"FilterKeyword": filter.Keyword,
		"FilterPayment": filter.PaymentMethod,
		// 这一次没读出来的原因（空串 = 正常）：模板据此把「装载失败」与「还没有订单」分开。
		"LoadErr": loadErrText,
		// 列表上下文（拼进页内链接的 query；不含 status，见 orderListQuery）。
		"ListQuery": orderListQuery(selected, filter, page, limit),
		// 国家/地区代码 → 当前语言的显示名。函数值直接进数据：模板里
		// `{{ .CountryLabel(code) }}` 调用它，详情不必预先翻好每一段地址。
		"CountryLabel": countryLabelFunc(c, h.dict),
		"Page":         page,
		"Limit":        limit,
	})
	base := backpath.WithParams("/admin/orders", map[string]string{
		"project": selected, "status": filter.Status, "keyword": filter.Keyword,
		"paymentMethod": filter.PaymentMethod,
	})
	pg := pagination.Facts(total, page, limit)
	pg.Base = base
	data["Pagination"] = pg
	c.HTML(http.StatusOK, "admin/order/orders.html", data)
}

// orderListQuery 列表上下文 → 查询串（拼进表单 action；空值不拼）。
//
// 刻意**不含 status**：徽章链接要覆盖它、写表单要保留它，两者各自显式拼，
// 混进基础串里会出现「点了别的状态徽章却还带着旧状态」。
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

// countryLabelFunc 国家/地区代码 → 当前语言的显示名（模板直接调用的函数值）。
//
// 字典未注入（装配退化）时返回一个**原样返回代码**的函数，而不是 nil ——
// 模板里 `{{ .CountryLabel(code) }}` 对 nil 函数会运行时报错、整页 500；
// 而「未接入字典」只该表现为「显示代码」（与「接入但查不到」同一条回落路径）。
func countryLabelFunc(c *gin.Context, dict sysconfigcontract.DictReader) func(string) string {
	label := countryLabelFn(c, dict)
	if label == nil {
		return func(code string) string { return strings.TrimSpace(code) }
	}
	return label
}
