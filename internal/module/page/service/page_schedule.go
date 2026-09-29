package pageservice

// page_schedule.go — 定时上下线（PIPE-7）：排定、取消、列表与到点执行的总编排。
//
// 两条拍板的决策（本文件的实现依据，改之前先读）：
//
//  1. **到点只切指针，绝不重新编译**。上线 = 把 active 目录里该语言的符号链接原子切到
//     **排定时冻结的暂存产物**（page_stagings），再落数据库。为什么不重新编译：
//     编译是「用现在的站点环境产出一份字节」的动作 —— 到点那一刻的语言清单、组件版本、
//     引用到的内容都可能与排定时不同，编译出的字节会与运营排定时看到的那一份不一样，
//     而「到点执行」的全部意义就是「把我当时确认过的那一份推上去」。副作用也顺带消失：
//     到点不再需要一次可能失败、可能耗时的构建，切指针是毫秒级的原子操作。
//     这条路径复用发布主链已经收敛好的零件（applyPublishActivation / 回执 / 恢复 / 收敛），
//     因此「手工发布」与「定时上线」在数据库与恢复协议上是同一件事，没有第二套机制。
//
//  2. **排定后草稿被改 → 硬失败**。到点时 pages.draft_version 与排定时记下的版本不一致，
//     就置 failed 并在后台可见（key 取 ErrRebuildRequired，与 publish() 的同名判据同源），
//     **不**按当前草稿重新编译。理由与上一条对偶：重新编译等于执行一条运营没有批准过的发布。
//
// 执行侧的三件套（照 build_jobs 的队列形状，审计 DB-01）：
//   - **认领**：claimDueSchedules 一条 SQL 内 SELECT + UPDATE + 发租约令牌，
//     互斥粒度 (page_id, lang, action)，由部分唯一索引在数据库层兜底；
//   - **完成归属**：MarkScheduleDone / MarkScheduleFailed 必须带租约令牌 ——
//     超时被回收并重新认领后，旧执行者的结果不能再落库；
//   - **回收**：ReclaimExpiredSchedules 把超时未结案的 running 行退回待执行（或判失败）。
//
// 调度器形状与 page_retention / page_publish_converge 一致（进程内 ticker + IsTestProcess 守卫），
// 见 page_schedule_scheduler.go。**不依赖 asynq**：queue.enabled 默认 false，
// 依赖队列会让未启用队列的部署静默不生效。

import (
	"context"
	"errors"
	"strings"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"

	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

const (
	// pageScheduleInterval 到点扫描的兜底间隔（每分钟一次）。
	//
	// 上限就是它：定时上线的精度是「分钟」而不是「秒」。比这更细要付出的是
	// 每次扫描一次数据库往返，而到点动作本身是毫秒级的 —— 分钟级抖动由运营接受
	// （排定界面显示的也是分钟）。
	pageScheduleInterval = time.Minute
	// pageScheduleLeaseTTL 单条排定的认领租约时长。
	//
	// 它同时决定「执行者崩溃后多久能被回收」—— 因此必须**大于**单次执行的最坏耗时
	// （切指针 + 一个事务 + sitemap 刷新，秒级），否则正常执行会被判成超时。
	pageScheduleLeaseTTL = 5 * time.Minute
	// pageScheduleScanTimeout 单轮扫描的总预算（认领 + 执行全部到期项）。
	pageScheduleScanTimeout = 5 * time.Minute
	// pageScheduleBatchSize 单批认领条数。
	pageScheduleBatchSize = 20
	// pageScheduleMaxRounds 单轮扫描最多认领几批（防止积压把一轮拉得比间隔还长）。
	pageScheduleMaxRounds = 5
	// pageScheduleMaxAttempts 允许的认领次数上限：达上限的排定判失败而不是无限重试。
	pageScheduleMaxAttempts = 3
	// pageScheduleListLimit 面板返回的排定条数上限。
	pageScheduleListLimit = 50
)

// SetPageSchedule 排定一次到点动作（上线或下线）。
//
// 上线排定**内含一次构建**：排定即冻结产物 —— 没有暂存产物就无从「只切指针」，
// 而且「构建成功」本身就是「这次排定可执行」的证明（文档非法、路径被占都会在这里报错，
// 而不是等到半夜到点才失败）。
//
// 时间口径：入参按**站点时区**解释，落库一律 UTC（列是 timestamptz）。
func (s *Service) SetPageSchedule(ctx context.Context, req *pagedto.ScheduleSetReq) (res *pagedto.ScheduleItem, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" {
		return nil, ErrInvalidParam
	}
	action := strings.TrimSpace(req.Action)
	switch action {
	case pagemodel.ScheduleActionPublish, pagemodel.ScheduleActionOffline:
	default:
		return nil, ErrScheduleActionInvalid
	}
	at, perr := sitetz.ParseDateTime(req.ScheduledAt)
	if perr != nil {
		// 解析失败只记日志：原文（含用户输入）不该回显到界面上，而 400 的归口文案
		// 已经说明了「参数不对」。过去的时刻另有专门判据（ErrScheduleInPast），
		// 两者对使用者是两件不同的事：一个改格式、一个改时间。
		logger.Scene("page").With("pageId", req.PageID).Error(perr, "排定时间无法解析")
		return nil, ErrInvalidParam
	}
	if !at.After(time.Now()) {
		return nil, ErrScheduleInPast
	}
	page, err := s.getExistingPage(ctx, req.PageID)
	if err != nil {
		return nil, err
	}
	lang := buildLang(req.Lang)
	upsert := pagemodel.ScheduleUpsert{
		PageID: page.ID, Lang: lang, Action: action,
		ScheduledAt: at.UTC(), DraftVersion: page.DraftVersion, CreateBy: req.CreateBy,
	}
	switch action {
	case pagemodel.ScheduleActionPublish:
		// 一次构建让暂存指针与草稿版本对齐。构建失败按原样返回（业务错误 → 400/409），
		// 不留半条排定 —— 排定与产物必须同时成立。
		if _, berr := s.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: lang}); berr != nil {
			return nil, berr
		}
		// getExistingPage 是构建**之前**的快照：构建会写暂存指针、可能推进 update_time，
		// 而 draft_version 才是排定要冻结的那个版本号，重新取一次避免拿到旧值。
		page, err = s.getExistingPage(ctx, page.ID)
		if err != nil {
			return nil, err
		}
		staging, serr := s.model.GetStaging(ctx, page.ID, lang)
		if serr != nil || staging == nil || strings.TrimSpace(staging.ArtifactID) == "" {
			return nil, ErrNoStagedArtifact
		}
		upsert.ArtifactID = staging.ArtifactID
		upsert.DraftVersion = page.DraftVersion
	default:
		// 下线不产生产物；跳转目标在这里就归一化（非法路径不该等到到点才发现）。
		if target := strings.TrimSpace(req.RedirectPath); target != "" {
			normalized, nerr := normalizePagePath(target)
			if nerr != nil {
				return nil, nerr
			}
			upsert.RedirectPath = normalized
		}
	}
	id, err := s.model.UpsertPendingSchedule(ctx, upsert)
	if err != nil {
		// 同键已有 **running** 的排定：唯一索引拒绝，映射成业务错误（pending 会被取代，不算冲突）。
		if database.IsUniqueViolation(err) {
			return nil, ErrScheduleOccupied
		}
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("lang", lang).With("action", action).
		With("scheduleId", id).With("scheduledAt", upsert.ScheduledAt.Format(time.RFC3339)).
		Info("已排定定时上下线")
	item := scheduleItemOf(pagemodel.ScheduleEntity{
		ID: id, PageID: page.ID, Lang: lang, Action: action,
		ScheduledAt: upsert.ScheduledAt, Status: pagemodel.ScheduleStatusPending,
		DraftVersion: upsert.DraftVersion,
		ArtifactID:   optionalStringPtr(upsert.ArtifactID),
		RedirectPath: optionalStringPtr(upsert.RedirectPath),
		CreateTime:   time.Now().UTC(), UpdateTime: time.Now().UTC(),
	})
	return &item, nil
}

// CancelPageSchedule 取消一条尚未执行的排定。
//
// 只取消 pending：running 的那条已经在执行（执行者手上拿着它的租约与快照），
// 此刻「取消」只会让界面显示一个与事实不符的状态；终态行不是「可取消的排定」，
// 按不存在处理（面板刷新后它会从「待执行」区消失）。
func (s *Service) CancelPageSchedule(ctx context.Context, req *pagedto.ScheduleCancelReq) (err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" || req.ID <= 0 {
		return ErrInvalidParam
	}
	status, err := s.model.ScheduleStatusAt(ctx, req.ID, req.PageID)
	if err != nil {
		return err
	}
	switch status {
	case "":
		return ErrScheduleNotFound
	case pagemodel.ScheduleStatusPending:
		// 继续往下取消
	case pagemodel.ScheduleStatusRunning:
		return ErrScheduleRunning
	default:
		// done / failed / canceled：没有可取消的排定。
		return ErrScheduleNotFound
	}
	canceled, err := s.model.CancelSchedule(ctx, req.ID, req.PageID)
	if err != nil {
		return err
	}
	if !canceled {
		// 读与写之间被认领了（并发）：这就是 ErrScheduleRunning 的成因，不是内部错误。
		return ErrScheduleRunning
	}
	logger.Scene("page").With("pageId", req.PageID).With("scheduleId", req.ID).Info("排定已取消")
	return nil
}

// ListPageSchedules 列出某页面的排定（新到旧，上限 pageScheduleListLimit）。
//
// LastErrorText 不在这里填：service 没有请求语言上下文，硬翻会把中英混排写进响应；
// 由读侧（handler）按 pageenums.ScheduleFailureFallbacks 取词。
func (s *Service) ListPageSchedules(ctx context.Context, req *pagedto.ScheduleListReq) (res *pagedto.ScheduleListResp, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" {
		return nil, ErrInvalidParam
	}
	limit := req.Limit
	if limit <= 0 || limit > pageScheduleListLimit {
		limit = pageScheduleListLimit
	}
	rows, err := s.model.ListSchedulesByPage(ctx, req.PageID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]pagedto.ScheduleItem, 0, len(rows))
	for i := range rows {
		items = append(items, scheduleItemOf(rows[i]))
	}
	return &pagedto.ScheduleListResp{PageID: req.PageID, Items: items}, nil
}

// scheduleSummaryStatuses 列表页徽标要看的三种状态（终态 done / canceled 不看）。
var scheduleSummaryStatuses = []string{
	pagemodel.ScheduleStatusPending,
	pagemodel.ScheduleStatusRunning,
	pagemodel.ScheduleStatusFailed,
}

// ListSchedulesForPages 批量取这批页面的排定投影（后台列表页的行内徽标）。
//
// 一次 IN 查询取回全部命中行再分组：列表页一屏几十行，逐页问一次会把一次页面渲染
// 变成几十次查询。没有排定（或只有终态完成记录）的页面不出现在结果里。
//
// pageIDs 为空返回空 map（不是 nil）：调用方直接 range，无需额外判空。
func (s *Service) ListSchedulesForPages(ctx context.Context, pageIDs []string) (res map[string]pagedto.SchedulePageSummary, err error) {
	res = map[string]pagedto.SchedulePageSummary{}
	if len(pageIDs) == 0 {
		return res, nil
	}
	rows, err := s.model.ListSchedulesByPages(ctx, pageIDs, scheduleSummaryStatuses, 0)
	if err != nil {
		return nil, err
	}
	// 行按 create_time DESC 返回：第一次遇到的就是每页最新的那一条，无需再比时间。
	for i := range rows {
		row := rows[i]
		summary, ok := res[row.PageID]
		if !ok {
			summary = pagedto.SchedulePageSummary{PageID: row.PageID}
		}
		item := scheduleItemOf(row)
		switch row.Status {
		case pagemodel.ScheduleStatusPending, pagemodel.ScheduleStatusRunning:
			if summary.Pending == nil {
				pending := item
				summary.Pending = &pending
			}
		case pagemodel.ScheduleStatusFailed:
			if summary.Failed == nil {
				failed := item
				summary.Failed = &failed
			}
		}
		res[row.PageID] = summary
	}
	return res, nil
}

// RunDueSchedules 执行一轮到点扫描（调度器的执行体，也可由运维手动触发）。//
// 三步：回收超时租约 → 分批认领到期项 → 逐条执行。
//
// **单条失败不中断整批**：一条排定的失败（草稿变了、路径被占）不该让同批的其它排定
// 一起不执行 —— 那是「一次配置错误把整晚的定时全部堵住」。每条的成败各自落库。
//
// 返回的统计供日志与手动触发查看；单条失败不改变返回的 err（err 只表示
// 「这一轮本身没跑起来」，例如认领语句失败）。
func (s *Service) RunDueSchedules(ctx context.Context) (res *pagedto.ScheduleRunResp, err error) {
	out := &pagedto.ScheduleRunResp{}
	reclaimed, rerr := s.model.ReclaimExpiredSchedules(ctx, pageScheduleMaxAttempts, pageenums.ErrScheduleApplyFailed)
	if rerr != nil {
		// 回收失败不阻断本轮：到期项照常执行，下轮再收（回收本身幂等）。
		logger.Scene("page").Error(rerr, "回收超时的排定租约失败")
	} else {
		out.Reclaimed = reclaimed
	}
	for round := 0; round < pageScheduleMaxRounds; round++ {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		rows, cerr := s.model.ClaimDueSchedules(ctx, pageScheduleLeaseTTL, pageScheduleBatchSize)
		if cerr != nil {
			return out, cerr
		}
		if len(rows) == 0 {
			break
		}
		out.Claimed += len(rows)
		for i := range rows {
			s.applyOneSchedule(ctx, rows[i], out)
		}
		if len(rows) < pageScheduleBatchSize {
			break
		}
	}
	if out.Claimed > 0 || out.Reclaimed > 0 {
		logger.Scene("page").With("claimed", out.Claimed).With("applied", out.Applied).
			With("failed", out.Failed).With("retried", out.Retried).With("reclaimed", out.Reclaimed).
			Info("定时上下线扫描完成")
	}
	return out, nil
}

// applyOneSchedule 执行一条已认领的排定并把结果按**完成归属**写回。
//
// 三种收尾，判据只有一条：这个失败「下一轮再试有没有意义」。
//   - 成功 → done；
//   - 不可重试（草稿变了、无暂存产物、路径被占、页面已删……）或已试满次数 → failed；
//   - 可重试（数据库 / 文件系统的瞬时故障）→ 退回 pending，下一轮再认领。
//
// 租约丢失（ErrScheduleLeaseLost）一律只记日志：本次动作可能已经生效
// （切指针是原子的、重放幂等），但结果不落库 —— 新的认领者会重放同一动作并收敛，
// 这与发布回执的「状态不可判定就留给恢复」同一形态。
func (s *Service) applyOneSchedule(ctx context.Context, e pagemodel.ScheduleEntity, out *pagedto.ScheduleRunResp) {
	token := ""
	if e.LeaseToken != nil {
		token = *e.LeaseToken
	}
	now := time.Now().UTC()
	aerr := s.applyScheduleAction(ctx, e)
	if aerr == nil {
		if derr := s.model.MarkScheduleDone(ctx, e.ID, token, now); derr != nil {
			logger.Scene("page").With("scheduleId", e.ID).With("pageId", e.PageID).
				Error(derr, "排定执行成功但结案失败（结果不落库，将由下一次认领重放收敛）")
			return
		}
		out.Applied++
		return
	}
	key := scheduleFailureKey(aerr)
	terminal := errors.Is(aerr, errScheduleTerminal)
	if terminal || e.Attempts >= pageScheduleMaxAttempts {
		if ferr := s.model.MarkScheduleFailed(ctx, e.ID, token, key, now); ferr != nil {
			logger.Scene("page").With("scheduleId", e.ID).With("pageId", e.PageID).
				Error(ferr, "排定失败状态写入失败（结果不落库）")
			return
		}
		out.Failed++
		// 原文只进日志：last_error 落的是可翻译的业务 key（会显示在后台）。
		logger.Scene("page").With("scheduleId", e.ID).With("pageId", e.PageID).
			With("action", e.Action).With("lang", e.Lang).With("attempts", e.Attempts).
			With("key", key).Error(aerr, "排定到点执行失败")
		return
	}
	if rerr := s.model.ReleaseScheduleForRetry(ctx, e.ID, token, key, now); rerr != nil {
		logger.Scene("page").With("scheduleId", e.ID).Error(rerr, "排定退回待执行失败（结果不落库）")
		return
	}
	out.Retried++
	logger.Scene("page").With("scheduleId", e.ID).With("pageId", e.PageID).
		With("attempts", e.Attempts).With("key", key).Warn("排定执行遇到可重试故障，已退回待执行")
}

// scheduleFailureKey 把执行失败映射成**可翻译的业务 key**（落进 last_error 的那一列）。
//
// 判据是「这条失败对运营意味着什么」：产物不再代表当前草稿、没有暂存产物、路径被占、
// 页面已删各有各的处置动作；识别不出来的归口到 ErrScheduleApplyFailed（原文只进日志）。
func scheduleFailureKey(err error) string {
	switch {
	case errors.Is(err, ErrRebuildRequired):
		return pageenums.ErrRebuildRequired
	case errors.Is(err, ErrNoStagedArtifact):
		return pageenums.ErrNoStagedArtifact
	case errors.Is(err, ErrPathOccupied):
		return pageenums.ErrPathOccupied
	case errors.Is(err, ErrPageNotFound):
		return pageenums.ErrPageNotFound
	default:
		return pageenums.ErrScheduleApplyFailed
	}
}

// scheduleItemOf 把表行投影成对外的排定条目。
func scheduleItemOf(e pagemodel.ScheduleEntity) pagedto.ScheduleItem {
	item := pagedto.ScheduleItem{
		ID: e.ID, PageID: e.PageID, Lang: e.Lang, Action: e.Action, Status: e.Status,
		Attempts:         e.Attempts,
		ScheduledAt:      utils.NewJSONTime(e.ScheduledAt.UTC()),
		ScheduledAtLocal: sitetz.FormatDateTime(e.ScheduledAt),
		DraftVersion:     e.DraftVersion,
		CreateTime:       utils.NewJSONTime(e.CreateTime.UTC()),
		UpdateTime:       utils.NewJSONTime(e.UpdateTime.UTC()),
	}
	if e.ArtifactID != nil {
		item.ArtifactID = *e.ArtifactID
	}
	if e.RedirectPath != nil {
		item.RedirectPath = *e.RedirectPath
	}
	if e.LastError != nil {
		item.LastError = *e.LastError
	}
	return item
}

// optionalStringPtr 空串转 nil（表里「没有这个值」用 NULL 表达，空串会在
// 「排定是否带跳转」这类判据上被误判成有值）。
func optionalStringPtr(raw string) *string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
