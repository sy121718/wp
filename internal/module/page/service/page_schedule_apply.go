package pageservice

// page_schedule_apply.go — 到点动作的两条路径（PIPE-7 的执行面）。
//
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

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	pubcontract "go_wp/internal/module/publication/contract"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

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
		s.notFoundHTMLOf(ctx, page.ProjectID)); rerr != nil {
		logger.Scene("page").With("pageId", page.ID).Error(rerr, "定时上下线后刷新 sitemap/robots 失败")
	}
}
