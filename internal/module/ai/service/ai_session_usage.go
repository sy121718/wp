package aiservice

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	aidto "go_wp/internal/module/ai/dto"
	aimodel "go_wp/internal/module/ai/model"
)

const (
	// sessionTrendDaysDefault 趋势图默认窗口（天）。
	sessionTrendDaysDefault = 30
	// sessionTrendDaysMax 趋势窗口上限（天）。同一张图再宽下去每根柱子不到 2px，
	// 看不出形状反而更贵；要看更长的历史就靠筛选把区间切小分段看。
	sessionTrendDaysMax = 90
)

// ListSessionsFiltered 多条件分页列会话（口径与 ListSessions 一致，条件更多）。
func (s *SessionService) ListSessionsFiltered(ctx context.Context, f aidto.SessionQuery, page, size int) ([]aidto.Session, int64, error) {
	page, size = normalizeSessionPage(page, size)
	rows, total, err := s.model.ListSessionsFiltered(ctx, sessionQueryOf(f), (page-1)*size, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]aidto.Session, 0, len(rows))
	for i := range rows {
		out = append(out, sessionDTO(&rows[i]))
	}
	return out, total, nil
}

// SessionUsageOf 指标卡（会话数 / 事件数 / 累计 token）。
func (s *SessionService) SessionUsageOf(ctx context.Context, f aidto.SessionQuery) (aidto.SessionUsage, error) {
	raw, err := s.model.SessionUsageOf(ctx, sessionQueryOf(f))
	if err != nil {
		return aidto.SessionUsage{}, err
	}
	out := aidto.SessionUsage{
		Sessions: raw.Sessions,
		Events:   raw.Events,
		Tokens:   raw.Tokens,
		Compacts: raw.Compacts,
	}
	if raw.Sessions > 0 {
		// 除法的守卫放在这里而不是让模板自己判：模板里每个用到均值的地方都得记得判一次。
		out.AvgTokens = raw.Tokens / raw.Sessions
	}
	out.TokensText = formatTokens(out.Tokens)
	out.AvgTokensText = formatTokens(out.AvgTokens)

	// —— docs/16 §3.1 的三个验收数字 ——
	//
	// 命中率的分母只取**报了缓存字段**的那些调用（CachedInputTokens）：
	// 把没报的算进分母会让命中率凭空掉一大截，而「难看」这个症状会被归因到提示词上，
	// 没人会想到是上游没报。一条都没报时 HitRateReady=false，页面显示「—」而不是 0%。
	out.HitRateReady = raw.CachedInputTokens > 0
	out.HitRatePct = percentage(raw.CachedTokens, raw.CachedInputTokens)
	out.HitRateText = percentText(out.HitRatePct, out.HitRateReady)
	out.CachedCalls = raw.CachedCalls
	out.UsageReportedCalls = raw.UsageReportedCalls

	// 压缩开销：摘要占的上下文比重。分母用**事件**的 token 之和（不是调用流水）——
	// 这个数问的是「压缩后的上下文里，摘要本身占了多大一块」，所以两侧必须同源。
	out.CompactCostPct = percentage(raw.SummaryTokens, raw.Tokens)
	out.CompactCostText = percentText(out.CompactCostPct, raw.Tokens > 0)
	return out, nil
}

// percentage 求分子占分母的百分比（一位小数）；分母 <= 0 时回 0。
//
// 回 0 而不是 NaN：NaN 会一路渲染成「NaN%」，且不报错、不进日志。
func percentage(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

// percentText 百分比展示串。ready=false 时回「—」。
//
// **不能用 0% 表示「没数据」**：0% 命中率是一个严重的信号（前缀每轮都在变），
// 而「这家没报这个字段」完全不是一回事。把两者显示成同一个字符，
// 等于把最有价值的一条观测信息抹掉。
func percentText(pct float64, ready bool) string {
	if !ready {
		return "—"
	}
	return strconv.FormatFloat(pct, 'f', 1, 64) + "%"
}

// SessionFilterOptions 筛选下拉的候选值（供应商 / 模型 / 创建人）。
//
// 取全量：筛成 A 之后下拉里还得有 B，否则筛一次就回不去。
func (s *SessionService) SessionFilterOptions(ctx context.Context) (aidto.SessionFilterOptions, error) {
	facets, err := s.model.SessionFacetsOf(ctx)
	if err != nil {
		return aidto.SessionFilterOptions{}, err
	}
	// append 到空切片而不是直接赋值：Pluck 在零行时给的是 nil，
	// 直接赋进 json 会变成 null，前端就得为 null 再写一遍判空。
	return aidto.SessionFilterOptions{
		Providers: append([]string{}, facets.Providers...),
		Models:    append([]string{}, facets.Models...),
		Creators:  append([]int64{}, facets.Creators...),
	}, nil
}

// 折线图坐标系与上限。viewBox 是固定值 + preserveAspectRatio="none"：图自适应容器宽度，
// 描边靠 CSS 的 vector-effect: non-scaling-stroke 保住 2px，不被横向拉伸带粗。
const (
	trendViewW = 1000
	trendViewH = 200
	// trendPadX 左右各留一点：x=0 的点描边有一半落在 viewBox 外，会被裁掉半条线。
	trendPadX = 6
	trendPadY = 12
	// trendSeriesMax 折线最多画几条：再多颜色就分不开、图例也压成一团。
	// 超出的不丢弃，合并成一条「其他」（丢掉会让图上总量对不上指标卡）。
	trendSeriesMax = 8
	// trendColorCount 调色板色数（--chart-c1..8）。
	trendColorCount = 8
)

// SessionTrend 折线图：按 (供应商, 模型) 分组的逐日 token。
//
// 为什么是折线而不是柱状：柱状图一天一根、每根只有一个高度，多序列只能堆叠或并排 ——
// 堆叠看不出单条趋势，并排又把一天切成一簇细柱。折线天然就是多序列的形态。
//
// 窗口规则与旧柱状图版一致：终点取筛选区间的结束日（缺省今天），起点往回推 trendDays-1 天，
// 但不早于筛选起点；筛选区间比窗口窄时以筛选为准（图与表说的是同一段时间）。
// 有数据的第一天之前不画 —— 那一段本来就是一片 0，画出来只会把真正的变化压成一条直线
// （实测「30 天窗口里只有一天有数据」时，旧版图上是 1 根柱 + 29 根看不见的底线，看着像坏了）。
func (s *SessionService) SessionTrend(ctx context.Context, f aidto.SessionQuery, trendDays int) (aidto.SessionTrend, error) {
	q := sessionQueryOf(f)
	span := trendDays
	if span < 1 || span > sessionTrendDaysMax {
		span = sessionTrendDaysDefault
	}

	end := dayStart(time.Now())
	if q.To != nil {
		end = *q.To
	}
	start := end.AddDate(0, 0, -(span - 1))
	if q.From != nil && q.From.After(start) {
		start = *q.From
	}
	if start.After(end) {
		// 筛选给了早于结束日的起点之类的手改区间：收敛成一天，不要造出空区间。
		start = end
	}

	rows, err := s.model.SessionTokenTrendBySeries(ctx, q, start, "day")
	if err != nil {
		return aidto.SessionTrend{}, err
	}
	// 用 rows[0] 而不是「最早的事件时间」—— 服务端不为此多查一次库。
	if len(rows) > 0 {
		if first := dayStart(rows[0].Day.UTC()); first.After(start) {
			start = first
		}
	}

	days := make([]time.Time, 0, span)
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		days = append(days, cur)
	}
	dayIndex := make(map[string]int, len(days))
	for i := range days {
		dayIndex[days[i].Format("2006-01-02")] = i
	}

	// 按 (供应商, 模型) 归组。map 遍历顺序不定，所以另存一份出现顺序 ——
	// 同总量时的排序稳定性依赖它。
	type bucket struct {
		tokens []int64
		total  int64
		// count 只在「其他」那条上非零：并入的家数。
		count int
	}
	groups := make(map[string]*bucket)
	order := make([]string, 0)
	for i := range rows {
		key := rows[i].ProviderKey + "\x00" + rows[i].ModelID
		bk := groups[key]
		if bk == nil {
			bk = &bucket{tokens: make([]int64, len(days))}
			groups[key] = bk
			order = append(order, key)
		}
		// ::date 出来的是 UTC 零点，直接用本地时区格式化会偏移一天。
		if di, ok := dayIndex[rows[i].Day.UTC().Format("2006-01-02")]; ok {
			bk.tokens[di] += rows[i].Tokens
			bk.total += rows[i].Tokens
		}
	}

	var peak int64
	for _, key := range order {
		for _, v := range groups[key].tokens {
			if v > peak {
				peak = v
			}
		}
	}

	// 总量降序：消耗最大的那条排最上面，也拿到调色板第 1 号色。
	sort.SliceStable(order, func(i, j int) bool { return groups[order[i]].total > groups[order[j]].total })

	if len(order) > trendSeriesMax {
		rest := &bucket{tokens: make([]int64, len(days)), count: len(order) - trendSeriesMax + 1}
		for _, key := range order[trendSeriesMax-1:] {
			bk := groups[key]
			rest.total += bk.total
			for i := range bk.tokens {
				rest.tokens[i] += bk.tokens[i]
			}
		}
		order = append(order[:trendSeriesMax-1], "\x00other")
		groups["\x00other"] = rest
	}

	out := aidto.SessionTrend{
		From:     start.Format("2006-01-02"),
		To:       end.Format("2006-01-02"),
		Peak:     peak,
		PeakText: formatTokens(peak),
		Series:   make([]aidto.TrendSeries, 0, len(order)),
	}
	for i, key := range order {
		bk := groups[key]
		pts := make([]string, 0, len(days))
		var dotX, dotY int
		for di, v := range bk.tokens {
			// 单点时落正中：它没有「首尾」可言，贴左反而像被裁掉了。
			x := trendViewW / 2
			if len(days) > 1 {
				x = trendPadX + di*(trendViewW-2*trendPadX)/(len(days)-1)
			}
			y := trendViewH - trendPadY
			if peak > 0 {
				y = trendViewH - trendPadY - int(v*int64(trendViewH-2*trendPadY)/peak)
			}
			pts = append(pts, strconv.Itoa(x)+","+strconv.Itoa(y))
			dotX, dotY = x, y
		}
		// key 是 "provider\x00model"（「其他」那条是 "\x00other"）：在这里拆回字段，
		// 不另存一份 label —— 图例文案由模板按语言组装。
		parts := strings.SplitN(key, "\x00", 2)
		out.Series = append(out.Series, aidto.TrendSeries{
			ProviderKey: parts[0],
			ModelID:     parts[1],
			OtherCount:  bk.count,
			Total:       bk.total,
			TotalText:   formatTokens(bk.total),
			Color:       (i % trendColorCount) + 1,
			Points:      strings.Join(pts, " "),
			Single:      len(days) == 1,
			DotX:        dotX,
			DotY:        dotY,
		})
	}
	return out, nil
}

// sessionQueryOf dto 筛选条件 → model 查询条件。
func sessionQueryOf(f aidto.SessionQuery) aimodel.SessionQuery {
	return aimodel.SessionQuery{
		Keyword:     strings.TrimSpace(f.Keyword),
		Status:      f.Status,
		ProviderKey: strings.TrimSpace(f.ProviderKey),
		ModelID:     strings.TrimSpace(f.ModelID),
		CreateBy:    f.CreateBy,
		From:        parseDayStart(f.From),
		To:          parseDayStart(f.To),
	}
}

// parseDayStart 把 yyyy-mm-dd 解析成本地时区当天零点；空串或格式不对返回 nil（= 不限）。
//
// 解析失败不报错：这三个字符来自 URL，手改错一个数字就让整页 500 不成比例；
// 按「不限」处理在页面上是看得见的（筛选栏那格还是空的），不会静默给出错结果。
func parseDayStart(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return nil
	}
	return &t
}

// dayStart 抹掉时分秒，只留本地日期。
func dayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// normalizeSessionPage 归一化分页参数（口径与 ListSessions 一致）。
func normalizeSessionPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}

// formatTokens 把 token 数压成短文本（1234 → "1234"，12345 → "12.3K"，12345678 → "12.3M"）。
//
// 万以下给原数：看板上「事件数 4」「token 15」这种小数字用 K 表示会变成 0.0K，
// 比原数难读。原始整数仍随 dto 一起给出，模板要精确值就用整数字段。
func formatTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return strconv.FormatFloat(float64(n)/1e9, 'f', 1, 64) + "B"
	case n >= 1_000_000:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	case n >= 10_000:
		return strconv.FormatFloat(float64(n)/1e3, 'f', 1, 64) + "K"
	default:
		return strconv.FormatInt(n, 10)
	}
}

// SessionTokenUsageOf 一个会话的 token 消耗快照（详情抽屉顶部那块数）。
//
// 两个口径来自两处：上下文与压缩次数读会话行（折叠后的当下状态），累计与明细按 kind
// 聚合事件表（历史总量）。它们本来就不是一个数 —— 折叠会让前者变小、后者不变。
func (s *SessionService) SessionTokenUsageOf(ctx context.Context, id int64) (out aidto.SessionTokenUsage, err error) {
	detail, err := s.GetSession(ctx, id)
	if err != nil {
		return out, err
	}
	out.ContextTokens = detail.ContextTokens
	out.Compacts = int64(detail.CompactCount)
	out.Rows = []aidto.SessionTokenRow{}

	rows, err := s.model.SessionKindUsageOf(ctx, id)
	if err != nil {
		// 聚合失败不该让整个抽屉打不开：三个数照给，只是没有明细表。
		return out, err
	}
	var total int64
	for i := range rows {
		total += rows[i].Tokens
		out.Rows = append(out.Rows, aidto.SessionTokenRow{
			Kind:   rows[i].Kind,
			Events: rows[i].Events,
			Tokens: rows[i].Tokens,
		})
	}
	out.Total = total
	out.TotalText = formatTokens(total)
	out.HasBreakdown = len(out.Rows) > 0
	return out, nil
}

// SessionModelUsageOf 批量取一组会话的按 (供应商, 模型) 拆分，按会话 id 分组返回。
//
// 列表页一次拿整页：悬浮卡是「鼠标一停就要看到」的东西，逐行查就是 20 次往返，
// 而第 20 行的卡永远在等第 20 次查询。
// 未记录来源的那一组（529 之前写入的历史事件）标 Unrecorded —— 展示层据此显示「未记录」，
// 而不是把它当成「某个名字为空的供应商」。
func (s *SessionService) SessionModelUsageOf(ctx context.Context, sessionIDs []int64) (out map[int64][]aidto.SessionModelUsage, err error) {
	out = make(map[int64][]aidto.SessionModelUsage, len(sessionIDs))
	rows, err := s.model.SessionModelUsageOf(ctx, sessionIDs)
	if err != nil {
		return out, err
	}
	for i := range rows {
		pk := strings.TrimSpace(rows[i].ProviderKey)
		mid := strings.TrimSpace(rows[i].ModelID)
		out[rows[i].SessionID] = append(out[rows[i].SessionID], aidto.SessionModelUsage{
			ProviderKey: pk,
			ModelID:     mid,
			Events:      rows[i].Events,
			Tokens:      rows[i].Tokens,
			TokensText:  formatTokens(rows[i].Tokens),
			Unrecorded:  pk == "" && mid == "",
		})
	}
	return out, nil
}
