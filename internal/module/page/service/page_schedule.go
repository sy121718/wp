package pageservice

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

// 与 page_publish.go 的关系：定时上线**不重新编译**，因此不走 publish() 主链；
// 但 FS 切换之后的数据库那几步与主链**共用同一份实现**（applyPublishActivation /
// 回执账本 / deactivatePaths），于是「手工发布」与「定时上线」在访问面、数据库与
// 启动恢复协议上是同一件事。这一点是有意的：另写一套补齐逻辑，两边迟早分叉，
// 而分叉的表现是「崩溃恢复后状态看着收敛了、但与正常发布的结果不同」。
//
// 崩溃窗口与发布同源：切换访问面（符号链接）与数据库写入之间不可原子。
// 因此这里同样「先登记 pending 回执 → 再切 → 再写库」：
//   · 切换前登记（AR2-002 / TX-009）：否则崩溃窗口里查不到「这次上线发生过」；
//   · 切换后任何失败都保留 pending，交给启动恢复（page_publish_recover.go）与
//     每分钟收敛（page_publish_converge.go）按链接实际指向补齐 —— 两者都不区分
//     「手工发布」还是「定时上线」，因为它们只看回执的 action 与链接的指向。

// 形状与 page_retention.go / page_publish_converge.go 一致：进程内 goroutine + ticker，
// 先跑一次再等间隔。启动点在同模块的路由装配处（page_router.go），与那两个调度器并排 ——
// 模块自己的调度器在模块内启动，不经由任何外部注册表。
//
// 为什么不是 asynq / 外部队列：queue.enabled 与 run_worker 的默认值是 false
// （config.yaml.example），把「到点上线」挂在队列上会让**未启用队列的部署静默不生效** ——
// 排定照常写进数据库、到点什么都不发生，而界面上一切正常。进程内 ticker 是唯一
// 不依赖部署开关的形态；队列将来只能作可选加速层，且必须有 ticker 兜底。
//
// 多实例安全性：重复扫描是安全的（认领是一条带 FOR UPDATE SKIP LOCKED 的原子语句，
// 同一瞬间只有一个实例领到同一条排定）；重复执行也是安全的（切指针幂等、
// 数据库步骤幂等、DS 步骤都能重放）。因此这里**不做**跨实例的选主。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
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

// errScheduleTerminal 执行失败的分类标记：「这一轮不成功、而且下一轮也没有意义」。
//
// 与「可重试失败」的区别必须显式表达，因为两者的收尾动作相反（failed vs 退回 pending）：
// 草稿变了、产物没了、路径被占都属于「要人去处理」，反复重试只会把日志刷满，
// 而数据库抖动、文件系统瞬时错误下一轮就自愈。
//
// 用 fmt.Errorf("%w: %w", errScheduleTerminal, cause) 双重包装：外层标记类别、
// 内层仍是具体的业务 sentinel（ErrRebuildRequired 等），scheduleFailureKey 才认得出它。
var errScheduleTerminal = errors.New("排定动作不可重试失败")

// errScheduleUnavailable 访问面组件未装配。
//
// 它是**进程级配置问题**（降级装配 / 精简部署），不是「下次再试就好」的瞬时故障：
// 因此按不可重试处理。对外只经 scheduleFailureKey 归口成 ErrScheduleApplyFailed，
// 原文只进日志。
var errScheduleUnavailable = errors.New("访问面组件未装配，无法执行到点动作")

// terminalScheduleError 把一条具体失败标记成不可重试。
func terminalScheduleError(err error) error {
	return fmt.Errorf("%w: %w", errScheduleTerminal, err)
}

// applyScheduleAction 按动作分派到点执行。
func (s *Service) applyScheduleAction(ctx context.Context, e pagemodel.ScheduleEntity) error {
	if e.Action == pagemodel.ScheduleActionOffline {
		return s.applyScheduleOffline(ctx, e)
	}
	return s.applySchedulePublish(ctx, e)
}

// applySchedulePublish 到点上线：把该语言的符号链接切到**排定时冻结的暂存产物**，再落数据库。
//
// 步骤与各自的理由：
//
//  1. 取暂存指针（page_stagings 为该语言的真源）—— 排定时冻结的产物就是它指向的那一份；
//  2. 校验产物文件在位且 CanonicalPath 与当前站点路径一致。文件不在（被误删 / 存储故障）
//     属于不可重试；CanonicalPath 不符说明这份产物是按**另一个访问路径**编译的
//     （排定后改过 URL），激活它等于把一个指向别处的页面推上线，必须拒绝；
//  3. 前置草稿版本比对（决策 2）：pages.draft_version 与排定时记下的版本不等即「排定后
//     草稿被改过」，置 failed 并**不**按当前草稿重新编译。这里同时要求暂存行的草稿版本
//     也等于当前版本 —— 两者一起构成「这份产物确实代表当前草稿」；
//  4. ensureRouteNotOccupied 预检（H7）：目标路径被别的页面/实例占用时提前失败，
//     避免「FS 已覆盖成本页产物、DB 路由写入才报错」的状态分裂；
//  5. 幂等直通：链接已指向本次产物时跳过 FS 切换（重复执行、或崩溃后重放）。
//     注意仍要走完回执与数据库步骤 —— 那正是「访问面已切、数据库没跟上」要补的一半；
//  6. 登记回执 → 切指针 → 数据库事务 → 解除旧路径的访问面链接 → 结案；
//  7. 站点级产物（sitemap / robots / feed / 404）与 IndexNow：尽力而为，失败只记日志，
//     不回滚已经生效的上线（与 publish() 主链同一口径）。
func (s *Service) applySchedulePublish(ctx context.Context, e pagemodel.ScheduleEntity) error {
	if s.publication == nil || s.store == nil || s.routes == nil {
		// 降级装配（访问面未接）：这是一条永远不会成功的排定，不该无限重试。
		return terminalScheduleError(errScheduleUnavailable)
	}
	page, err := s.locatePageInProjects(ctx, e.PageID)
	if err != nil {
		return terminalScheduleError(ErrPageNotFound)
	}
	lang := buildLang(e.Lang)
	// 发布计划与访问路径的算法**与 publish() 主链逐字相同**（审计 I18N-01）：
	// 计划决定默认语言，默认语言决定「这个语言有没有前缀」，前缀决定访问路径 ——
	// 路径又必须与产物里的 canonicalPath 一致（第 2 步的校验就是它）。
	// 这里若另用一套算法（例如现场解析默认语言），改过默认语言的站点会算出另一个路径，
	// 于是「产物是对的、路径判据却把它判成错的」。
	plan, _, perr := s.publicationPlanFor(ctx, page, lang)
	if perr != nil {
		return perr // 语言表读不到：可重试（下一轮同一步骤再读）
	}
	path, perr := s.sitePathOfWithPlan(ctx, lang, page, &plan)
	if perr != nil {
		return terminalScheduleError(perr)
	}
	staging, serr := s.model.GetStaging(ctx, page.ID, lang)
	if serr != nil && !errors.Is(serr, gorm.ErrRecordNotFound) {
		return serr // 读库失败：可重试
	}
	if staging == nil || strings.TrimSpace(staging.ArtifactHash) == "" {
		return terminalScheduleError(ErrNoStagedArtifact)
	}
	loc := pipeline.ArtifactLocator(staging.ArtifactHash)
	art, aerr := s.store.GetArtifact(loc)
	if aerr != nil || art == nil {
		// 产物文件丢失不属于「重试能好」：要人去重建（POST /api/page/artifact/rebuild）。
		return terminalScheduleError(fmt.Errorf("%w: %v", ErrNoStagedArtifact, aerr))
	}
	if art.CanonicalPath != path {
		// 这份产物是按另一个路径编译的（排定后改过 URL）。激活它会让 /new-path 出旧路径的内容。
		return terminalScheduleError(ErrRebuildRequired)
	}
	if staging.DraftVersion != page.DraftVersion || e.DraftVersion != page.DraftVersion {
		logger.Scene("page").With("scheduleId", e.ID).With("pageId", page.ID).With("lang", lang).
			With("scheduleDraftVersion", e.DraftVersion).With("stagingDraftVersion", staging.DraftVersion).
			With("pageDraftVersion", page.DraftVersion).
			Warn("排定到点时草稿已变更，跳过上线（不按当前草稿重新编译）")
		return terminalScheduleError(ErrRebuildRequired)
	}
	if oerr := s.ensureRouteNotOccupied(ctx, page.ProjectID, path, page.ID); oerr != nil {
		return terminalScheduleError(oerr)
	}
	// 回执与旧路径快照都必须在**切换之前**读：
	// 切换后 page_publications 会指向本次路径，那时旧路径既无法取消激活，
	// 也无法写进回执交给恢复处置（publish() 主链同一处论证）。
	fromArtifactID := s.publishedArtifactIDOf(ctx, page, lang)
	oldPath, perr2 := s.publishedPathOf(ctx, page, lang)
	if perr2 != nil {
		return perr2
	}
	alreadyActive := s.activeArtifactHashAt(path) == staging.ArtifactHash
	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionSwitchActive,
		ProjectID: page.ProjectID, PageID: page.ID, Path: path, Lang: lang,
		FromArtifactID: fromArtifactID, ToArtifactID: staging.ArtifactID, OldPath: oldPath,
	})
	if rerr != nil {
		return rerr // 登记失败一律中止（带着未知状态去切访问面正是这条回执要消灭的）
	}
	if !alreadyActive {
		if actErr := s.publication.Activate(path, loc); actErr != nil {
			// 激活失败线上保持不变（pipeline.LocalPublicationStore.Activate 同一分支）：
			// 属于可判定的无副作用失败，显式结案为已回滚。
			s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
			return actErr
		}
	}
	// 访问面已切换。此后任何失败都不能判定为「没生效」，一律保留 pending 交收敛判定。
	if aerr2 := s.applyPublishActivation(ctx, publishActivationInput{
		Page: page, Lang: lang, Path: path,
		ArtifactID: staging.ArtifactID, ArtifactHash: staging.ArtifactHash, OldPath: oldPath,
	}); aerr2 != nil {
		s.keepPublishReceiptPending(receiptID, "DB 激活状态写入失败")
		return aerr2
	}
	if oldPath != "" && oldPath != path {
		if derr := s.deactivatePaths([]string{oldPath}); derr != nil {
			s.keepPublishReceiptPending(receiptID, "解除旧路径访问面激活失败")
			return derr
		}
	}
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		// 两边都已就位，回执留 pending 只会被收敛例程幂等收尾。
		logger.Scene("page").With("receiptId", receiptID).With("pageId", page.ID).
			Error(cerr, "定时上线回执结案失败（状态已一致，收敛例程会幂等收尾）")
		s.NotifyPendingReceipt()
	}
	logger.Scene("page").With("scheduleId", e.ID).With("pageId", page.ID).With("lang", lang).
		With("path", path).With("hash", staging.ArtifactHash).With("alreadyActive", alreadyActive).
		Info("定时上线完成（只切指针，未重新编译）")
	s.refreshSiteFilesAfterSchedule(ctx, page)
	s.notifyIndexNow(ctx, page.ProjectID, path)
	return nil
}

// applyScheduleOffline 到点下线：先动访问面，再在一个事务里落数据库。
//
// 顺序与 page_locale_retire.go 同源，且理由相同：待清理的路径是从 page_publications
// 反查出来的，**指针一旦提交删除就再也没有入口能算出该清哪些链接**。反过来
// （先清数据库、再删链接）失败时留下的是「后台显示已下线、线上还在服务旧内容」，
// 而没有任何入口能发现或修复它。
//
// 两种形态：
//   - 有 redirect_path：旧路径落一条 301 产物并激活（页面在新地址继续可用，
//     旧链接不 404），数据库侧把路由占用改记为 redirect；
//   - 无 redirect_path：直接删符号链接（访问面 404），数据库侧解除路由占用
//     （只删 active 行 —— 保留 reserved 草稿占用，否则别的页面能抢占这个路径）。
//
// 两种形态都要清该语言的 page_publications 行与 pages 的单值镜像
// （ClearPublicationLangTx）——只删路由不清发布指针，后台会一直显示「已发布」。
//
// 幂等：路径为空（该语言从未上线或上次已下线）直接结案；删链接、取消占用、清指针
// 都可以重放。因此「FS 已下线、事务失败」是可重试收敛的，方向永远是 fail closed。
func (s *Service) applyScheduleOffline(ctx context.Context, e pagemodel.ScheduleEntity) error {
	if s.publication == nil {
		return terminalScheduleError(errScheduleUnavailable)
	}
	page, err := s.locatePageInProjects(ctx, e.PageID)
	if err != nil {
		return terminalScheduleError(ErrPageNotFound)
	}
	lang := buildLang(e.Lang)
	path, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return perr
	}
	if strings.TrimSpace(path) == "" {
		// 该语言没有激活记录：没有链接可删、也没有占用可释放。这是「重复执行」或
		// 「页面从未上线」的正常形态，不是失败。
		logger.Scene("page").With("scheduleId", e.ID).With("pageId", page.ID).With("lang", lang).
			Info("定时下线：该语言没有激活记录，无需处置")
		return nil
	}
	redirectPath := ""
	if e.RedirectPath != nil {
		redirectPath = strings.TrimSpace(*e.RedirectPath)
	}
	// 第 1 步：访问面（跨系统动作，不能进数据库事务 —— 事务回滚撤不掉已经删掉的符号链接）。
	if redirectPath != "" {
		if werr := s.writeRedirectArtifact(path, redirectPath); werr != nil {
			return werr // 可重试：产物落盘 / 链接激活的瞬时故障
		}
	} else {
		if derr := s.deactivatePaths([]string{path}); derr != nil {
			return derr
		}
	}
	// 第 2 步：数据库（路由占用 + 该语言的发布指针 + pages 镜像），一个事务。
	if terr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if s.routes != nil {
			if redirectPath != "" {
				// 占用改记为 redirect：路径仍被本页占着（谁都不能抢），但它服务的是 301。
				// 只清 active 行会让「这个路径还归谁」失去记录，别的页面随后就能抢占它。
				if _, rerr := s.routes.RedirectTx(ctx, tx, &pubcontract.RedirectReq{
					ProjectID: page.ProjectID, OldPath: path, PageID: page.ID,
				}); rerr != nil {
					return rerr
				}
			} else if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: page.ProjectID, Path: path,
			}); derr != nil {
				return derr
			}
		}
		return s.model.ClearPublicationLangTx(ctx, tx, page.ProjectID, page.ID, lang, time.Now().UTC())
	}); terr != nil {
		return terr // FS 已下线、DB 未跟上：下一轮重放（各步幂等）
	}
	logger.Scene("page").With("scheduleId", e.ID).With("pageId", page.ID).With("lang", lang).
		With("path", path).With("redirect", redirectPath).Info("定时下线完成")
	s.refreshSiteFilesAfterSchedule(ctx, page)
	return nil
}

// refreshSiteFilesAfterSchedule 刷新站点级产物（sitemap.xml / robots.txt / feed.xml / 404.html）。
//
// 与 publish() 主链同一口径：语言集按**发布口径**取，读不到就**跳过本次刷新**并留一条
// Error（保留上一版站点文件 —— 它们至少是完整的），绝不回滚已经生效的上下线。
// 失败不改变排定的成败：线上页面已经切换，站点文件是派生投影。
func (s *Service) refreshSiteFilesAfterSchedule(ctx context.Context, page *pagemodel.PageEntity) {
	if s.routes == nil || page == nil {
		return
	}
	siteLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		logger.Scene("page").With("pageId", page.ID).
			Error(lerr, "站点语言清单不可读，跳过 sitemap/robots 刷新（保留上一版站点文件）")
		return
	}
	if rerr := s.routes.RefreshSiteFiles(ctx, page.ProjectID, siteBaseURL(), pipeline.ActiveRoot(),
		siteLangs, s.defaultLocaleOf(ctx, page.ProjectID),
		// 方案按**本工程**解析后传下去：publication 不认识 project 契约，
		// 站点文件必须与这个工程的页面路径用同一份方案。
		string(pipeline.SiteLangURLModeOf(ctx, s.project, page.ProjectID)),
		s.notFoundHTMLOf(ctx, page.ProjectID)); rerr != nil {
		logger.Scene("page").With("pageId", page.ID).Error(rerr, "定时上下线后刷新 sitemap/robots 失败")
	}
}

// StartPageScheduleScheduler 启动定时上下线的到点扫描。
func StartPageScheduleScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return // 测试进程不启动：调度首跑会动真实库与存储，测试的行为必须由用例自己触发。
	}
	startPageScheduleScheduler(svc, pageScheduleInterval)
}

// StartPageScheduleSchedulerWithInterval 同上，但可注入间隔。
//
// 给用例用：注入一个远大于用例时长的间隔，就能证明「这次状态变化确实由用例自己触发的
// 那一轮扫描产生」，而不是被定时器顺手做掉的（与 pendingReceiptConverge 的
// Start...WithInterval 同一用意）。
func StartPageScheduleSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPageScheduleScheduler(svc, interval)
}

func startPageScheduleScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	if interval <= 0 {
		interval = pageScheduleInterval
	}
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), pageScheduleScanTimeout)
			defer cancel()
			if _, err := svc.RunDueSchedules(ctx); err != nil {
				logger.Scene("page").Error(err, "定时上下线扫描失败")
			}
		}
		// 启动首跑：补上进程停机期间到点的排定 —— 「到点」是绝对时刻，
		// 不因为进程当时没在运行而顺延（排定界面显示的也是那个时刻）。
		run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
