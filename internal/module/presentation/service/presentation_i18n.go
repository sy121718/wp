package presentationservice

// presentation_i18n.go — 自动发布实例逐语言构建与激活（I18N-013）。

import (
	"context"
	"errors"
	"fmt"
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

// publishAllLangs 按站点启用语言构建、落库、激活、登记路由。
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

	now := time.Now().UTC()
	primaryArtifactID, err = s.persistMultiLangBuild(ctx, inst, tpl, results, now, logicalPath, defaultLang)
	if err != nil {
		return "", err
	}

	// 本次发布前各语言的活跃产物（回执里的 from）：恢复与审计据此比对「从哪个产物
	// 切到了哪个」。查询失败不阻断发布 —— 回执的 to 才是恢复判据。
	previousArtifacts := map[string]string{}
	if pubs, perr := s.m.ListPublications(ctx, inst.ID); perr == nil {
		for _, pub := range pubs {
			if pub.ArtifactID != nil {
				previousArtifacts[pub.Lang] = *pub.ArtifactID
			}
		}
	}

	pinged := make([]string, 0, len(results))
	for i := range results {
		b := &results[i]
		// 每语言在切换访问面之前登记 pending 回执：切换是不可逆的访问面副作用，
		// 先有账本才谈得上恢复（登记失败即中止，不切换）。
		receiptID, berr := s.beginPublishReceipt(ctx, inst, b.accessPath, b.lang,
			previousArtifacts[b.lang], b.artifactID)
		if berr != nil {
			return "", fmt.Errorf("登记 %s 的发布回执失败（未切换访问面）: %w", b.accessPath, berr)
		}
		if err = s.activate(b.accessPath, b.built); err != nil {
			s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
			return "", fmt.Errorf("激活 %s 失败: %w", b.accessPath, err)
		}
		// 路由登记失败不再只是日志里的 Warn（审计 AR2-004）：访问面已切换、路由账本
		// 缺行，会让占用预检 / 回滚 / 删除 / GC 全部依据错误的路由表决策。回执保持
		// pending —— 它记录的正是「访问面已激活但路由未登记」这个可恢复状态，
		// 启动恢复按访问面实际指向幂等补齐登记。
		if rerr := s.registerRoute(ctx, inst, b.accessPath, b.artifactID); rerr != nil {
			logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
				Error(rerr, "多语言路由登记失败（访问面已激活，回执待恢复）")
			return "", fmt.Errorf("登记 %s 的路由占用失败（访问面已激活，回执 %s 待恢复）: %w",
				b.accessPath, receiptID, rerr)
		}
		// 结案失败只意味着账本没落终结态：访问面与路由都已生效，未结案回执会在下次
		// 启动恢复时按同样证据补齐（幂等），因此不把整次发布判为失败。
		if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
			logger.Scene("build").With("instanceId", inst.ID).With("url", b.accessPath).
				Warn("多语言发布回执结案失败（访问面与路由已生效，留待恢复补齐）: " + cerr.Error())
		}
		pinged = append(pinged, b.accessPath)
	}
	s.notifyIndexNow(ctx, inst, pinged...)
	return primaryArtifactID, nil
}

// persistMultiLangBuild 一次快照 + 多语言产物行 + 各语言 publication + 默认语言指针。
func (s *Service) persistMultiLangBuild(ctx context.Context, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate, results []langBuildResult, now time.Time,
	logicalPath, defaultLang string) (primaryArtifactID string, err error) {
	snapID := uuid.NewString()
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
			pub := presentationmodel.PublicationRecord{
				PresentationID: inst.ID, Lang: b.lang, ActivePath: b.accessPath,
				ArtifactID: aid, ArtifactHash: b.built.Hash, PublishedAt: now,
			}
			if perr := s.m.MarkPublishedLangTx(tx, pub); perr != nil {
				return perr
			}
		}
		if primaryArtifactID == "" && len(results) > 0 {
			primaryArtifactID = results[0].artifactID
		}
		inst.CurrentSnapshotID = &snapID
		inst.StagedSnapshotID = &snapID
		inst.StagedArtifactID = &primaryArtifactID
		inst.ActiveArtifactID = &primaryArtifactID
		inst.Stale = false
		inst.PublishedAt = &now
		inst.UpdatedAt = now
		if uerr := s.m.UpdateInstancePointersTx(tx, inst.ProjectID, inst); uerr != nil {
			return uerr
		}
		// 依赖记录挂在默认语言产物上（各语言依赖集合相同）。
		if len(results) > 0 {
			deps := results[0].built.Manifest.Dependencies
			return s.persistDependenciesTx(tx, inst.ID, primaryArtifactID, deps, now)
		}
		return nil
	})
	return primaryArtifactID, err
}
