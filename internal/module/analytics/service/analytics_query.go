package analyticsservice

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	analyticsdto "go_wp/internal/module/analytics/dto"
	analyticsenums "go_wp/internal/module/analytics/enums"
	analyticsmodel "go_wp/internal/module/analytics/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	defaultRangeDays = 30

	maxRangeDays = 366

	defaultPathLimit = 20
	maxPathLimit     = 200

	defaultRankLimit = 20
	maxRankLimit     = 200
)

const dateLayout = "2006-01-02"

const granularityHour = "hour"

const hourLayout = "2006-01-02T15:00"

func (s *Service) Summary(ctx context.Context, req *analyticsdto.SummaryReq) (res *analyticsdto.SummaryResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return nil, ErrInvalidParam
	}
	// 形状也必须在进 SQL 之前判掉：project_id 是 uuid 列，非法串下发给 PG 会在类型解析处
	// 报错，被出口当成内部错误（500）。调用方传错了参数是 400，不是我们这边出错。
	// 与 collect 的 buildEntity 用同一判法（那边是 skip 语义，这里返回错误）。
	if _, perr := uuid.Parse(projectID); perr != nil {
		return nil, ErrInvalidParam
	}
	from, to, rerr := s.normalizeWindow(req.From, req.To)
	if rerr != nil {
		return nil, rerr
	}
	page, limit := normalizePathPaging(req.PathPage, req.PathLimit)
	rankLimit := normalizeRankLimit(req.RankLimit)

	var (
		views, visitors int64
		dayRows         []analyticsmodel.DayRow
		pathTotal       int64
		pathRows        []analyticsmodel.PathRow
		source          string
	)

	windowDays := int(to.Sub(from) / (24 * time.Hour))
	rolledDays, cerr := s.m.CountRolledDays(ctx, projectID, from, to)
	if cerr != nil {
		return nil, cerr
	}

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

	var nextViews int64
	var nextPath string
	if n := len(paths); n > 0 {
		nextViews, nextPath = paths[n-1].Views, paths[n-1].Path
	}
	return &analyticsdto.SummaryResp{
		ProjectID: projectID,
		From:      from.Format(dateLayout),

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

		BreakdownSource: analyticsdto.SourceDetail,

		Source:             source,
		PathNextAfterViews: nextViews,
		PathNextAfter:      nextPath,
	}, nil
}

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

func dayStart(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func toRankCounts(rows []analyticsmodel.DimensionRow) []analyticsdto.RankCount {
	out := make([]analyticsdto.RankCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, analyticsdto.RankCount{Value: r.Value, Views: r.Views, Visitors: r.Visitors})
	}
	return out
}

func normalizeRankLimit(limit int) int {
	if limit < 1 {
		return defaultRankLimit
	}
	if limit > maxRankLimit {
		return maxRankLimit
	}
	return limit
}

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
	rollupInterval = time.Hour

	rollupBackfillMaxDays = 400
)

func (s *Service) RollupRecent(ctx context.Context) (projects int, err error) {
	if s == nil || s.m == nil {
		return 0, nil
	}

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

func (s *Service) rollupOne(ctx context.Context, projectID string, day, from, to time.Time) error {
	if err := s.m.RollupDay(ctx, projectID, day, from, to); err != nil {
		logger.Scene("analytics").With("project_id", projectID).With("day", day.Format(dateLayout)).
			Error(err, "访问统计汇总失败")
		return err
	}
	return nil
}

func StartAnalyticsRollupScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
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

func (s *Service) retentionPolicies(ctx context.Context) ([]projectcontract.RetentionPolicyResp, error) {
	if s == nil || s.retention == nil {
		return nil, analyticsenums.Err(analyticsenums.ErrInvalidParam)
	}
	return s.retention.ListRetentionPolicies(ctx)
}

const analyticsRetentionInterval = 24 * time.Hour

func StartAnalyticsRetentionScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
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
