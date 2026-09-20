package presentationservice

// presentation_i18n.go — 自动发布实例逐语言构建与激活（I18N-013）。
//
// 发布顺序（审计 AR2-003 收口）：
//
//	构建（无访问面副作用）→ 登记账本产物（一个事务：快照 + 各语言产物行 + 依赖）
//	→ 逐语言「登记 pending 回执 → 激活访问面 → 登记路由 → 写语言账本 → 结案」
//	→ 全部语言成功后推进实例指针并清 stale。
//
// 旧实现是「先在一个事务里提交全部语言的 publication 行与实例指针，事务提交之后
// 才逐个激活文件」：任一语言激活失败，数据库已经显示所有语言都已发布，而线上只有
// 前面几种语言的文件 —— 后台、sitemap 与语言切换器都会报出不可访问的 URL。
// 现在语言账本行（presentation_publications）是「该语言 URL 确实可访问」的唯一声明，
// 只在激活与路由登记都成功之后才写；指针只在整批成功后才前进。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type langBuildResult struct {
	lang       string
	accessPath string
	built      builtArtifact
	artifactID string
}

// instanceLogicalPath 实例 url_path 列存逻辑路径；若历史数据带语言前缀则剥掉。
func (s *Service) instanceLogicalPath(ctx context.Context, inst *presentationmodel.InstanceEntity) string {
	return pipeline.LogicalPathOf(ctx, s.project, inst.ProjectID, inst.URLPath)
}

// normalizeLogicalPath 创建/改 URL 时把输入路径归一化为逻辑路径。
func (s *Service) normalizeLogicalPath(ctx context.Context, projectID, raw string) (string, error) {
	normalized, err := pipeline.NormalizeURL(raw)
	if err != nil {
		return "", err
	}
	return pipeline.LogicalPathOf(ctx, s.project, projectID, normalized), nil
}

// ensureLogicalPathFree 预检逻辑路径下全部语言访问路径均未被占用。
func (s *Service) ensureLogicalPathFree(ctx context.Context, projectID, logicalPath, excludeInstanceID string) error {
	entries, err := pipeline.SiteRouteEntries(ctx, s.project, projectID, logicalPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := s.ensurePathFree(ctx, projectID, e.Path, excludeInstanceID); err != nil {
			return err
		}
	}
	// 逻辑路径本身也不得与其他实例冲突（UNIQUE project_id + url_path）。
	if _, err := s.m.FindInstanceByPath(ctx, projectID, logicalPath, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", logicalPath).Warn("详情页逻辑路径预检被拒绝：已被其他展示实例占用")
		return errors.New(presentationenums.ErrPathOccupied)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

// publishLangsOf 发布口径的站点启用语言：语言清单读不到即返回错误（审计 I18N-02）。
//
// 与 page 模块同名方法同义：本模块「会改动访问面 / 会写发布事实」的两处
// （publishAllLangs 的整批发布循环、batchConverged 的收敛判定）都走它 ——
// 前者降级会只发布默认语言并推进指针，后者降级会把「每种语言都有账本行」的检查
// 缩成只查默认语言那一行（假收敛）。两处的失败处置不同（一个中止整批、一个本轮
// 不处置），但**取数口径必须是同一条**，否则判定依据会随调用点漂移。
func (s *Service) publishLangsOf(ctx context.Context, projectID string) ([]string, error) {
	return pipeline.ResolveSiteLangs(ctx, s.project, projectID, pipeline.LangFallbackForbidden)
}

// publishAllLangs 按站点启用语言构建、逐语言结案，整批成功后才推进实例指针。
//
// 三段式（PERF-01；实例锁到底保护什么见 presentation_publish_plan.go 的文件头）：
//
//	① 锁内冻结本次发布的不可变输入（实例行 + 语言清单 + 编译底稿）
//	② 锁外逐语言编译与产物落盘 —— 最慢的一段，不再占着实例锁
//	③ 锁内校验实例状态未变 → 发布计划落库 → 逐语言结案 → 推进实例指针
//
// ② 期间别的发布会话可以先进锁提交（锁已让出）。此时 ③ 的指纹校验会判定本次编译
// 基于过期的实例状态，丢弃结果并重新冻结重试 —— 而不是把旧输入写回去（L4）。
func (s *Service) publishAllLangs(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent) (primaryArtifactID string, err error) {
	// 分阶段耗时（PERF-01 验收）：成功与失败都发一条 Info（失败时带原因），
	// 于是「锁等待还剩多少 / 编译占多少 / 有没有被冲突重试拖长」在生产上可见。
	metrics := publishSessionMetrics{startedAt: time.Now()}
	defer func() { s.logPublishSession(inst, metrics, err) }()

	var moved error
	for attempt := 1; attempt <= publishAttempts; attempt++ {
		metrics.attempts = attempt
		frozen, freezeTiming, ferr := s.freezePublish(ctx, inst, intent)
		metrics.lockWaitFreeze += freezeTiming.lockWait
		metrics.freeze += freezeTiming.work
		if ferr != nil {
			return "", ferr
		}
		metrics.langs = len(frozen.langs)

		results, compileWork, cerr := s.compileLangs(ctx, inst, frozen)
		metrics.compile += compileWork
		if cerr != nil {
			return "", cerr
		}

		var commitTiming phaseTiming
		primaryArtifactID, commitTiming, moved = s.commitPublish(ctx, inst, frozen, results)
		metrics.lockWaitCommit += commitTiming.lockWait
		metrics.commit += commitTiming.work
		if moved == nil {
			return primaryArtifactID, nil
		}
		if !errors.Is(moved, errPublishStateMoved) {
			return "", moved
		}
		// 冲突：编译结果整体作废（校验在任何写入之前，因此这里是零副作用失败），
		// 重新冻结实例状态再来一次。产物是内容寻址的，重试若产出同样的字节，
		// 落库阶段按 hash 复用同一行（recordArtifactTx），不会堆出版本。
		logger.Scene("build").With("instanceId", inst.ID).With("attempt", attempt).
			Warn("发布会话的实例状态已被并发批次推进，丢弃本次编译结果并重新冻结")
	}
	return "", fmt.Errorf("发布会话连续 %d 次被并发的发布批次推进实例状态，本次中止: %w", publishAttempts, moved)
}

// publishOneLang 单语言结案：登记回执 → 激活访问面 → 登记路由 → 写语言账本 → 结案。
//
// 任一步失败都向上返回，且该语言的 publication 行不会被写入（它只在走到倒数第二步
// 时才写）—— 「数据库声称已发布」与「文件真的在线」之间不允许有窗口。
// 回执保留 pending 时记录的正是「切到了哪一步」，交由启动恢复补齐或结案为未生效。
func (s *Service) publishOneLang(ctx context.Context, inst *presentationmodel.InstanceEntity,
	b *langBuildResult, fromArtifactID string, now time.Time) error {
	// 切换是不可逆的访问面副作用：先有账本才谈得上恢复（登记失败即中止，不切换）。
	receiptID, berr := s.beginPublishReceipt(ctx, inst, b.accessPath, b.lang, fromArtifactID, b.artifactID)
	if berr != nil {
		return fmt.Errorf("登记 %s 的发布回执失败（未切换访问面）: %w", b.accessPath, berr)
	}
	if aerr := s.activate(b.accessPath, b.built); aerr != nil {
		s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
		return aerr
	}
	// 路由登记失败不再只是日志里的 Warn（审计 AR2-004）：访问面已切换、路由账本缺行，
	// 会让占用预检 / 回滚 / 删除 / GC 全部依据错误的路由表决策。回执保持 pending。
	if rerr := s.registerRoute(ctx, inst, b.accessPath, b.artifactID); rerr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
			Error(rerr, "多语言路由登记失败（访问面已激活，回执待恢复）")
		// 回执留在 pending：推一次进程内快通道让收敛立刻重放（主链失败收口，非阻塞）。
		s.NotifyPendingReceipt()
		return fmt.Errorf("登记 %s 的路由占用失败（访问面已激活，回执 %s 待恢复）: %w",
			b.accessPath, receiptID, rerr)
	}
	// 语言账本：走到这里，这个语言的 URL 才真的可访问。
	if merr := s.m.MarkPublishedLang(ctx, presentationmodel.PublicationRecord{
		PresentationID: inst.ID, Lang: b.lang, ActivePath: b.accessPath,
		ArtifactID: b.artifactID, ArtifactHash: b.built.Hash, PublishedAt: now,
	}); merr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
			Error(merr, "多语言语言账本写入失败（访问面与路由已生效，回执待恢复）")
		// 同上：回执留在 pending，快通道让收敛按访问面证据补齐（幂等）。
		s.NotifyPendingReceipt()
		return fmt.Errorf("写入 %s 的发布账本失败（访问面已激活，回执 %s 待恢复）: %w",
			b.accessPath, receiptID, merr)
	}
	// 结案失败只意味着账本没落终结态：访问面、路由与语言账本都已生效，未结案回执会在
	// 下次启动恢复时按同样证据补齐（幂等），因此不把该语言判为失败。
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
			Warn("多语言发布回执结案失败（访问面、路由与语言账本已生效，留待恢复补齐）: " + cerr.Error())
		// 状态已一致、回执没收口：推快通道让收敛立刻幂等收尾。
		s.NotifyPendingReceipt()
	}
	return nil
}

// finalizeMultiLangBatch 整批成功后的收尾：推进实例指针（active/staged = 默认语言产物）、
// 清 stale、记发布时间。
//
// 指针落后于访问面（部分语言失败）是安全方向：stale + pending 回执 + 启动恢复会收敛，
// 而且指针指向的旧产物是内容寻址的不可变文件，仍在原处可访问。反方向——指针声称已发布
// 而文件不存在——才会骗到 sitemap 与后台，这里从顺序上排除了它。
func (s *Service) finalizeMultiLangBatch(ctx context.Context, inst *presentationmodel.InstanceEntity,
	snapID, primaryArtifactID string, now time.Time) error {
	if strings.TrimSpace(primaryArtifactID) == "" || strings.TrimSpace(snapID) == "" {
		return errors.New("多语言发布缺少默认语言产物或快照，无法推进实例指针")
	}
	inst.CurrentSnapshotID = &snapID
	inst.StagedSnapshotID = &snapID
	inst.StagedArtifactID = &primaryArtifactID
	inst.ActiveArtifactID = &primaryArtifactID
	inst.Stale = false
	inst.PublishedAt = &now
	inst.UpdatedAt = now
	return s.m.UpdateInstancePointers(ctx, inst)
}

// markBatchUnconverged 批次未收敛时把实例标记为待重建（后台可见的「明确 partial」）。
//
// 语言账本里缺哪些语言是精确的（缺行 = 该语言没上线），stale 则表达「这个实例还有
// 一批没走完」：依赖失效触发的自动重建、构建队列或启动恢复都会把它收敛回全成功。
func (s *Service) markBatchUnconverged(ctx context.Context, inst *presentationmodel.InstanceEntity, cause error) {
	inst.Stale = true
	if _, err := s.m.MarkStale(ctx, inst.ProjectID, []string{inst.ID}, time.Now().UTC()); err != nil {
		logger.Scene("build").With("instanceId", inst.ID).Error(err, "标记实例待重建失败")
	}
	logger.Scene("build").With("instanceId", inst.ID).
		Error(cause, "多语言发布批次未收敛（实例已标记待重建）")
}

// publishedArtifactsByLang 实例当前各语言的活跃产物 id（无记录 / 查询失败时为空表）。
func (s *Service) publishedArtifactsByLang(ctx context.Context, instanceID string) map[string]string {
	out := map[string]string{}
	pubs, err := s.m.ListPublications(ctx, instanceID)
	if err != nil {
		return out
	}
	for _, pub := range pubs {
		if pub.ArtifactID != nil {
			out[pub.Lang] = *pub.ArtifactID
		}
	}
	return out
}

// persistMultiLangArtifacts 一次快照 + 多语言账本产物行 + 模板/路径变更 + 依赖记录。
//
// 这个事务**不声明任何语言已上线**：它只登记「本次构建产出了哪些字节」（产物行）与
// 「构建输入是什么」（快照、依赖）。语言账本行与实例指针由逐语言结案和批次收尾各自
// 写入 —— 把「记账」与「上线」分开，才有 AR2-003 要的那条不变式。
func (s *Service) persistMultiLangArtifacts(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate, results []langBuildResult, now time.Time,
	logicalPath, defaultLang string, mode *instanceModePending) (primaryArtifactID, snapID string, err error) {
	snapID = uuid.NewString()
	snap := &presentationmodel.SnapshotEntity{
		ID: snapID, PresentationInstanceID: inst.ID,
		SourceTemplateVersionID: tpl.VersionID, SourceEntityRevisionID: inst.EntityID,
		Document: tpl.Document, CreatedAt: now,
	}

	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateSnapshotTx(tx, snap); cerr != nil {
			return cerr
		}
		// 渲染模式与独立文档（双轨，迁移 282）与快照/产物/指针同事务：
		// 分开写会留下「文档已换、模式没换」的中间态，下一次模板更新就会按
		// template 模式把它重建回模板文档 —— 用户的自定义凭空消失。
		if mode != nil {
			if uerr := s.m.UpdateInstanceModeTx(tx, inst.ProjectID, inst.ID, mode.renderMode, mode.document, now); uerr != nil {
				return uerr
			}
			inst.RenderMode = mode.renderMode
			if mode.renderMode == presentationmodel.RenderModeDocument {
				inst.OverrideDocument = mode.document
			} else {
				inst.OverrideDocument = nil
			}
		}
		if inst.TemplateID != tpl.TemplateID {
			if uerr := s.m.UpdateInstanceTemplateTx(tx, inst.ProjectID, inst.ID, tpl.TemplateID, now); uerr != nil {
				return uerr
			}
			inst.TemplateID = tpl.TemplateID
			// 换底稿 = 放弃独立文档（双轨语义）：产物已按新模板编译，文档列若留着旧自定义，
			// 下次重建又会拿旧文档盖掉新模板 —— 与本次「切换」自相矛盾。
			// mode != nil 时上面已处理（ReapplyPreset 的「换模板 + 回跟随」路径同时给两者）。
			if mode == nil && (presentationmodel.IsDocumentMode(inst.RenderMode) || len(inst.OverrideDocument) > 0) {
				if cerr := s.m.ClearInstanceModeTx(tx, inst.ProjectID, inst.ID, now); cerr != nil {
					return cerr
				}
				inst.RenderMode = presentationmodel.RenderModeTemplate
				inst.OverrideDocument = nil
			}
		}
		if logicalPath != "" && inst.URLPath != logicalPath {
			if uerr := s.m.UpdateInstanceURLTx(tx, inst.ProjectID, inst.ID, logicalPath, now); uerr != nil {
				return uerr
			}
			inst.URLPath = logicalPath
		}
		version, verr := s.m.NextArtifactVersionTx(tx, inst.ID)
		if verr != nil {
			return verr
		}
		for i := range results {
			b := &results[i]
			aid, aerr := s.recordArtifactTx(ctx, tx, inst, snapID, b.built, b.lang, version, now)
			if aerr != nil {
				return aerr
			}
			b.artifactID = aid
			if b.lang == defaultLang {
				primaryArtifactID = aid
			}
		}
		if primaryArtifactID == "" && len(results) > 0 {
			primaryArtifactID = results[0].artifactID
		}
		// 依赖记录挂在默认语言产物上（各语言依赖集合相同）。
		if len(results) > 0 {
			deps := results[0].built.Manifest.Dependencies
			return s.persistDependenciesTx(tx, inst.ID, primaryArtifactID, deps, now)
		}
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return primaryArtifactID, snapID, nil
}
