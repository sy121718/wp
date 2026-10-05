package analyticsmcp

// analytics_tools.go — 「站点访问情况」的只读工具。
//
// 为什么补这一个：用户问「这周有多少人访问」「哪个页面最热」「流量从哪来的」时，
// 答案全在 page_views 里，而在此之前 AI 侧看不到任何访问统计 —— 它会说
// 「我看不到流量数据」然后停下来。底层一直是齐的：service 的 Summary 一次就给出
// 总量（PV / UV）、按天曲线、页面排行、来源域排行、设备分类排行、语言排行。
//
// 只读：依赖收窄到 TrafficReader（一个方法），手里没有 Collect ——
// 统计数据是事实流水，被 AI 写进去的计数等于没有计数。
//
// 为什么一个工具而不是六个：那六组数字来自**同一次聚合**，拆成六个工具时
// 模型要回答「最近流量怎么样」得连调六次、每次都把 projectId 与窗口重报一遍，
// 而它们的时间口径必须一致才有意义（分两次调用就可能跨过日界）。

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"go_wp/internal/mcp"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	"go_wp/internal/permission"
)

// 排行与明细的默认条数。
//
// 默认 10 而不是 20（service 的默认值）：工具结果整段进模型上下文，
// 20 行页面排行够把一条提问的预算吃掉大半，而模型真正要回答的
// 「哪个页面最热」看前三行就够了。
const (
	trafficDefaultPathLimit = 10
	trafficMaxPathLimit     = 50
	trafficDefaultRankLimit = 10
	trafficMaxRankLimit     = 30
	// trafficDailyInlineMax 逐日明细最多铺多少天。
	//
	// 超过就只给「峰值几天 + 总量」：一次 90 天的窗口铺成 90 个「09-06:12」
	// 除了挤掉模型回答的空间之外没有别的效果，而「哪天最高」三个数就答完了。
	trafficDailyInlineMax = 31
)

// TrafficTools 返回访问统计模块的只读工具集。
func TrafficTools(reader analyticscontract.TrafficReader) ([]mcp.Tool, error) {
	if reader == nil {
		return nil, errors.New("analyticsmcp: 访问统计依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{trafficSummary(reader)}, nil
}

// trafficSummaryArgs traffic_summary 的入参。
type trafficSummaryArgs struct {
	ProjectID string `json:"projectId"`
	From      string `json:"from"`
	To        string `json:"to"`
	PathLimit int    `json:"pathLimit"`
	RankLimit int    `json:"rankLimit"`
}

func trafficSummary(reader analyticscontract.TrafficReader) mcp.Tool {
	return mcp.New("traffic_summary", "站点访问统计",
		"读站点的访问统计，用于回答「这周有多少人访问」「哪个页面最热」「流量从哪来的」「手机还是电脑看得多」。\n"+
			"一次给出六组同口径的数字：总浏览数（PV）、独立访客数（UV）、按天走势、页面排行、"+
			"来源域排行、设备分类排行、语言排行 —— 时间口径完全一致，可以互相印证。\n"+
			"projectId 用 site_projects 查（只有一个工程时就是它）。\n"+
			"from / to 是 YYYY-MM-DD、含当天、按 **UTC 日界**（产物面向全球访客，用 UTC 避免时区歧义）；"+
			"不传则默认最近 30 天。注意这里的「天」与订单的「天」是同一套 UTC 日界，可以并排比较。",
		permission.AnalyticsView,
		mcp.Object("站点访问统计参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid），用 site_projects 查"),
			"from":      mcp.String("起始日期，格式 YYYY-MM-DD，含当天（可选；不传则最近 30 天）"),
			"to":        mcp.String("结束日期，格式 YYYY-MM-DD，含当天（可选；不传则今天）"),
			"pathLimit": mcp.Integer("页面排行取前几页，默认 10，上限 50"),
			"rankLimit": mcp.Integer("来源域 / 设备 / 语言三组排行各取前几，默认 10，上限 30"),
		}, "projectId"),
		func(ctx context.Context, args trafficSummaryArgs) (mcp.Result, error) {
			// 纵深防御：projectId 已在 schema 的 required 里被拦过一道。这里再拦一次，
			// 是为了 schema 万一被误改（去掉 required）时仍不会带着空 id 去查全表 ——
			// 那次查询会成功返回一堆读不出站点的数字，而不是报错。
			if strings.TrimSpace(args.ProjectID) == "" {
				return mcp.Result{}, &mcp.ArgsError{Msg: "projectId 必填；先用 site_projects 查站点工程 id"}
			}
			res, err := reader.Summary(ctx, &analyticsdto.SummaryReq{
				ProjectID: args.ProjectID,
				From:      args.From,
				To:        args.To,
				PathLimit: clampTrafficLimit(args.PathLimit, trafficDefaultPathLimit, trafficMaxPathLimit),
				RankLimit: clampTrafficLimit(args.RankLimit, trafficDefaultRankLimit, trafficMaxRankLimit),
			})
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.Result{Text: describeTraffic(res), Data: res}, nil
		})
}

func clampTrafficLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// describeTraffic 把一次聚合写成模型能直接引用的几段。
func describeTraffic(res *analyticsdto.SummaryResp) string {
	if res == nil {
		return "没有取到访问统计。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "站点 %s 的访问统计（%s ~ %s，含当天）：\n", res.ProjectID, res.From, res.To)
	if res.Total == 0 {
		b.WriteString("这个窗口内**没有任何访问记录**。可能是这段时间确实没人来，也可能是站点还没接上访问打点 ——" +
			"如果站点刚上线，先确认产物的统计脚本是否生效，而不是当成「流量为零」汇报。\n")
		return b.String()
	}
	fmt.Fprintf(&b, "* 总浏览数（PV）：%d；独立访客数（UV）：%d\n", res.Total, res.Visitors)
	if len(res.Daily) > 0 {
		fmt.Fprintf(&b, "* 按天走势：%s\n", dailyText(res.Daily))
	}
	if len(res.Paths) > 0 {
		fmt.Fprintf(&b, "* 页面排行（前 %d，共 %d 个页面有访问）：\n%s\n",
			len(res.Paths), res.PathTotal, pathRankText(res.Paths))
	}
	writeRank(&b, "来源域", res.Referrers)
	writeRank(&b, "设备分类", res.UAClasses)
	writeRank(&b, "语言", res.Langs)
	return strings.TrimRight(b.String(), "\n")
}

// dailyText 按天走势。
//
// 短窗口铺成「日期:PV」一行（模型可以逐日念给用户）；长窗口只给峰值三天的
// 加总量 —— 铺 90 天的明细除了挤掉回答空间没有别的效果。
func dailyText(daily []analyticsdto.DailyCount) string {
	if len(daily) > trafficDailyInlineMax {
		return peakDaysText(daily, len(daily))
	}
	parts := make([]string, 0, len(daily))
	for _, d := range daily {
		parts = append(parts, fmt.Sprintf("%s:%d", shortDay(d.Day), d.Views))
	}
	return fmt.Sprintf("%d 天有访问（日期:浏览数）—— %s", len(daily), strings.Join(parts, "、"))
}

// peakDaysText 浏览数最高的三天 + 窗口天数。
func peakDaysText(daily []analyticsdto.DailyCount, total int) string {
	sorted := make([]analyticsdto.DailyCount, len(daily))
	copy(sorted, daily)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Views > sorted[j].Views })
	n := 3
	if len(sorted) < n {
		n = len(sorted)
	}
	parts := make([]string, 0, n)
	for _, d := range sorted[:n] {
		parts = append(parts, fmt.Sprintf("%s（%d 次、%d 位访客）", d.Day, d.Views, d.Visitors))
	}
	return fmt.Sprintf("窗口内共 %d 天有访问，最多的三天是 %s", total, strings.Join(parts, "、"))
}

func pathRankText(paths []analyticsdto.PathCount) string {
	var b strings.Builder
	for i, p := range paths {
		fmt.Fprintf(&b, "  %d. %s — %d 次浏览、%d 位访客\n", i+1, pathLabel(p.Path), p.Views, p.Visitors)
	}
	return strings.TrimRight(b.String(), "\n")
}

// writeRank 写一组维度排行（来源域 / 设备 / 语言）。
//
// 空取值是**合法取值**（没有来源 / UA 缺失 / 没上报语言，见 RankCount 的注释），
// 渲染成占位文案而不是跳过 —— 跳过会让「一半流量没有来源」看起来像数据缺失。
func writeRank(b *strings.Builder, title string, ranks []analyticsdto.RankCount) {
	if len(ranks) == 0 {
		return
	}
	parts := make([]string, 0, len(ranks))
	for _, r := range ranks {
		parts = append(parts, fmt.Sprintf("%s %d 次", emptyDimLabel(r.Value), r.Views))
	}
	fmt.Fprintf(b, "* %s：%s\n", title, strings.Join(parts, " · "))
}

func emptyDimLabel(v string) string {
	if strings.TrimSpace(v) == "" {
		return "（未标注）"
	}
	return v
}

// pathLabel 路径展示：空串是首页（统计侧存的就是空 path）。
func pathLabel(p string) string {
	if strings.TrimSpace(p) == "" {
		return "/（首页）"
	}
	return p
}

// shortDay 「2026-10-05」→「10-05」。窗口内不会跨年（上限一年），月份日期足够定位。
func shortDay(day string) string {
	if len(day) >= 10 {
		return day[5:10]
	}
	return day
}
