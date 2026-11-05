package analyticsmcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go_wp/internal/mcp"
	analyticscontract "go_wp/internal/module/analytics/contract"
	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
)

const (
	trafficDefaultPathLimit = 10
	trafficMaxPathLimit     = 50
	trafficDefaultRankLimit = 10
	trafficMaxRankLimit     = 30

	trafficDailyInlineMax = 31
)

func TrafficTools(reader analyticscontract.TrafficReader) ([]mcp.Tool, error) {
	if reader == nil {
		return nil, errors.New("analyticsmcp: 访问统计依赖缺失（装配期接线错误）")
	}
	return []mcp.Tool{trafficSummary(reader)}, nil
}

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
		analyticsenums.AnalyticsView,
		mcp.Object("站点访问统计参数", map[string]mcp.Schema{
			"projectId": mcp.String("站点工程 id（uuid），用 site_projects 查"),
			"from":      mcp.String("起始日期，格式 YYYY-MM-DD，含当天（可选；不传则最近 30 天）"),
			"to":        mcp.String("结束日期，格式 YYYY-MM-DD，含当天（可选；不传则今天）"),
			"pathLimit": mcp.Integer("页面排行取前几页，默认 10，上限 50"),
			"rankLimit": mcp.Integer("来源域 / 设备 / 语言三组排行各取前几，默认 10，上限 30"),
		}, "projectId"),
		func(ctx context.Context, args trafficSummaryArgs) (mcp.Result, error) {

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

func peakDaysText(daily []analyticsdto.DailyCount, total int) string {
	sorted := make([]analyticsdto.DailyCount, len(daily))
	copy(sorted, daily)
	slices.SortStableFunc(sorted, func(a, b analyticsdto.DailyCount) int { return cmp.Compare(b.Views, a.Views) })
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

func pathLabel(p string) string {
	if strings.TrimSpace(p) == "" {
		return "/（首页）"
	}
	return p
}

func shortDay(day string) string {
	if len(day) >= 10 {
		return day[5:10]
	}
	return day
}
