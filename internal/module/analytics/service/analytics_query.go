package analyticsservice

// analytics_query.go — 后台只读聚合（按天 / 按路径 / 时间范围筛选 + 分页）。

import (
	"context"
	"strings"
	"time"

	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsmodel "go_wp/internal/module/analytics/model"
)

const (
	// defaultRangeDays 未指定时间范围时的默认窗口（含今天在内 30 天）。
	defaultRangeDays = 30
	// maxRangeDays 单次查询的最大跨度：一天一行乘以天数就是返回体量，
	// 不设上限的话一次请求就能让后台页面渲染上万行。
	maxRangeDays = 366
	// defaultPathLimit / maxPathLimit 路径聚合的每页条数。
	defaultPathLimit = 20
	maxPathLimit     = 200
)

// dateLayout 请求与响应里的日期格式（后台表单与 JSON 同一格式）。
const dateLayout = "2006-01-02"

// Summary 按天与按路径聚合浏览数。
//
// 聚合是**纯读**：没有任何写入口，也不缓存（缓存会让「刚发生的访问看不到」
// 变成常态，而运营看统计时最想确认的就是刚发的文章有没有人来）。
func (s *Service) Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, ErrInvalidParam
	}
	from, to, rerr := s.normalizeWindow(req.From, req.To)
	if rerr != nil {
		return nil, rerr
	}
	page, limit := normalizePathPaging(req.PathPage, req.PathLimit)

	// 取数来源（审计 DB-005 / IDX-010）：窗口完全落在今天之前 → 读按天预聚合表；
	// 窗口含今天 → 读明细表。
	//
	// 为什么含今天就整体走明细：预聚合里的「今天」是最近一次汇总任务的快照（最多一小时前），
	// 而运营打开统计页最想确认的恰恰是刚发布的文章有没有人看 —— 那种滞后造成的
	// 「数字没动」会被当成故障。今天的数据量本身也远小于历史窗口，走明细不吃亏。
	var (
		views, visitors int64
		dayRows         []analyticsmodel.DayRow
		pathTotal       int64
		pathRows        []analyticsmodel.PathRow
		source          string
	)
	// 走汇总的条件有两个，缺一不可：
	//   - 窗口完全落在今天之前（今天的汇总行只是快照，见上方说明）；
	//   - 窗口内**每一天都已汇总**（补齐是按批次推进的，长历史可能还没追完）。
	//
	// 第二个条件是正确性底线：少几天就是少几次访问，而这种偏差从页面上根本看不出来。
	// 宁可退回明细慢一点，也不能给出一个少了几天数据的数字。
	windowDays := int(to.Sub(from) / (24 * time.Hour))
	rolledDays, cerr := s.m.CountRolledDays(ctx, projectID, from, to)
	if cerr != nil {
		return nil, cerr
	}
	if !to.After(dayStart(s.now())) && windowDays > 0 && int64(windowDays) == rolledDays {
		source = analyticsdto.SourceSummary
		if views, visitors, err = s.m.RollupTotals(ctx, projectID, from, to); err != nil {
			return nil, err
		}
		if dayRows, err = s.m.RollupByDay(ctx, projectID, from, to); err != nil {
			return nil, err
		}
		if pathTotal, err = s.m.RollupPathTotal(ctx, projectID, from, to); err != nil {
			return nil, err
		}
		// 路径排行：有游标走 keyset（成本与页码无关），否则按页码 offset。
		if after := strings.TrimSpace(req.PathAfter); after != "" {
			pathRows, err = s.m.RollupByPathKeyset(ctx, projectID, from, to, req.PathAfterViews, after, limit)
		} else {
			pathRows, err = s.m.RollupByPath(ctx, projectID, from, to, (page-1)*limit, limit)
		}
		if err != nil {
			return nil, err
		}
	} else {
		source = analyticsdto.SourceDetail
		if views, visitors, err = s.m.CountRange(ctx, projectID, from, to); err != nil {
			return nil, err
		}
		if dayRows, err = s.m.CountByDay(ctx, projectID, from, to); err != nil {
			return nil, err
		}
		if pathTotal, err = s.m.CountPathTotal(ctx, projectID, from, to); err != nil {
			return nil, err
		}
		if pathRows, err = s.m.CountByPath(ctx, projectID, from, to, (page-1)*limit, limit); err != nil {
			return nil, err
		}
	}

	daily := make([]analyticsdto.DailyCount, 0, len(dayRows))
	for _, row := range dayRows {
		daily = append(daily, analyticsdto.DailyCount{
			Day:      row.Day.UTC().Format(dateLayout),
			Views:    row.Views,
			Visitors: row.Visitors,
		})
	}
	paths := make([]analyticsdto.PathCount, 0, len(pathRows))
	for _, row := range pathRows {
		paths = append(paths, analyticsdto.PathCount{Path: row.Path, Views: row.Views, Visitors: row.Visitors})
	}
	// 下一页游标取本页最后一行；空结果时留空（客户端据此收起翻页按钮）。
	var nextViews int64
	var nextPath string
	if n := len(paths); n > 0 {
		nextViews, nextPath = paths[n-1].Views, paths[n-1].Path
	}
	return &analyticsdto.SummaryResp{
		ProjectID: projectID,
		From:      from.Format(dateLayout),
		// 回显的结束日期是**含当天**的（内部窗口是半开区间，这里换回人看的写法）。
		To:        to.AddDate(0, 0, -1).Format(dateLayout),
		Total:     views,
		Visitors:  visitors,
		Daily:     daily,
		Paths:     paths,
		PathTotal: pathTotal,
		PathPage:  page,
		PathLimit: limit,

		Source:             source,
		PathNextAfterViews: nextViews,
		PathNextAfter:      nextPath,
	}, nil
}

// normalizeWindow 把请求里的日期字符串归一化成半开窗口 [from, to)。
//
// 规则（全部以 **UTC 日界**为准，与按天聚合的口径一致）：
//   - 空 from / 空 to → 默认最近 defaultRangeDays 天（含今天）；
//   - to 晚于今天 → 收敛到今天（未来的日期没有数据，也不该被当成合法窗口）；
//   - from 晚于 to → ErrInvalidRange；跨度超过 maxRangeDays → ErrInvalidRange。
func (s *Service) normalizeWindow(rawFrom, rawTo string) (from, to time.Time, err error) {
	today := dayStart(s.now())
	to = today.AddDate(0, 0, 1)
	if t, ok := parseDay(rawTo); ok {
		to = t.AddDate(0, 0, 1)
		if to.After(today.AddDate(0, 0, 1)) {
			to = today.AddDate(0, 0, 1)
		}
	}
	from = to.AddDate(0, 0, -defaultRangeDays)
	if t, ok := parseDay(rawFrom); ok {
		from = t
	}
	if from.After(to.AddDate(0, 0, -1)) {
		return time.Time{}, time.Time{}, ErrInvalidRange
	}
	if to.Sub(from) > time.Duration(maxRangeDays)*24*time.Hour {
		return time.Time{}, time.Time{}, ErrInvalidRange
	}
	return from, to, nil
}

// parseDay 解析 YYYY-MM-DD（非法 / 空返回 ok=false，由调用方走默认值）。
func parseDay(raw string) (t time.Time, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation(dateLayout, raw, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// dayStart 取某个时刻所在 UTC 日的零点。
func dayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// normalizePathPaging 归一化分页参数（页码 <1 取 1，条数越界收敛到 [1, maxPathLimit]）。
func normalizePathPaging(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = defaultPathLimit
	}
	if limit > maxPathLimit {
		limit = maxPathLimit
	}
	return page, limit
}
