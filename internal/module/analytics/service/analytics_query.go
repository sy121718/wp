package analyticsservice

// 明细表（page_views）只增不减，而统计查询几乎总是查过去若干天。
// 那些天一旦过去就不会再变，却每次都被重新 GROUP BY 一遍。
// 本文件把「已经落定的天」提前算好写进 page_views_daily，
// 让历史窗口的查询成本与明细行数脱钩。
//
// 两条刻意的取舍：
//   - **重算而不是增量**：每次都从明细全量重算当天，靠 ON CONFLICT 覆盖。
//     增量累加一旦漏算就永久留下偏差（且无法与明细对账），重算不会。
//   - **今天也汇总，但查询仍走明细**：汇总每小时跑一次，今天那一行是快照，
//     用它回答「刚才那篇文章有没有人看」必然滞后；查询侧因此只在
//     「窗口完全落在今天之前」时才读汇总（见 analytics_query.go）。

import (
	"context"
	"errors"
	"strings"
	"time"

	"go_wp/internal/module/analytics/dto"
	"go_wp/internal/module/analytics/enums"
	"go_wp/internal/module/analytics/model"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
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
	// defaultRankLimit / maxRankLimit 维度排行（来源域 / 设备分类 / 语言）的条数。
	//
	// 与路径排行的 20/200 同值但**刻意分成两组常量**：两者将来收紧的理由不一样 ——
	// 路径的取值域随站点规模无上限增长，而这三组的取值域是收敛的，
	// 共用一组数字会让「调路径分页」顺带改掉维度排行的形状。
	defaultRankLimit = 20
	maxRankLimit     = 200
)

// dateLayout 请求与响应里的日期格式（后台表单与 JSON 同一格式）。
const dateLayout = "2006-01-02"

// granularityHour 小时粒度的取值（SummaryReq.Granularity）。
//
// 与 utils.LayoutHour 同形但不是同一个东西：那个是**桶键的格式**，这个是**请求里的档位名**。
// 档位名是接口契约（前端与 URL 都用它），格式是展示细节，两者将来都可能各自变。
const granularityHour = "hour"

// hourLayout 小时桶键的格式，与 utils.LayoutHour 保持一致（此处不复用常量是为了
// 让 analytics 不为了一个字符串去依赖 utils —— 它的按天口径同样是本地字面量）。
const hourLayout = "2006-01-02T15:00"

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
	rankLimit := normalizeRankLimit(req.RankLimit)

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
	// 小时粒度**必须走明细**：预聚合表 page_views_daily 的最小粒度就是天，
	// 小时在它里面没有对应的行。用汇总行去凑小时桶会得到「每天 24 个空桶 +
	// 一个总量塞在某一个小时」，形状全错而数字看着还对。
	useHour := req.Granularity == granularityHour
	if !useHour && !to.After(dayStart(s.now())) && windowDays > 0 && int64(windowDays) == rolledDays {
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
		if useHour {
			dayRows, err = s.m.CountByHour(ctx, projectID, from, to)
		} else {
			dayRows, err = s.m.CountByDay(ctx, projectID, from, to)
		}
		if err != nil {
			return nil, err
		}
		if pathTotal, err = s.m.CountPathTotal(ctx, projectID, from, to); err != nil {
			return nil, err
		}
		if pathRows, err = s.m.CountByPath(ctx, projectID, from, to, (page-1)*limit, limit); err != nil {
			return nil, err
		}
	}

	// 来源域 / 设备分类 / 语言的排行。
	//
	// **形态选择：恒定读明细，不为这三个维度引入新的预聚合 scope。** 三条理由：
	//
	//  1. 加 scope 要动 DDL。scope 的取值受迁移 170 的 CHECK 约束（scope IN ('all','path')），
	//     而预聚合表只有 (project_id, day, scope, path) 这一个形状 —— 新维度只能把取值
	//     塞进第 4 列 path，让同一列同时表示「路径 / 来源域 / UA 分类 / 语言码」四种语义。
	//     那不是命名问题：表里所有既有的按 path 过滤（RollupByPath / RollupPathTotal /
	//     清理重算里的 NOT EXISTS）都因此要多带一个 scope 谓词，将来漏一个就会把
	//     来源域当路径读出来，而且是静默的。
	//  2. 正确性上不需要汇总。预聚合是「对已落定的天全量重算」（rollup 的幂等设计，
	//     有 TestRollupMatchesDetail 逐项对账），所以**过去窗口**下走明细与走汇总
	//     算出来的是同一个数字 —— 两者的差异只在成本，不在口径。
	//  3. 成本可控。维度排行的形状是「单列 GROUP BY + 排序 + LIMIT Top-N」，
	//     不做深分页（取值域天然收敛：ua_class 受 CHECK 约束只有 5 种可能值、
	//     语言码十几、来源域远小于路径数）。它与路径排行在**明细分支**里已经在做
	//     的事同量级（CountByPath 同样是全窗口扫描），不是新的复杂度来源；
	//     而汇总任务本身每小时就对整个窗口做两遍全量聚合。
	//
	// 「窗口含今天必须走明细」这条既有约束在这里的含义：它只决定 Total/Daily/Paths
	// 从哪张表取数，不约束维度排行 —— 维度恒定走明细，所以不存在
	// 「总数来自明细、来源域来自一小时前的快照」这种同一响应内的口径分裂。
	// 代价是三次扫描同一窗口；合并成 GROUPING SETS 能省两次扫描，
	// 但会把「三个各自独立的榜」变成「一次扫描按分组标签拆行」，不值得。
	breakdown := make(map[string][]analyticsmodel.DimensionRow, 3)
	for _, dim := range []string{
		analyticsmodel.DimensionReferrer,
		analyticsmodel.DimensionUA,
		analyticsmodel.DimensionLang,
	} {
		rows, rerr := s.m.CountByDimension(ctx, projectID, from, to, dim, rankLimit)
		if rerr != nil {
			return nil, rerr
		}
		breakdown[dim] = rows
	}

	daily := make([]analyticsdto.DailyCount, 0, len(dayRows))
	layout := dateLayout
	if useHour {
		layout = hourLayout
	}
	for _, row := range dayRows {
		daily = append(daily, analyticsdto.DailyCount{
			Day:      row.Day.UTC().Format(layout),
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

		Referrers: toRankCounts(breakdown[analyticsmodel.DimensionReferrer]),
		UAClasses: toRankCounts(breakdown[analyticsmodel.DimensionUA]),
		Langs:     toRankCounts(breakdown[analyticsmodel.DimensionLang]),
		RankLimit: rankLimit,
		// 恒定明细：这三个榜不读预聚合（见上方形态选择）。
		BreakdownSource: analyticsdto.SourceDetail,

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

// toRankCounts 把维度聚合行转成对外形状（空结果给空切片而不是 nil：
// 响应里的 [] 与 null 是两种不同的信号，前者是「这个维度没有数据」）。
func toRankCounts(rows []analyticsmodel.DimensionRow) []analyticsdto.RankCount {
	out := make([]analyticsdto.RankCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, analyticsdto.RankCount{Value: r.Value, Views: r.Views, Visitors: r.Visitors})
	}
	return out
}

// normalizeRankLimit 归一化维度排行的条数（<1 取默认，越界收敛到上限）。
func normalizeRankLimit(limit int) int {
	if limit < 1 {
		return defaultRankLimit
	}
	if limit > maxRankLimit {
		return maxRankLimit
	}
	return limit
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

const (
	// rollupInterval 汇总间隔。
	// 小时级足够：汇总只服务「已经过去的天」，当天的实时性由明细表保证；
	// 跑得再勤也只是把同一批不可变数据重算一遍。
	rollupInterval = time.Hour
	// rollupBackfillMaxDays 单次运行最多补齐的天数。
	//
	// 首次汇总时历史可能很长（明细保留期默认 90 天），一次补齐可接受；
	// 设上限是为了兜住「保留期被调得很长」的极端配置，让单次任务时间有界，
	// 剩下的下次接着补（水位是持久化的，不会漏）。
	rollupBackfillMaxDays = 400
)

// RollupRecent 补齐历史水位并刷新最近两天，返回处理的工程数。
//
// 两件事按顺序做：
//
//  1. **补齐**：从「最后已汇总日的次日」（从未汇总过则从最早明细日）逐日算到昨天。
//     没有这一步，历史窗口读汇总会读到空 —— 数字比明细少，比查得慢严重得多。
//  2. **刷新**：无条件重算今天与昨天。今天那行是当前快照（查询侧并不使用它，
//     但保留它让「今天已汇总」可判定，运维也能从 rolled_at 看出任务在跑）；
//     昨天那行是兜住跨日边界 —— 临近零点写入的访问可能落在昨天，
//     而昨天是在它还是今天时汇总的。
//
// 幂等：同一天被重算多少次，结果都等于从明细重新算一遍的值。
// 单个工程失败只记日志并继续 —— 一个工程的异常不该让其它工程的汇总停摆。
func (s *Service) RollupRecent(ctx context.Context) (projects int, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	// 候选工程清单经 project 契约取，再交给 model 逐工程在作用域内探测明细 ——
	// 「有哪些工程」的真源在 project 模块，本模块的 model 只查 page_views（自己那张表）。
	candidates, err := s.projectIDs(ctx)
	if err != nil {
		return 0, err
	}
	ids, err := s.m.ListProjectsWithViews(ctx, candidates)
	if err != nil {
		return 0, err
	}
	today := dayStart(s.now())
	yesterday := today.AddDate(0, 0, -1)
	// lastErr 只用于把失败原因交回调用方（部分失败不影响其它工程继续跑）：
	// 单日失败已经逐条记了日志，但调度器看不到；返回给调用方才有机会被发现。
	var lastErr error
	for _, id := range ids {
		start, ok, gerr := s.rollupStart(ctx, id)
		if gerr != nil {
			logger.Scene("analytics").With("project_id", id).Error(gerr, "读取汇总水位失败")
			continue
		}
		days := 0
		for d := start; ok && !d.After(yesterday) && days < rollupBackfillMaxDays; d = d.AddDate(0, 0, 1) {
			if rerr := s.rollupOne(ctx, id, d, d, d.AddDate(0, 0, 1)); rerr != nil {
				lastErr = rerr
				break
			}
			days++
		}
		if days >= rollupBackfillMaxDays {
			logger.Scene("analytics").With("project_id", id).With("days", days).
				Warn("访问统计补齐达到单次上限，剩余天数由下次任务继续")
		}
		if rerr := s.rollupOne(ctx, id, yesterday, yesterday, today); rerr != nil {
			lastErr = rerr
		}
		if rerr := s.rollupOne(ctx, id, today, today, today.AddDate(0, 0, 1)); rerr != nil {
			lastErr = rerr
		}
	}
	return len(ids), lastErr
}

// rollupStart 计算补齐的起点：已有水位则从次日继续，否则从最早明细日重头补。
func (s *Service) rollupStart(ctx context.Context, projectID string) (start time.Time, ok bool, err error) {
	last, has, err := s.m.LastRolledDay(ctx, projectID)
	if err != nil {
		return time.Time{}, false, err
	}
	if has {
		return last.AddDate(0, 0, 1), true, nil
	}
	earliest, hasEarliest, err := s.m.EarliestViewDay(ctx, projectID)
	if err != nil {
		return time.Time{}, false, err
	}
	if !hasEarliest {
		return time.Time{}, false, nil
	}
	return earliest, true, nil
}

// rollupOne 汇总一天：失败记日志并返回错误（补齐循环据此停在该天，下次继续）。
func (s *Service) rollupOne(ctx context.Context, projectID string, day, from, to time.Time) error {
	if err := s.m.RollupDay(ctx, projectID, day, from, to); err != nil {
		logger.Scene("analytics").With("project_id", projectID).With("day", day.Format(dateLayout)).
			Error(err, "访问统计汇总失败")
		return err
	}
	return nil
}

// StartAnalyticsRollupScheduler 启动每小时汇总（先跑一次再等间隔，与其它调度同形）。
func StartAnalyticsRollupScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_, _ = svc.RollupRecent(ctx)
		}
		run()
		ticker := time.NewTicker(rollupInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}

// PurgeExpiredViews 删除超过工程保留期的 page_views 明细（IDX-001）。
func (s *Service) PurgeExpiredViews(ctx context.Context) error {
	if s == nil || s.m == nil {
		return nil
	}
	policies, err := s.retentionPolicies(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, p := range policies {
		if p.RetentionDays <= 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -p.RetentionDays)
		n, derr := s.m.DeleteViewsBefore(ctx, p.ProjectID, cutoff)
		if derr != nil {
			logger.Scene("analytics").With("project_id", p.ProjectID).Error(derr, "清理过期访问明细失败")
			continue
		}
		if n > 0 {
			logger.Scene("analytics").With("project_id", p.ProjectID).With("deleted", n).Info("已清理过期访问明细")
		}
	}
	return nil
}

// retentionPolicies 工程的访问明细保留策略。
//
// 经 project 契约取：保留期那一列（`projects.analytics_retention_days`）长在 project 模块的
// 表上，读它的方法就该住在那个模块里（`projectmodel.ListRetentionPolicies`）—— 本模块原来
// 直接 `SELECT ... FROM projects WHERE analytics_retention_days > 0`，是越界读表。
func (s *Service) retentionPolicies(ctx context.Context) ([]projectcontract.RetentionPolicyResp, error) {
	if s == nil || s.retention == nil {
		return nil, errors.New(analyticsenums.ErrInvalidParam)
	}
	return s.retention.ListRetentionPolicies(ctx)
}

const analyticsRetentionInterval = 24 * time.Hour

// StartAnalyticsRetentionScheduler 每日清理过期访问明细（IDX-001）。
func StartAnalyticsRetentionScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发（见 utils.IsTestProcess）。
	}
	if svc == nil {
		return
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			_ = svc.PurgeExpiredViews(ctx)
		}
		run()
		ticker := time.NewTicker(analyticsRetentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
