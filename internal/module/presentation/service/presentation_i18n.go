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

// publishAllLangs 按站点启用语言构建、逐语言结案，整批成功后才推进实例指针。
func (s *Service) publishAllLangs(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate, logicalPath string) (primaryArtifactID string, err error) {
	logicalPath = pipeline.LogicalPathOf(ctx, s.project, inst.ProjectID, logicalPath)
	if logicalPath == "" {
		logicalPath = s.instanceLogicalPath(ctx, inst)
	}
	langs := pipeline.EnabledLangs(ctx, s.project, inst.ProjectID)
	rule := pipeline.LangURLRuleForProject(ctx, s.project, inst.ProjectID)
	defaultLang := pipeline.DefaultLocale(ctx, s.project, inst.ProjectID)

	results := make([]langBuildResult, 0, len(langs))
	for _, lang := range langs {
		accessPath, perr := pipeline.SitePath(rule, lang, logicalPath)
		if perr != nil {
			return "", perr
		}
		built, berr := s.buildArtifact(ctx, inst.EntityType, inst.EntityID, accessPath, inst.ProjectID, lang, tpl)
		if berr != nil {
			return "", berr
		}
		results = append(results, langBuildResult{lang: lang, accessPath: accessPath, built: built})
	}

	// 本次发布前各语言的活跃产物（回执里的 from）：恢复与审计据此比对「从哪个产物切到
	// 哪一个」。必须在登记账本产物之前读取 —— 之后读到的是本次新写的行。
	previousArtifacts := s.publishedArtifactsByLang(ctx, inst.ID)

	now := time.Now().UTC()
	primaryArtifactID, snapID, err := s.persistMultiLangArtifacts(ctx, inst, tpl, results, now, logicalPath, defaultLang)
	if err != nil {
		return "", err
	}

	pinged := make([]string, 0, len(results))
	for i := range results {
		b := &results[i]
		if perr := s.publishOneLang(ctx, inst, b, previousArtifacts[b.lang], now); perr != nil {
			s.markBatchUnconverged(ctx, inst, perr)
			return "", perr
		}
		pinged = append(pinged, b.accessPath)
	}

	// 收尾：指针只在全部语言都结案后前进（失败分支不会走到这里）。
	if ferr := s.finalizeMultiLangBatch(ctx, inst, snapID, primaryArtifactID, now); ferr != nil {
		s.markBatchUnconverged(ctx, inst, ferr)
		return "", ferr
	}
	s.notifyIndexNow(ctx, inst, pinged...)
	return primaryArtifactID, nil
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
		return fmt.Errorf("写入 %s 的发布账本失败（访问面已激活，回执 %s 待恢复）: %w",
			b.accessPath, receiptID, merr)
	}
	// 结案失败只意味着账本没落终结态：访问面、路由与语言账本都已生效，未结案回执会在
	// 下次启动恢复时按同样证据补齐（幂等），因此不把该语言判为失败。
	if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
		logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
			Warn("多语言发布回执结案失败（访问面、路由与语言账本已生效，留待恢复补齐）: " + cerr.Error())
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
	logicalPath, defaultLang string) (primaryArtifactID, snapID string, err error) {
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
		if inst.TemplateID != tpl.TemplateID {
			if uerr := s.m.UpdateInstanceTemplateTx(tx, inst.ProjectID, inst.ID, tpl.TemplateID, now); uerr != nil {
				return uerr
			}
			inst.TemplateID = tpl.TemplateID
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
