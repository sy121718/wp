package pageservice

// page_rebuild.go — 单页重建编排（审计 ARCH-04）。
//
// 报告的问题（P1）：同步路径（RebuildStale 的前 20 页）遍历站点启用语言逐个构建，
// 并重新发布**此前已发布**的语言；溢出部分交给构建队列后，executor 只调 Build(ID) ——
// 没有语言、没有重新发布。于是「同一批第 21 个之后」可能停在默认语言的暂存态，
// 页面上线状态与前面 20 个不一致。
//
// 根因不是漏传了一个参数，而是**两条路径各有一份实现**。本文件把「一次单页重建」
// 的唯一实现收在这里：
//
//	planPageRebuild  冻结这次重建的上下文（语言集合 / 旧发布范围 / 输入版本 / 意图）；
//	rebuildPage      按计划逐语言构建，构建成功后只回写旧发布范围内的语言；
//	RunPageBuildJob  队列执行体的入口 —— 把任务行还原成计划，再走上面同一条编排。
//
// 同步路径（RebuildStale）与异步路径（构建队列 worker）因此只差「计划是怎么来的」，
// 不差「计划怎么执行」。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	"go_wp/pkg/logger"
)

// maxAutoRebuildPages 单次依赖失效触发的自动重建上限。
//
// 为什么需要上限：自动重建发生在内容写入的请求内（PIPE-2 构建队列尚未落地），
// 无界重建会让一次内容保存耗时随站点规模线性增长。超限的页面交给构建队列
// （enqueueOverflowBuildJobs），由它的编排按同一套语义重建 —— 不是丢弃。
const maxAutoRebuildPages = 20

// pageRebuildPlan 一次单页重建的冻结上下文（编排的唯一输入）。
//
// 「冻结」的含义：三样东西在**进入编排之前**一次性确定，编排内部不再重新推导 ——
// 推导两次就可能拿到两份（两次读之间站点语言表 / 发布台账变了），
// 而这次的构建与回写必须落在同一份事实上。
type pageRebuildPlan struct {
	// Page 已定位（含工程作用域）的页面记录。
	Page *pagemodel.PageEntity
	// Langs 本次要构建的语言集合（默认语言在前）。
	Langs []string
	// PublishLangs 旧发布范围：构建成功后允许回写线上的语言（Langs 的子集）。
	// 为空表示「这次不做任何回写」——手工构建与从未发布过的页面都走这条。
	PublishLangs []string
	// DraftVersion / InputHash 计划生成时冻结的输入版本。
	DraftVersion int64
	InputHash    string
	// Intent 构建意图（pagecontract.BuildIntent*）。
	Intent string
}

// publishes 该语言是否在旧发布范围内（构建成功后要重新发布）。
func (p *pageRebuildPlan) publishes(lang string) bool {
	if p == nil {
		return false
	}
	for _, l := range p.PublishLangs {
		if l == lang {
			return true
		}
	}
	return false
}

// normalizeRebuildIntent 归一化构建意图：空 = 依赖重建（与 build_jobs.intent 的默认值同口径）。
//
// 白名单外的值也归到依赖重建：这个值只影响「构建完要不要回写线上」，而回写又只发生在
// 旧发布范围内（此前已发布的语言）。未知意图被当成依赖重建最多是「按已发布语言刷新一次」，
// 当成手工构建却会让本该上线的语言停在旧字节 —— 默认值必须站在更安全的一侧。
func normalizeRebuildIntent(raw string) string {
	if strings.TrimSpace(raw) == pagecontract.BuildIntentManual {
		return pagecontract.BuildIntentManual
	}
	return pagecontract.BuildIntentDependency
}

// planPageRebuild 组装单页重建计划。
//
// langs 为空时按**发布口径**解析站点启用语言（读不到即返回错误，不降级为默认语言一种）；
// 非空时用它（队列任务的语言在入队时就冻结了，这里不再读一次）。
//
// 旧发布范围只在 dependency 意图下解析：manual 是「人工点一次构建」，它不该顺手把页面
// 重新上线 —— 回写线上是发布动作，必须由人显式发起。
func (s *Service) planPageRebuild(ctx context.Context, page *pagemodel.PageEntity, langs []string, intent string) (*pageRebuildPlan, error) {
	if page == nil {
		return nil, ErrPageNotFound
	}
	plan := &pageRebuildPlan{
		Page: page, Intent: normalizeRebuildIntent(intent),
		DraftVersion: page.DraftVersion, InputHash: hash(page.DraftDocument),
	}
	if len(langs) == 0 {
		resolved, err := s.publishLangsOf(ctx, page.ProjectID)
		if err != nil {
			return nil, err
		}
		langs = resolved
	}
	plan.Langs = langs
	if plan.Intent == pagecontract.BuildIntentManual {
		return plan, nil
	}
	// 旧发布范围：此前已发布的语言（page_publications 为真源）。
	//
	// 一次性解析而不是在构建循环里逐语言读：循环里读到的集合可能中途变化，
	// 而本次构建与回写要落在同一份事实上。读不到即整体失败（调用方保持 stale）——
	// 「读不到」与「没发布过」在访问面上的差别是「站点停更」与「不上线」，
	// 拿不准就宁可不动，也不能用猜出来的范围去回写线上。
	for _, lang := range langs {
		path, err := s.publishedPathOf(ctx, page, lang)
		if err != nil {
			return nil, fmt.Errorf("读取语言 %s 的发布状态失败: %w", lang, err)
		}
		if path != "" {
			plan.PublishLangs = append(plan.PublishLangs, lang)
		}
	}
	return plan, nil
}

// RebuildPage 按计划执行单页重建（同步路径与队列 worker 共用的**唯一**实现）。
//
// 逐语言：构建；构建成功后，若该语言在旧发布范围内则发布（重新发布此前已发布的语言，
// 从未发布过的语言只留在暂存态 —— 「CMS 变更自动发布」的落地口径）。
//
// 单语言失败不阻断其余语言（继续下一个，错误累积后一并返回）：
//   - 同步路径的调用方是内容写入的后置副作用，它按「尽力而为」处理并把错误记进日志，
//     页面保持 stale 等待收敛；
//   - 队列 worker 的调用方是执行器，它按错误把任务标 failed（失败准确反映到任务上）。
func (s *Service) rebuildPage(ctx context.Context, plan *pageRebuildPlan) (rebuilt, published int, err error) {
	if plan == nil || plan.Page == nil {
		return 0, 0, ErrInvalidParam
	}
	manual := plan.Intent == pagecontract.BuildIntentManual
	var errs []error
	for _, lang := range plan.Langs {
		if cerr := ctx.Err(); cerr != nil {
			errs = append(errs, cerr)
			break
		}
		if _, berr := s.Build(ctx, &pagedto.BuildReq{ID: plan.Page.ID, Lang: lang}); berr != nil {
			errs = append(errs, fmt.Errorf("语言 %s 构建失败: %w", lang, berr))
			continue
		}
		rebuilt++
		if manual || !plan.publishes(lang) {
			continue
		}
		if _, perr := s.Publish(ctx, &pagedto.PublishReq{ID: plan.Page.ID, Lang: lang}); perr != nil {
			errs = append(errs, fmt.Errorf("语言 %s 重新发布失败: %w", lang, perr))
			continue
		}
		published++
	}
	return rebuilt, published, errors.Join(errs...)
}

// RunPageBuildJob 执行一条构建队列任务（执行器执行体，实现 pagecontract.PageService）。
//
// 任务行的三样上下文（lang / intent / 输入版本）在这里还原成计划：
//   - lang 非空：本条任务只负责该语言（依赖重建在入队时按语言拆成了多行，见迁移 307）；
//   - lang 为空：只可能是 ARCH-04 之前入队的存量行，按**站点启用语言集合**处理 ——
//     与同步路径逐条一致，而不是把空 lang 当成「默认语言一种」（那正是本条审计要消灭的
//     「停在默认语言暂存态」）；
//   - intent=manual：只构建，不回写线上。
//
// 输入版本（DraftVersion / BuildInputHash）在这里**不作为跳过条件**：依赖重建的语义是
// 「这一页的产物可能过期了」，重建永远以当前草稿为准（确定性构建使同输入产出同字节）；
// 若冻结版本落后于当前草稿，记一条日志说明本次实际构建的是更新后的草稿 ——
// 这是如实记录，不改变结果。用版本差异跳过会让「草稿变了但没再扇出」的页面永远停在旧字节。
func (s *Service) RunPageBuildJob(ctx context.Context, req *pagedto.PageBuildJobReq) error {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		// 页面不存在 / 已删除：任务失败（原因写进 error_message），不静默算成功。
		return err
	}
	intent := normalizeRebuildIntent(req.Intent)
	var langs []string
	if lang := strings.TrimSpace(req.Lang); lang != "" {
		langs = []string{lang}
	} else if intent == pagecontract.BuildIntentManual {
		// 手工构建没有语言上下文时按 BuildReq 的既有口径：站点默认语言一种。
		langs = []string{buildLang("")}
	}
	// intent=dependency 且 lang 为空（存量任务）时 langs 留空，交给计划按站点语言集合解析。
	plan, perr := s.planPageRebuild(ctx, page, langs, intent)
	if perr != nil {
		return perr
	}
	if req.DraftVersion > 0 && req.DraftVersion != page.DraftVersion {
		logger.Scene("build").With("pageId", page.ID).With("lang", req.Lang).
			With("frozenDraftVersion", req.DraftVersion).With("currentDraftVersion", page.DraftVersion).
			Info("构建任务冻结的草稿版本已落后于当前草稿，本次按当前草稿重建（依赖重建以最新草稿为准）")
	}
	rebuilt, published, rerr := s.rebuildPage(ctx, plan)
	if rerr != nil {
		logger.Scene("build").With("pageId", page.ID).With("lang", req.Lang).With("intent", plan.Intent).
			With("rebuilt", rebuilt).With("published", published).Error(rerr, "构建任务执行失败（页面保持 stale）")
		return rerr
	}
	logger.Scene("build").With("pageId", page.ID).With("lang", req.Lang).With("intent", plan.Intent).
		With("rebuilt", rebuilt).With("published", published).Info("构建任务执行完成")
	return nil
}

// SetBuildQueue 注入构建队列端口（装配期调用）。
//
// 未注入时 enqueueOverflowBuildJobs 会退回「记告警、保持 stale」的既有行为 ——
// 不静默丢弃，也不假装已经排上了。
func (s *Service) SetBuildQueue(q pagecontract.BuildQueueEnqueuer) {
	if s == nil {
		return
	}
	s.buildQueue = q
}

// enqueueOverflowBuildJobs 把超出单次同步重建上限的页面交给构建队列。
//
// 逐页逐语言入队（审计 ARCH-04）：语言集合在**入队时冻结** —— 每条任务负责一种语言，
// 消费侧因此不再需要（也不应该）重新解析站点语言表。队列的待办去重键含 lang
// （迁移 307），所以同一页面的两种语言是两条待办，不会互相去重。
//
// 输入版本：draft_version 与草稿文档摘要一起进任务行，作为去重键与审计依据
// （不再用空 hash 充当「这是依赖重建」的隐式操作码 —— 意图由 intent 列显式表达）。
//
// 单个页面入队失败只记日志：这是一条尽力而为的旁路（同步那部分已经重建完了），
// 抛错会让调用方误以为整批失败。
func (s *Service) enqueueOverflowBuildJobs(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	if s.buildQueue == nil {
		logger.Scene("dependency").With("affected", len(ids)).
			Warn("自动重建超出单次上限且构建队列未接入，剩余页面保持 stale 等待后续触发")
		return
	}
	queued := 0
	for _, id := range ids {
		// 同 RebuildStale：入队前也要按工程作用域读一次页面（漏作用域时整批任务静默不再入队）。
		page, err := s.locatePageInProjects(ctx, id)
		if err != nil {
			continue
		}
		langs, lerr := s.publishLangsOf(ctx, page.ProjectID)
		if lerr != nil {
			logger.Scene("dependency").With("page_id", id).
				Error(lerr, "超限重建入队跳过：站点语言清单不可读，入队默认语言一种会让其余语言永久停在旧字节")
			continue
		}
		inputHash := hash(page.DraftDocument)
		for _, lang := range langs {
			if qerr := s.buildQueue.EnqueuePageBuild(ctx, id, page.ProjectID, lang,
				pagecontract.BuildIntentDependency, page.DraftVersion, inputHash); qerr != nil {
				logger.Scene("dependency").With("page_id", id).With("lang", lang).
					Error(qerr, "超限重建任务入队失败")
				continue
			}
			queued++
		}
	}
	logger.Scene("dependency").With("queued", queued).With("affected", len(ids)).
		Info("超限的自动重建已交给构建队列")
}
