package orderhttp

// order_sales_page_handle.go — 后台「销售概览」页（GET /admin/orders/overview，BIZ-1 销售侧）。
//
// 一个整页报表：筛选条（工程 / 起止日期 / 订单状态）+ 两排统计卡 + 环比 + 月度趋势柱图。
// **无 JS 也能用** —— 筛选是普通表单 GET，图表是服务端渲染的 SVG。
//
// 三条与订单模块的约定：
//
//  1. 跨模块只依赖 ordercontract 与不可变的 orderdto，**不 import 订单模块的 model 包**：
//     口径（哪些状态算消费、谁是回头客）全在订单模块内，本页只把结论摆出来。
//
//  2. 金额一律整数分，换算只在 orderMoneyLabel / orderAmountText 里发生一次，
//     模板不做任何算术（趋势柱高是唯一的例外，但它在视图层算好，见 order_sales_page_view.go）。
//
//  3. 默认区间是**本月 1 号到今天**（UTC 日界，与订单模块所有按天口径一致）。
//     不给「最近 30 天」：报表按自然月对齐才好与上个月的环比对照，
//     而「最近 30 天」的环比基期是一段跨两个月边界的区间，读的人对不上日历。

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"

	"go_wp/internal/web/shell"
)

// salesStatusQuick 筛选条上给出的状态快捷项（值 + 文案来源）。
//
// 与白名单同源：这里的每一项都必须在 model 的销售白名单里，否则点了会回落全量
// （服务端刻意**不报错**，于是「点了没反应」是唯一的症状）。空值那一项表示「全部计入消费的状态」。
//
// **非空状态不在这里写 i18n key 字面量**，而是交给 orderStatusLabel → orderenums.OrderStatusLabel
// 这个真源（与订单列表页同一处）。两条理由：① 状态词条将来改名只需改 enums 一处；
// ② 门禁 check-i18n-keys-seeded.sh 只认「Go 文件里字面出现的 key 必须有迁移 INSERT」，
// 而 `admin.orders.status.*` 这类 key 从来是模板 fallback + enums 拼接出来的、没有 seed ——
// 在这里手写字面量会让门禁变红，而「为了过门禁补一份 seed」等于造了第二个真源。
var salesStatusQuick = []struct {
	Value string
	// Key / Fallback 只用于空值那一项（「全部」不是订单状态，enums 里没有它）。
	Key      string
	Fallback string
}{
	{"", "admin.order.sales.status.all", "全部"},
	{"paid", "", ""},
	{"shipped", "", ""},
	{"completed", "", ""},
}

// orderSalesPageHandle 销售概览页的 handler。
type orderSalesPageHandle struct {
	orders   ordercontract.OrderSalesOverviewReader
	projects projectcontract.ProjectService
}

// NewOrderSalesPageHandle 构造。svc 传全量 OrderService 即可 ——
// 它天然嵌入 OrderSalesOverviewReader，本轮只用到其中一条只读方法。
func NewOrderSalesPageHandle(svc ordercontract.OrderService, projects projectcontract.ProjectService) *orderSalesPageHandle {
	return &orderSalesPageHandle{orders: svc, projects: projects}
}

// SalesOverviewPage 销售概览页（GET /admin/orders/overview）。
func (h *orderSalesPageHandle) SalesOverviewPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)

	pageErr := shell.FacingQueryText(c.Query("err"), shell.PageInternalText(c), orderPageFacingText(c))

	projects, loadErr := h.projects.List(ctx)
	// 工程列表读不出来**不拿走整个页面**（判据见 order_page_handle.go 的 OrdersPage）：
	// 空列表 + 归口提示 + HTTP 200，页头与筛选壳全部保留 —— 运营看得出「是这一页没读出来」，
	// 而不是对着一块纯文本以为整个后台坏了。
	loadFailed := loadErr != nil
	if loadFailed {
		projects = nil
		pageErr = orderFacingError(c, loadErr)
	}

	selected := ""
	if !loadFailed {
		selected = strings.TrimSpace(c.Query("project"))
		if selected == "" && len(projects) > 0 {
			selected = projects[0].ID
		}
	}

	// 默认区间：本月 1 号 → 今天（UTC）。回显值由 service 归一化后给出，
	// 这里只负责在「用户没传」时填默认 —— 两个方向的默认值必须来自同一处，
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
	filter := salesFilterView{
		Project: selected,
		From:    fromRaw,
		To:      toRaw,
		Status:  strings.TrimSpace(c.Query("status")),
		Monthly: salesQueryInt(c.Query("monthly")),
	}

	data := shell.Prepare(c, gin.H{
		"title": tr("admin.order.sales.title", "销售概览"),
		"menu":  "orders",
		"Err":   pageErr,
		// 装载失败：空态必须与「这个区间确实没有销售」区分开（判据见 OrdersPage 的 LoadFailed）。
		"LoadFailed": loadFailed,
		// 状态快捷项（含当前选中态由模板判断）。
		"StatusQuick": salesStatusQuickView(tr, filter.Status),
	})

	if selected != "" {
		res, serr := h.orders.SalesOverview(ctx, &orderdto.SalesOverviewReq{
			ProjectID: selected,
			From:      fromRaw,
			To:        toRaw,
			Status:    filter.Status,
			Monthly:   filter.Monthly,
		})
		if serr != nil {
			data["Err"] = firstNonEmpty(pageErr, orderFacingError(c, serr))
			// 取数失败时**不渲染半张报表**：一半真一半 0 的页面比空态危险得多
			//（读的人会以为「这段时间只卖了一点点」）。
			data["LoadFailed"] = true
		} else {
			// service 归一化后的生效值才是页面上该显示的区间（请求可能给了未来日期）。
			filter.From = res.From
			filter.To = res.To
			filter.Status = res.Status
			filter.Monthly = res.Monthly
			filter.StatusLabel = salesStatusLabel(tr, res.Status)
			for k, v := range salesOverviewView(res, projects, selected, filter, tr) {
				data[k] = v
			}
		}
	} else {
		// 没有可用的工程（列表为空或装载失败）：退化成空态而不是查一个空 projectID
		//（空 id 在服务端会撞 ErrProjectRequired，页面显示的归口文案就变成「参数错误」，
		// 而真实原因是「一个工程都没有」）。
		for k, v := range salesOverviewEmptyView(projects, filter) {
			data[k] = v
		}
	}

	c.HTML(http.StatusOK, "admin/order/sales_overview.html", data)
}

// salesStatusQuickView 状态快捷项（含选中态与链接）。
//
// 链接保留当前区间与工程：点一下状态就把区间丢回默认，是本页最容易让人困惑的交互
// （「我明明选的是上个月」）。
func salesStatusQuickView(tr func(key, fallback string) string, current string) []map[string]any {
	out := make([]map[string]any, 0, len(salesStatusQuick))
	for _, s := range salesStatusQuick {
		label := ""
		if s.Key != "" {
			label = tr(s.Key, s.Fallback)
		} else {
			label = orderStatusLabel(tr, s.Value)
		}
		out = append(out, map[string]any{
			"Value":  s.Value,
			"Label":  label,
			"Active": s.Value == current,
		})
	}
	return out
}

// salesStatusLabel 生效状态筛选的展示文案（空串 → 「全部」）。
func salesStatusLabel(tr func(key, fallback string) string, status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return tr("admin.order.sales.status.all", "全部")
	}
	for _, s := range salesStatusQuick {
		if s.Value == status && s.Value != "" {
			return orderStatusLabel(tr, s.Value)
		}
	}
	// 多个状态（逗号串）：原样显示 —— 它是用户自己选的组合，
	// 而白名单里只有三个值，穷举所有组合去做翻译不值得。
	return status
}

// salesOverviewEmptyView 没有可用工程时的空态视图（与有数据时同名同义的键）。
func salesOverviewEmptyView(projects []projectdto.ProjectResp, filter salesFilterView) map[string]any {
	return map[string]any{
		"Projects":        projects,
		"SelectedProject": filter.Project,
		"Filter":          filter,
		"HasData":         false,
		"HasMonthly":      false,
		"MonthlyPoints":   []map[string]any{},
		"MonthlyMaxSales": int64(0),
		"Cards":           []salesCardView{},
		"Clients":         []salesCardView{},
		"Compare":         []salesCompareRowView{},
	}
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
		if n > 1000 {
			return 0
		}
	}
	return n
}
