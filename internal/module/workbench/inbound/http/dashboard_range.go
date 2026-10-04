// dashboard_range.go — 概览页的时间区间：预设 + 自定义，以及趋势的粒度选择。
//
// 为什么区间解析在**这里**算完再往下传：三个块（KPI / 趋势 / 榜单）必须是同一个窗口。
// 让每个端口各自解释「本月」，迟早出现「KPI 说本月 12 单、趋势图却画的是近 7 天」——
// 页面在自相矛盾，而每一块单独看都对。所以这里只算出 from/to 两个日期字符串往下传，
// 下游不认识「本月」这个词。
//
// 日界统一 UTC，与 order 的按天桶、analytics 的按天聚合同口径（见 dashboard_overview.go 文件头）。
package workbenchhttp

import (
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/utils"
)

// dashboardPath 概览页自己的地址。
//
// 它是 `/admin` 而不是 `/admin/dashboard` —— 仪表盘挂在 workbench 的**根级前缀组**里，
// 注册语句是 router.go 的 SetupWorkbenchRoutes 中的 `g.GET("/admin", h.Dashboard)`。
// 写 `/admin/dashboard` 得到的是 404（页面不报错，只是筛选条点下去什么都不发生），
// 所以模板里的 form action 与本常量必须同源，并由
// TestAdminDashboardFormActionMatchesRoute 钉住。
const dashboardPath = "/admin"

// 预设键（页面上那一排按钮）。
const (
	rangeToday     = "today"
	rangeYesterday = "yesterday"
	rangeWeek      = "week"
	rangeMonth     = "month"
	rangeYear      = "year"
	rangeCustom    = "custom"
)

// rangeDefault 默认区间：本周。
//
// 为什么不是「今日」：概览页最常见的问法是「最近怎么样」，而今天的数字在上午
// 往往只有几单，看不出形状。本周既有几天的量、又不至于把「今天」淹掉。
const rangeDefault = rangeWeek

// rangeMaxDays 自定义区间的天数上限（含首尾）。
//
// 挡的是 `from=1970-01-01` 这类请求：它会变成一次全表扫描，而概览页是登录后的第一跳。
// 366 天足够覆盖「去年同期」这类正常需求。
const rangeMaxDays = 366

// trendDailyMaxDays 超过这个天数，趋势就按**周**聚合。
//
// 按天画 365 根柱子在页面上既看不清也没意义（每根不足 1px），按周聚合后最多 53 根，
// 形状仍然读得出来。粒度跟着区间走而不是给用户一个「粒度」下拉：那等于让人自己
// 算一遍「多长的区间该用什么粒度」，而他本来只想看「这个月的走势」。
const trendDailyMaxDays = 31

// overviewRange 一个已生效的区间（含首尾两端）。
type overviewRange struct {
	// Key 生效的预设键（自定义区间被收敛后会变成 custom）。
	Key string
	// From / To YYYY-MM-DD，**含**首尾两端。
	From string
	To   string
	// Days 区间天数（含首尾）：1 表示就是一天。
	Days int
	// Weekly 为真时趋势按周聚合（见 trendDailyMaxDays）。
	Weekly bool
	// Clamped 为真表示请求的区间被收敛过（超长 / 未来日期 / 反向）。
	//
	// 页面据此提示一句「已按最长 366 天截取」：静默改口径比报错更糟 ——
	// 用户会以为自己看到的就是他选的那一段。
	Clamped bool
}

// rangePreset 筛选条上的一个预设按钮。
type rangePreset struct {
	Key string
	// LabelKey 完整词条键（模板直接用它取词条，不做字符串拼接 ——
	// Jet 里的拼接要依赖表达式的求值顺序，而这里没有非拼不可的理由）。
	LabelKey string
	Label    string
	// URL 直接可点（带上当前工程等其它筛选），模板不必拼 query。
	URL    string
	Active bool
}

// parseOverviewRange 解析请求里的区间参数，收敛到合法值。
//
// 收敛而不是拒绝：概览页是「打开就看」的页面，为一个越界的日期参数回 400
// 只会让用户看到一页错误 —— 而他要的信息（最近的情况）本来就能给。
func parseOverviewRange(c *gin.Context, today time.Time) overviewRange {
	key := strings.TrimSpace(c.Query("range"))
	if key == "" {
		key = rangeDefault
	}
	day := func(t time.Time) string { return t.Format(utils.LayoutDay) }

	switch key {
	case rangeToday:
		d := day(today)
		return newRange(rangeToday, d, d, false)
	case rangeYesterday:
		d := day(today.AddDate(0, 0, -1))
		return newRange(rangeYesterday, d, d, false)
	case rangeWeek:
		return defaultRange(today)
	case rangeMonth:
		first := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		return newRange(rangeMonth, day(first), day(today), false)
	case rangeYear:
		first := time.Date(today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return newRange(rangeYear, day(first), day(today), false)
	case rangeCustom:
		return parseCustomRange(c, today)
	default:
		// 认不出的键按默认走（老链接、手改的 URL）：不报错、也不猜语义。
		return defaultRange(today)
	}
}

// defaultRange 默认区间（本周）。
//
// ISO 周：周一为起点（time.Weekday 的周日是 0，要单独折一下）。
func defaultRange(today time.Time) overviewRange {
	offset := (int(today.Weekday()) + 6) % 7
	d := func(t time.Time) string { return t.Format(utils.LayoutDay) }
	return newRange(rangeDefault, d(today.AddDate(0, 0, -offset)), d(today), false)
}

// parseCustomRange 解析 from / to。
//
// 四种收敛各自的理由：
//   - 解析不出来 → 用默认区间（可能是手改的 URL，或复制时截断了）；
//   - from > to → 交换。用户想表达的是「这几天」，顺序写反不该变成空区间；
//   - to 在今天之后 → 收到今天。未来的订单不存在，窗口伸到未来只会让「日均」变小；
//   - 超过 rangeMaxDays → 保留 **to** 往前截，保住「最近」这一半（用户真正关心的那半）。
func parseCustomRange(c *gin.Context, today time.Time) overviewRange {
	from, errFrom := time.Parse(utils.LayoutDay, strings.TrimSpace(c.Query("from")))
	to, errTo := time.Parse(utils.LayoutDay, strings.TrimSpace(c.Query("to")))
	if errFrom != nil || errTo != nil {
		return defaultRange(today)
	}
	from, to = from.UTC(), to.UTC()
	clamped := false
	if from.After(to) {
		from, to = to, from
		clamped = true
	}
	if to.After(today) {
		to = today
		clamped = true
	}
	if days := int(to.Sub(from).Hours()/24) + 1; days > rangeMaxDays {
		from = to.AddDate(0, 0, -(rangeMaxDays - 1))
		clamped = true
	}
	r := newRange(rangeCustom, from.Format(utils.LayoutDay), to.Format(utils.LayoutDay), clamped)
	return r
}

// newRange 组装区间并算出天数与趋势粒度。
func newRange(key, from, to string, clamped bool) overviewRange {
	f, errF := time.Parse(utils.LayoutDay, from)
	t, errT := time.Parse(utils.LayoutDay, to)
	days := 1
	if errF == nil && errT == nil {
		days = int(t.Sub(f).Hours()/24) + 1
		if days < 1 {
			days = 1
		}
	}
	return overviewRange{
		Key:     key,
		From:    from,
		To:      to,
		Days:    days,
		Weekly:  days > trendDailyMaxDays,
		Clamped: clamped,
	}
}

// rangePresetURL 拼一个预设按钮的地址：保留其它筛选，只换 range（自定义时清掉 from/to）。
//
// 逐参数白名单而不是把原 query 整串拼回去：那样会把 from/to 一起带到预设链接里，
// 于是「点本周」带着上次的自定义区间一起提交，页面回到 custom。
func rangePresetURL(c *gin.Context, key, from, to string) string {
	q := url.Values{}
	if p := strings.TrimSpace(c.Query("project")); p != "" {
		q.Set("project", p)
	}
	q.Set("range", key)
	if key == rangeCustom {
		if from != "" {
			q.Set("from", from)
		}
		if to != "" {
			q.Set("to", to)
		}
	}
	return dashboardPath + "?" + q.Encode()
}

// rangePresets 筛选条上的一排按钮。
//
// Label 是**中文兜底**：页面侧用 i18n key 取词条，取不到就显示这个。
// 键名规则见 check-i18n-coverage：admin.<模块>.<语义>。
func rangePresets(c *gin.Context, active overviewRange) []rangePreset {
	keys := []string{rangeToday, rangeYesterday, rangeWeek, rangeMonth, rangeYear, rangeCustom}
	out := make([]rangePreset, 0, len(keys))
	for _, k := range keys {
		out = append(out, rangePreset{
			Key:      k,
			LabelKey: "admin.dashboard.range." + k,
			Label:    rangePresetLabel(k),
			URL:      rangePresetURL(c, k, active.From, active.To),
			Active:   active.Key == k,
		})
	}
	return out
}

// rangePresetLabel 预设的中文兜底文案（词条在迁移 550）。
func rangePresetLabel(key string) string {
	switch key {
	case rangeToday:
		return "今日"
	case rangeYesterday:
		return "昨日"
	case rangeWeek:
		return "本周"
	case rangeMonth:
		return "本月"
	case rangeYear:
		return "本年"
	case rangeCustom:
		return "自定义"
	default:
		return key
	}
}

// withPresets 补上筛选条的预设按钮（URL 已按当前筛选拼好）。
//
// 单独一步而不是塞进 collectOverview：那一层算的是数据，不认识也不该认识 *gin.Context
// —— 筛选条的链接是展示层的事。漏调它的代价只是「按钮没有选中态」，
// 而不是「数据算错」，两件事不该绑在一次调用里。
func withPresets(c *gin.Context, snap overviewSnapshot) overviewSnapshot {
	snap.Presets = rangePresets(c, snap.Range)
	return snap
}
