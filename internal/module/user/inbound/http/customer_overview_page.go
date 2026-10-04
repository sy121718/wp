package userhttp

// customer_overview_page.go — 客户概览（GET /admin/customers/overview）。
//
// 这一页只回答一个问题：**这段时间客户是怎么变的** —— 来了多少新客、多少人回来下单、
// 复购率多少。四个数出自订单模块的一条聚合（ordercontract.CustomerGrowthReader），
// 本页不做任何自己的算术（口径的解释权只在拥有 orders 表的模块）。
//
// **为什么这一页不做「自定义区间」**：自定义要一套收敛规则（日期非法、首尾颠倒、
// 终点在未来、跨度超长），概览页已经解决过一次。在这里先复制一遍的代价不是多写二十行，
// 而是**两套收敛规则迟早分叉**（一个把未来日期收成今天、另一个报错），
// 而运营看不出哪一页是对的。先只给预设；真有人要自定义时，正确做法是把它收口成
// 共享实现（`pkg/`），而不是让这一页长出自己的版本。

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/utils"
)

// customerOverviewPath 页面路径（菜单 seed 与导航高亮都用它）。
const customerOverviewPath = "/admin/customers/overview"

// customerOverviewDefaultPreset 默认档位。
//
// 取「本月」而不是「今天」：客户的增长是按周按月才看得出形状的，默认给一天会让
// 新客数常年是 0 或 1，页面看起来像坏了。
const customerOverviewDefaultPreset = "month"

// customerOverviewPresets 可选档位（顺序即展示顺序）。
//
// 复用概览页那批时间词条（`admin.dashboard.range.*`）：同一批档位在后台出现两次，
// 各写一套文案的话迟早出现「两页的『本周』不是同一个意思」—— 而那是个没人会去核对的地方。
var customerOverviewPresets = []string{"today", "yesterday", "week", "month", "year"}

// customerOverviewTitleLabel 页标题（词条 key + 兜底文案）。
//
// 用 userLabel 而不是直接写字面 key：模板壳读的是 data["title"]，而 injectI18n 只翻
// **非空字符串**。给裸 key 时若该词条恰好没 seed 进库，顶栏会显示 "admin.customer.overview.title"。
var customerOverviewTitleLabel = userLabel{"admin.customer.overview.title", "客户概览"}

// customerOverviewRange 生效窗口（含首尾，UTC 日界，与订单聚合同口径）。
type customerOverviewRange struct {
	Key  string
	From string
	To   string
}

// customerOverviewRangeOf 把一个预设名换成窗口。认不出的名字回落默认档。
//
// 按 **UTC 日界**：订单的区间聚合一律用 UTC 的 day 桶，本地时区会让「今天的订单」
// 与「今天这个客户」在跨零点前后错开一个时区的量。
func customerOverviewRangeOf(key string, now time.Time) customerOverviewRange {
	today := customerOverviewDayStart(now)
	switch key {
	case "today":
		key = "today"
	case "yesterday":
		key = "yesterday"
		today = today.AddDate(0, 0, -1)
	case "week":
		today = today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	case "year":
		today = time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	case "month":
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	default:
		key = customerOverviewDefaultPreset
		today = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	// 结束日永远是「今天」（UTC）：week / month / year 三个档位是「本周期至今」。
	end := customerOverviewDayStart(now)
	return customerOverviewRange{
		Key:  key,
		From: today.Format(utils.LayoutDay),
		To:   end.Format(utils.LayoutDay),
	}
}

// customerOverviewDayStart 取某个时刻所在 UTC 日的零点。
//
// 与 order service 的 dayStart 同名同义，但**刻意各写一份**：两处对「日界」的定义
// 将来若分叉（订单能查任意历史、客户域可能引入本地时区偏好），共用一份会把它们绑死。
// 真要收口，落点应是 pkg/utils，而不是让 user 模块 import order 的 service。
func customerOverviewDayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// customerOverviewPresetLinks 预设 → 可点击的链接（URL 在服务端拼好，模板不拼 query）。
func customerOverviewPresetLinks(active string) []gin.H {
	out := make([]gin.H, 0, len(customerOverviewPresets))
	for _, key := range customerOverviewPresets {
		labelKey := "admin.dashboard.range." + key
		out = append(out, gin.H{
			"Key":      key,
			"LabelKey": labelKey,
			"URL":      customerOverviewPath + "?range=" + key,
			"Active":   key == active,
		})
	}
	return out
}

// customerOverviewPageData 组装渲染数据（纯函数：不取数、不依赖 gin.Context）。
//
// 与客户列表页同一条理由：渲染键名只在这里定义一次，真实渲染测试可以直接喂数据
// 走同一条组装路径，不必在测试里手抄一份键名。
func customerOverviewPageData(tr func(key, fallback string) string, rng customerOverviewRange, presets []gin.H, growth *orderdto.CustomerGrowthResp, errText string) gin.H {
	data := gin.H{
		"title":   userLabelOf(tr, customerOverviewTitleLabel),
		"Path":    customerOverviewPath,
		"Range":   rng,
		"Presets": presets,
		"Err":     errText,
		// GrowthReady 与「有数据」是两件事：未接线时给一句人话，而不是一片 0
		//（0 会被读成「这段时间一个客户都没来」）。同其它页面的降级口径。
		"GrowthReady": growth != nil,
	}
	if growth == nil {
		return data
	}
	data["OrderingCustomers"] = growth.OrderingCustomers
	data["NewCustomers"] = growth.NewCustomers
	data["ReturningCustomers"] = growth.ReturningCustomers
	data["Repurchasers"] = growth.Repurchasers
	data["NewRepurchasers"] = growth.NewRepurchasers
	data["RepurchaseRateLabel"] = growth.RepurchaseRateLabel
	return data
}

// CustomerOverviewPage 客户概览（GET /admin/customers/overview）。
func (h *customerPageHandle) CustomerOverviewPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	rng := customerOverviewRangeOf(c.Query("range"), time.Now())

	// 工程：客户增长是工程维度的（同一批人在两个站点上是两件事），
	// 取值规则与详情页逐字一致 —— 先看 ?project=，没有就用第一个。
	// 本页**不渲染工程切换器**：这一页回答的是「这段时间客户怎么变」，
	// 工程由从列表页带过来的上下文决定；多一个下拉只是多一个能选错的控件。
	var projects []projectcontract.ProjectResp
	if h.projects != nil {
		if list, perr := h.projects.List(ctx); perr == nil {
			projects = list
		}
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	var growth *orderdto.CustomerGrowthResp
	var errText string
	switch {
	case h.growth == nil:
		errText = userLabelOf(tr, customerUnavailableLabel)
	case selected == "":
		// 没有工程就没有订单可算 —— 「还没建站点工程」是正常状态，不是错误。
		// 留空：模板会把 GrowthReady=false 渲染成一句空态说明，而不是一片 0。
		errText = ""
	default:
		res, err := h.growth.CustomerGrowthByRange(ctx, &orderdto.CustomerGrowthReq{
			ProjectID: selected,
			From:      rng.From,
			To:        rng.To,
		})
		if err != nil {
			errText = customerFacingError(c, err)
		} else {
			growth = res
		}
	}

	data := shell.Prepare(c, customerOverviewPageData(tr, rng, customerOverviewPresetLinks(rng.Key), growth, errText))
	c.HTML(200, "admin/user/customer_overview", data)
}
