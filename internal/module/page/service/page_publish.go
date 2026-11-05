package pageservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/pkg/builder"
	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactenums "go_wp/internal/module/artifact/enums"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	pubenums "go_wp/internal/module/publication/enums"
	"go_wp/internal/pipeline"
	"go_wp/pkg/seo"
	"go_wp/pkg/seo/compliance"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const systemCreator = ""

func (s *Service) Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion > 0 && req.ExpectedVersion != page.DraftVersion {
		return nil, ErrDraftVersionConflict
	}

	lang := buildLang(req.Lang)

	if pageExcludesLang(page, lang) {
		return nil, ErrPageLangExcluded
	}

	plan, persistPlan, err := s.publicationPlanFor(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	frozenRefs, err := s.freezeReferences(ctx, page.ProjectID, page.DraftDocument)
	if err != nil {
		return nil, err
	}
	plan.References = frozenRefs.Unwrap()
	persistPlan = true
	path, err := s.sitePathOfWithPlan(ctx, lang, page, &plan)
	if err != nil {
		return nil, err
	}
	logger.Scene("build").With("pageId", page.ID).With("lang", lang).With("path", path).
		With("planHash", plan.Hash()).Info("开始构建")
	if err = s.syncKernel(path, lang, page.DraftDocument, page.ID, &plan); err != nil {
		return nil, err
	}
	hash, err := s.publisher.Build(ctx, page.ID, s.kernelVersion(page.ID))
	if err != nil {
		logger.Scene("build").With("pageId", page.ID).Error(err, "构建失败")
		return nil, mapPublishError(err)
	}

	artifactID, deps, err := s.ensureArtifactRow(ctx, page, hash, page.DraftDocument, lang)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if derr := s.persistDependenciesTx(ctx, tx, page.ProjectID, page.ID, artifactID, deps); derr != nil {
			return derr
		}

		if persistPlan {
			if perr := s.model.UpdatePublicationPlanRecordTx(ctx, tx, page.ID, lang, plan, page.DraftVersion, now); perr != nil {
				return perr
			}
		}

		return s.model.MarkStagedLangTx(ctx, tx, page.ProjectID, page.ID, lang, artifactID, hash, page.DraftVersion, now)
	}); err != nil {
		logger.Scene("build").With("pageId", page.ID).With("artifactID", artifactID).Error(err, "依赖记录/暂存指针写入失败")
		return nil, err
	}

	s.inspectBuiltArtifact(ctx, page.ProjectID, hash, path, lang, s.pathListedInSitemap(path))
	logger.Scene("build").With("pageId", page.ID).With("hash", hash).Info("构建完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StateReady,
		StagedHash: hash, DraftPath: page.DraftPath,
	}, nil
}

func (s *Service) Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error) {
	return s.publish(ctx, req, true)
}

func (s *Service) PublishAllLanguages(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishAllResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	langs, err := s.publishLangsOf(ctx, page.ProjectID)
	if err != nil {
		return nil, err
	}
	references, err := s.freezeReferences(ctx, page.ProjectID, page.DraftDocument)
	if err != nil {
		return nil, err
	}
	ctx = pipeline.WithReferenceSnapshot(ctx, references)
	res = &pagedto.PublishAllResp{
		PageID:  page.ID,
		Results: make([]pagedto.LangPublishResp, 0, len(langs)),
	}
	logger.Scene("publication").With("pageId", page.ID).
		With("langs", strings.Join(langs, ",")).Info("一键发布全部启用语言")
	for _, lang := range langs {
		if cerr := ctx.Err(); cerr != nil {
			res.Results = append(res.Results, pagedto.LangPublishResp{
				Lang: lang, Status: "failed", Error: cerr.Error(),
			})
			break
		}

		if pageExcludesLang(page, lang) {
			res.Results = append(res.Results, pagedto.LangPublishResp{Lang: lang, Status: langPublishSkipped})
			res.Skipped++
			continue
		}
		if _, berr := s.Build(ctx, &pagedto.BuildReq{ID: req.ID, Lang: lang}); berr != nil {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				Error(berr, "一键发布：语言构建失败")
			res.Results = append(res.Results, pagedto.LangPublishResp{
				Lang: lang, Status: "failed", Error: berr.Error(),
			})
			continue
		}
		pr, perr := s.publish(ctx, &pagedto.PublishReq{ID: req.ID, Lang: lang}, false)
		if perr != nil {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				Error(perr, "一键发布：语言发布失败")
			res.Results = append(res.Results, pagedto.LangPublishResp{
				Lang: lang, Status: "failed", Error: perr.Error(),
			})
			continue
		}
		item := pagedto.LangPublishResp{Lang: lang, Status: "ok"}
		if pr != nil {
			item.ActiveHash = pr.ActiveHash
		}
		res.Results = append(res.Results, item)
		res.Published++
	}
	return res, nil
}

func (s *Service) publish(ctx context.Context, req *pagedto.PublishReq, refreshPeers bool) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	lang := buildLang(req.Lang)

	if pageExcludesLang(page, lang) {
		return nil, ErrPageLangExcluded
	}

	plan, persistPlan, err := s.publicationPlanFor(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	path, err := s.sitePathOfWithPlan(ctx, lang, page, &plan)
	if err != nil {
		return nil, err
	}
	logger.Scene("publication").With("pageId", page.ID).With("lang", lang).With("path", path).
		With("planHash", plan.Hash()).Info("开始发布")

	stagedArt, err := s.stagedArtifactOf(ctx, page, lang)
	if err != nil {
		return nil, err
	}
	if stagedArt.ArtifactKey == "" || stagedArt.PageID != page.ID {
		return nil, ErrNoStagedArtifact
	}

	s.persistDependenciesFromManifest(ctx, page.ProjectID, page.ID, stagedArt.ID, stagedArt.Manifest)

	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, path, page.ID); err != nil {
		logger.Scene("publication").With("pageId", page.ID).With("path", path).Warn("发布被拒绝：路径已被占用")
		return nil, err
	}

	if err = s.syncKernel(path, lang, page.DraftDocument, page.ID, &plan); err != nil {
		return nil, err
	}
	version := s.kernelVersionOrOne(page.ID)
	built, buildErr := s.publisher.Build(ctx, page.ID, version)
	if buildErr != nil {
		logger.Scene("build").With("pageId", page.ID).Error(buildErr, "发布前复构建失败")
		return nil, mapPublishError(buildErr)
	}
	if built != stagedArt.ArtifactHash {

		staging, gerr := s.model.GetStaging(ctx, page.ID, lang)
		if gerr != nil || staging == nil || staging.DraftVersion != page.DraftVersion {
			return nil, ErrRebuildRequired
		}
		refreshed, ferr := s.publisher.Build(ctx, page.ID, version)
		if ferr != nil {
			return nil, mapPublishError(ferr)
		}
		if refreshed != built {

			return nil, ErrRebuildRequired
		}
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Warn("暂存产物落后于站点级状态（如其它语言刚发布），已按当前草稿重新构建")

		artifactID, deps, aerr := s.ensureArtifactRow(ctx, page, refreshed, page.DraftDocument, lang)
		if aerr != nil {
			return nil, aerr
		}
		s.persistDependencies(ctx, page.ProjectID, page.ID, artifactID, deps)

		stagedArt.ID = artifactID
		stagedArt.ArtifactHash = refreshed
		built = refreshed

		s.inspectBuiltArtifact(ctx, page.ProjectID, refreshed, path, lang, true)
	}

	fromArtifactID := s.publishedArtifactIDOf(ctx, page, lang)

	oldPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}

	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionSwitchActive,
		ProjectID: page.ProjectID, PageID: page.ID, Path: path, Lang: lang,
		FromArtifactID: fromArtifactID, ToArtifactID: stagedArt.ID, OldPath: oldPath,
		CapturedInputVersion: page.InputVersion,
		FromReceiptID:        s.capturedFromReceiptID(ctx, page.ProjectID, page.ID, lang),
	})
	if rerr != nil {
		return nil, rerr
	}

	receiptSeq, seqErr := parseReceiptID(receiptID)
	if seqErr != nil {
		s.abortPublishReceipt(ctx, receiptID, "回执 id 无法解析为数值身份")
		return nil, ErrPublishLedgerUnavailable
	}

	activationPlan := (*pipeline.PublicationPlan)(nil)
	if persistPlan {
		activationPlan = &plan
	}

	now := time.Now().UTC()
	hash := ""
	activated := false
	txErr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		snap, lerr := s.model.LockPageForActivationTx(ctx, tx, page.ProjectID, page.ID)
		if lerr != nil {
			return lerr
		}
		if snap.InputVersion != page.InputVersion {

			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				With("captured", page.InputVersion).With("current", snap.InputVersion).
				Warn("发布放弃：捕获的输入代次已过期（期间内容或依赖变更）")
			return ErrInputMoved
		}

		stagedHash, serr := s.publisher.StageArtifact(page.ID)
		if serr != nil {
			return serr
		}
		if stagedHash != stagedArt.ArtifactHash {

			return ErrRebuildRequired
		}

		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}

		if aerr := s.publisher.ActivateStagedArtifact(page.ID); aerr != nil {
			return aerr
		}
		activated = true
		hash = stagedHash

		if merr := s.model.MarkPublishedLangTx(ctx, tx, page.ProjectID, pagemodel.PublicationRecord{
			PageID: page.ID, Lang: lang, ActivePath: path,
			ArtifactID: stagedArt.ID, ArtifactHash: hash, PublishedAt: now,

			ReceiptID: receiptSeq,
		}); merr != nil {
			return merr
		}
		if activationPlan != nil {
			if perr := s.model.UpdatePublicationPlanRecordTx(ctx, tx, page.ID, lang, *activationPlan, page.DraftVersion, now); perr != nil {
				return perr
			}
		}
		if s.routes == nil {
			return nil
		}
		if oldPath != "" && oldPath != path {

			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: page.ProjectID, Path: oldPath, PageID: page.ID,
			}); derr != nil {
				return derr
			}
		}

		if _, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: page.ProjectID, Path: path, PageID: page.ID, ArtifactID: stagedArt.ID,
		}); aerr != nil {
			return fmt.Errorf("访问面已切换但路径归属复验失败（另一实体已接管该路径）: %w", aerr)
		}

		if ferr := s.postSwitchFaultHit(); ferr != nil {
			return ferr
		}
		return nil
	})
	if txErr != nil {
		if !activated {

			s.abortPublishReceipt(ctx, receiptID, "提交段未切换访问面")
			logger.Scene("publication").With("pageId", page.ID).Error(txErr, "发布失败（访问面未切换）")
			return nil, mapPublishError(txErr)
		}

		s.keepPublishReceiptPending(receiptID, "DB 激活状态写入失败")
		logger.Scene("publication").With("pageId", page.ID).With("hash", hash).
			Error(txErr, "发布 FS 已激活，但 DB 激活状态事务失败（线上已生效，交收敛判定）")
		return nil, fmt.Errorf("发布已生效但数据库状态同步失败: %w", txErr)
	}

	oldPathSettleFailed := false
	if s.routes != nil && oldPath != "" && oldPath != path {
		if derr := s.settleOldPathLocked(ctx, page.ID, page.ProjectID, path, oldPath, false); derr != nil {
			if errors.Is(derr, errOldPathTaken) {
				logger.Scene("publication").With("pageId", page.ID).With("oldPath", oldPath).
					Warn("发布后旧路径已不归本页：不处置（由当前归属者负责）")
			} else {
				s.keepPublishReceiptPending(receiptID, "旧路径访问面处置失败")
				logger.Scene("publication").With("pageId", page.ID).With("oldPath", oldPath).
					Error(derr, "发布后旧路径处置失败（DB 已一致，回执留 pending 重放）")
				oldPathSettleFailed = true
			}
		}
	}

	if !oldPathSettleFailed {
		if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
			logger.Scene("publication").With("pageId", page.ID).With("receiptId", receiptID).
				Error(cerr, "发布回执结案失败（状态已一致，收敛例程会幂等收尾）")

			s.NotifyPendingReceipt()
		}
	}
	logger.Scene("publication").With("pageId", page.ID).With("hash", hash).Info("发布完成")

	siteLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		logger.Scene("publication").With("pageId", page.ID).
			Error(lerr, "站点语言清单不可读，跳过 sitemap/robots 刷新（保留上一版站点文件）")
	} else if rerr := s.routes.RefreshSiteFiles(ctx, page.ProjectID, siteBaseURL(), pipeline.ActiveRoot(),
		siteLangs, s.defaultLocaleOf(ctx, page.ProjectID),

		string(pipeline.SiteLangURLModeOf(ctx, s.project, page.ProjectID)),
		s.notFoundHTMLOf(ctx, page.ProjectID)); rerr != nil {
		logger.Scene("publication").With("pageId", page.ID).Error(rerr, "sitemap/robots 刷新失败")
	}
	if oldPath != path {
		s.invalidateActionTarget(ctx, page.ID)
	}
	s.notifyIndexNow(ctx, page.ProjectID, path)

	if refreshPeers {
		s.refreshPeerLocaleLinks(ctx, page, plan, lang)
	}
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StatePublished, ActiveHash: hash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

func (s *Service) refreshPeerLocaleLinks(ctx context.Context, page *pagemodel.PageEntity, plan pipeline.PublicationPlan, lang string) {
	if s == nil || page == nil || ctx.Err() != nil {
		return
	}

	if len(plan.SiteLangs) <= 1 || len(plan.SiteLangs) > maxCrossLinkRefreshLangs {
		return
	}
	pubs, err := s.model.ListPublications(ctx, page.ID)
	if err != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Error(err, "互指刷新跳过：读取本页发布状态失败")
		return
	}

	published := make(map[string]string, len(pubs))
	for i := range pubs {
		if pubs[i].ActivePath != "" && planHasLang(plan, pubs[i].Lang) {
			published[pubs[i].Lang] = pubs[i].ArtifactHash
		}
	}
	if len(published) <= 1 {
		return
	}
	refreshed := make([]string, 0, len(published)-1)
	for _, peer := range plan.SiteLangs {
		if ctx.Err() != nil {
			break
		}
		if peer == lang {
			continue
		}
		activeHash, ok := published[peer]
		if !ok {

			continue
		}

		if s.refreshPeerLocaleLink(ctx, page, peer, activeHash) {
			refreshed = append(refreshed, peer)
		}
	}
	if len(refreshed) > 0 {

		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("refreshed", strings.Join(refreshed, ",")).
			Info("互指刷新完成：其余已发布语言已重建为与新发布面互指")
	}
}

const maxCrossLinkRefreshLangs = 32

func (s *Service) refreshPeerLocaleLink(ctx context.Context, page *pagemodel.PageEntity, peerLang, activeHash string) bool {

	peerPlan, ok := s.frozenPublicationPlan(ctx, page, peerLang)
	if !ok {
		return false
	}

	staging, serr := s.model.GetStaging(ctx, page.ID, peerLang)
	if serr != nil || staging == nil || staging.DraftVersion != page.DraftVersion {
		return false
	}
	path, perr := s.sitePathOfWithPlan(ctx, peerLang, page, peerPlan)
	if perr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(perr, "互指刷新跳过：语言访问路径解析失败")
		return false
	}
	if kerr := s.syncKernel(path, peerLang, page.DraftDocument, page.ID, peerPlan); kerr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(kerr, "互指刷新跳过：内核记录同步失败")
		return false
	}
	candidate, berr := s.publisher.Build(ctx, page.ID, s.kernelVersion(page.ID))
	if berr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(berr, "互指刷新跳过：按冻结计划重编译失败")
		return false
	}
	if candidate == activeHash {

		return false
	}
	logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
		With("from", activeHash).With("to", candidate).
		Info("互指刷新：其余语言已上线的这一份重新构建并激活")
	if _, perr = s.publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: peerLang}, false); perr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", peerLang).
			Error(perr, "互指刷新失败（保持原状，等待下一次发布或重建收敛）")
		return false
	}
	return true
}

func (s *Service) notFoundHTMLOf(ctx context.Context, projectID string) string {
	if s.project == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return ""
	}
	return projectcontract.ParseSiteSettings(project.Settings).NotFoundHTML
}

func (s *Service) Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.TargetHash) == "" {
		return nil, ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("targetHash", req.TargetHash).Info("开始回滚")
	targetArt, err := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: req.TargetHash})
	if err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "回滚目标产物缺失")
		return nil, ErrRollbackTargetMiss
	}

	lang := buildLang(req.Lang)
	if strings.TrimSpace(targetArt.Lang) != "" {
		lang = targetArt.Lang
	}
	oldPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}
	if err = s.restoreKernelForHistory(page, targetArt); err != nil {
		return nil, err
	}

	fromArtifactID := s.publishedArtifactIDOf(ctx, page, lang)
	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionRollback,
		ProjectID: page.ProjectID, PageID: page.ID, Path: targetArt.CanonicalPath, Lang: lang,
		FromArtifactID: fromArtifactID, ToArtifactID: targetArt.ID, OldPath: oldPath,
		CapturedInputVersion: page.InputVersion,
		FromReceiptID:        s.capturedFromReceiptID(ctx, page.ProjectID, page.ID, lang),
	})
	if rerr != nil {
		return nil, rerr
	}
	receiptSeq, seqErr := parseReceiptID(receiptID)
	if seqErr != nil {
		s.abortPublishReceipt(ctx, receiptID, "回执 id 无法解析为数值身份")
		return nil, ErrPublishLedgerUnavailable
	}

	now := time.Now().UTC()
	rolledBack := false
	txErr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if _, lerr := s.model.LockPageForActivationTx(ctx, tx, page.ProjectID, page.ID); lerr != nil {
			return lerr
		}

		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if rerr := s.publisher.Rollback(page.ID, req.TargetHash); rerr != nil {
			return rerr
		}
		rolledBack = true

		if aerr := s.applyRollbackTx(ctx, tx, page.ProjectID, pagemodel.PublicationRecord{
			PageID: page.ID, Lang: lang, ActivePath: targetArt.CanonicalPath,
			ArtifactID: targetArt.ID, ArtifactHash: targetArt.ArtifactHash, PublishedAt: now,

			ReceiptID: receiptSeq,
		}, oldPath); aerr != nil {
			return aerr
		}

		if ferr := s.postSwitchFaultHit(); ferr != nil {
			return ferr
		}
		return nil
	})
	if txErr != nil {
		if !rolledBack {
			s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
			logger.Scene("page").With("pageId", page.ID).Error(txErr, "回滚失败（访问面未切换）")
			return nil, mapPublishError(txErr)
		}

		s.keepPublishReceiptPending(receiptID, "DB 激活状态写入失败")
		logger.Scene("page").With("pageId", page.ID).With("hash", req.TargetHash).
			Error(txErr, "回滚 FS 已激活，但 DB 激活状态事务失败（线上已生效，交收敛判定）")
		return nil, fmt.Errorf("回滚已生效但数据库状态同步失败: %w", txErr)
	}

	oldPathSettleFailed := false
	if s.routes != nil && oldPath != "" && oldPath != targetArt.CanonicalPath {
		if derr := s.settleOldPathLocked(ctx, page.ID, page.ProjectID, targetArt.CanonicalPath, oldPath, false); derr != nil {
			if errors.Is(derr, errOldPathTaken) {
				logger.Scene("publication").With("pageId", page.ID).With("oldPath", oldPath).
					Warn("回滚后旧路径已不归本页：不处置（由当前归属者负责）")
			} else {
				s.keepPublishReceiptPending(receiptID, "旧路径访问面处置失败")
				logger.Scene("publication").With("pageId", page.ID).With("oldPath", oldPath).
					Error(derr, "回滚后旧路径处置失败（DB 已一致，回执留 pending 重放）")
				oldPathSettleFailed = true
			}
		}
	}
	if !oldPathSettleFailed {
		if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
			logger.Scene("page").With("pageId", page.ID).With("receiptId", receiptID).
				Error(cerr, "回滚回执结案失败（状态已一致，收敛例程会幂等收尾）")

			s.NotifyPendingReceipt()
		}
	}
	st, _ := s.publisher.Status(page.ID)
	respStatus := pipeline.StatePublished
	if st != nil && st.Status != "" {
		respStatus = st.Status
	}
	logger.Scene("page").With("pageId", page.ID).With("targetHash", req.TargetHash).Info("回滚完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: respStatus, ActiveHash: req.TargetHash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

func (s *Service) stagedArtifactOf(ctx context.Context, page *pagemodel.PageEntity, lang string) (*artifactcontract.ArtifactResp, error) {
	artifactID := ""
	st, err := s.model.GetStaging(ctx, page.ID, lang)
	switch {
	case err == nil && st != nil:
		artifactID = st.ArtifactID
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, err
	}

	if artifactID == "" {
		if page.StagedArtifactID == nil || *page.StagedArtifactID == "" {
			return nil, ErrNoStagedArtifact
		}
		artifactID = *page.StagedArtifactID
	}
	art, derr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: artifactID})
	if derr != nil {

		if strings.Contains(derr.Error(), artifactenums.ErrArtifactNotFound) {
			return nil, ErrNoStagedArtifact
		}
		logger.Scene("publication").With("pageId", page.ID).With("artifactID", artifactID).
			Error(derr, "查询暂存产物失败")
		return nil, derr
	}
	if art.Lang != "" && art.Lang != lang {

		return nil, ErrNoStagedArtifact
	}
	return art, nil
}

func (s *Service) ensureArtifactRow(ctx context.Context, page *pagemodel.PageEntity, hash string, sourceDocument json.RawMessage, lang string) (string, []pipeline.Dependency, error) {
	existing, err := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: hash})
	loc := pipeline.ArtifactLocator(hash)
	art, err := s.store.GetArtifact(loc)
	if err != nil {
		return "", nil, err
	}
	if existing != nil && existing.ID != "" {
		return existing.ID, art.Manifest.Dependencies, nil
	}
	manifestJSON, err := json.Marshal(art.Manifest)
	if err != nil {
		return "", nil, err
	}
	recorded, err := s.artifacts.EnsureRecord(ctx, &artifactcontract.RecordReq{
		ArtifactID:       uuid.NewString(),
		PageID:           page.ID,
		Version:          page.DraftVersion,
		Lang:             lang,
		SourceDocument:   sourceDocument,
		SchemaVersion:    art.Manifest.PageDocumentSchemaVersion,
		SourceHash:       art.Manifest.SourceHash,
		BuildInputHash:   art.Manifest.BuildInputHash,
		ArtifactProvider: "local",
		ArtifactKey:      loc.Key,
		ArtifactHash:     hash,
		CompilerVersion:  art.Manifest.CompilerVersion,

		RegistryVersion: builder.RegistryVersion(),
		Manifest:        manifestJSON,
		CreatedBy:       systemCreator,
	})
	if err != nil {
		return "", nil, err
	}
	return recorded.ID, art.Manifest.Dependencies, nil
}

func activeHashOf(rec *pipeline.PageRecord) string {
	if rec == nil {
		return ""
	}
	return rec.ActiveHash
}

func mapPublishError(err error) error {
	switch {
	case errors.Is(err, pipeline.ErrVersionConflict):
		return ErrDraftVersionConflict
	case errors.Is(err, pipeline.ErrNoStagedArtifact):
		return ErrNoStagedArtifact
	case errors.Is(err, pipeline.ErrRollbackPathMismatch):
		return ErrRebuildRequired
	case errors.Is(err, pipeline.ErrPageNotFound):
		return ErrPageNotFound
	default:
		return err
	}
}

func (s *Service) syncKernel(path, lang string, doc json.RawMessage, pageID string, plan *pipeline.PublicationPlan) error {
	draft := pipeline.Draft{Path: path, Lang: lang, DocJSON: doc, Plan: plan}
	st, err := s.publisher.Status(pageID)
	if errors.Is(err, pipeline.ErrPageNotFound) {
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	if err != nil {
		return err
	}
	if st.Path != path || st.Lang != lang {

		s.publisher.LoadRecord(&pipeline.PageRecord{ID: pageID})
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	_, err = s.publisher.SaveDraftInput(pageID, st.Version, draft)
	return err
}

func (s *Service) restoreKernelForHistory(page *pagemodel.PageEntity, target *artifactcontract.ArtifactResp) error {
	doc := page.DraftDocumentFor(target.SourceDocument)
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: target.CanonicalPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc, Plan: publicationPlanFromManifest(target.Manifest),
		Histories: []*pipeline.HistoryEntry{{
			Hash: target.ArtifactHash, Path: target.CanonicalPath,
			Status: pipeline.StateSuperseded, Order: 1,
		}},
	}
	s.publisher.LoadRecord(rec)
	return nil
}

func (s *Service) restoreKernelForUpdate(ctx context.Context, page *pagemodel.PageEntity, publishedPath string, plan *pipeline.PublicationPlan) error {
	doc := page.DraftDocument
	activeHash := ""
	histories := []*pipeline.HistoryEntry{}
	if page.ActiveArtifactID != nil && *page.ActiveArtifactID != "" {
		art, err := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: *page.ActiveArtifactID})
		if err != nil {

			return fmt.Errorf("页面活动产物缺失（artifact_id=%s），无法修改 URL: %w", *page.ActiveArtifactID, err)
		}
		activeHash = art.ArtifactHash
		histories = append(histories, &pipeline.HistoryEntry{
			Hash: art.ArtifactHash, Path: art.CanonicalPath,
			Status: pipeline.StatePublished, Order: 1,
		})
		doc = page.DraftDocumentFor(art.SourceDocument)
	}
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: publishedPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc, ActiveHash: activeHash, Histories: histories,
		Plan: plan,
	}
	s.publisher.LoadRecord(rec)
	return nil
}

func publicationPlanFromManifest(raw []byte) *pipeline.PublicationPlan {
	if len(raw) == 0 {
		return nil
	}
	var m pipeline.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	plan := pipeline.PublicationPlan{SiteLangs: m.SiteLangs, DefaultLang: m.SiteDefaultLang, References: m.References.Clone()}
	if plan.Empty() {
		return nil
	}
	n := plan.Normalize()
	return &n
}

func (s *Service) kernelVersion(pageID string) int {
	if st, err := s.publisher.Status(pageID); err == nil && st.Version > 0 {
		return st.Version
	}
	return 1
}

func pageHasPublishedState(rec *pipeline.PageRecord) bool {
	if rec == nil {
		return false
	}
	if rec.ActiveHash != "" {
		return true
	}
	for _, h := range rec.Histories {
		if h.Status == pipeline.StatePublished {
			return true
		}
	}
	return false
}

func (s *Service) kernelVersionOrOne(pageID string) int { return s.kernelVersion(pageID) }

func (s *Service) frozenPublicationPlan(ctx context.Context, page *pagemodel.PageEntity, lang string) (*pipeline.PublicationPlan, bool) {
	if s.model == nil || page == nil || strings.TrimSpace(lang) == "" {
		return nil, false
	}
	row, err := s.model.GetPublicationPlan(ctx, page.ID, lang)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				Error(err, "发布计划读取失败：本次按当前站点语言配置解析并重新冻结")
		}
		return nil, false
	}
	var plan pipeline.PublicationPlan
	if uerr := json.Unmarshal(row.Plan, &plan); uerr != nil {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			Error(uerr, "发布计划解析失败：本次按当前站点语言配置解析并重新冻结")
		return nil, false
	}
	if plan.Empty() {
		return nil, false
	}
	if row.DraftVersion != page.DraftVersion {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("planDraftVersion", row.DraftVersion).With("draftVersion", page.DraftVersion).
			Info("草稿已更新：按当前站点语言配置重新冻结发布计划")
		return nil, false
	}

	if planHasExcludedLang(plan, page.ExcludedLangs) {
		logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
			With("excluded", strings.Join(page.ExcludedLangs, ",")).
			Info("本页排除了计划里包含的语言：按当前产出范围重新冻结发布计划")
		return nil, false
	}
	return &plan, true
}

func (s *Service) publicationPlanFor(ctx context.Context, page *pagemodel.PageEntity, lang string) (pipeline.PublicationPlan, bool, error) {
	if frozen, ok := s.frozenPublicationPlan(ctx, page, lang); ok {

		s.warnPlanDrift(ctx, page, lang, *frozen)
		return *frozen, false, nil
	}
	inputs, err := pipeline.ResolveSiteLangInputs(ctx, s.project, page.ProjectID, pipeline.LangFallbackForbidden)
	if err != nil {
		return pipeline.PublicationPlan{}, false, err
	}

	inputs = dropExcludedLangs(inputs, page.ExcludedLangs)
	plan := pipeline.PlanOfSiteLangInputs(inputs)
	if plan.Empty() {
		return pipeline.PublicationPlan{}, false, pipeline.ErrLangTableUnavailable
	}

	logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
		With("planHash", plan.Hash()).Info("按当前站点语言配置冻结发布计划")
	return plan, true, nil
}

func planHasLang(plan pipeline.PublicationPlan, lang string) bool {
	for _, l := range plan.SiteLangs {
		if l == lang {
			return true
		}
	}
	return false
}

func (s *Service) warnPlanDrift(ctx context.Context, page *pagemodel.PageEntity, lang string, plan pipeline.PublicationPlan) {
	if s == nil || s.project == nil || page == nil || plan.Empty() {
		return
	}
	current, err := s.project.EnabledLangs(ctx, page.ProjectID)
	if err != nil || len(current) == 0 {
		return
	}

	current = dropExcludedLangs(pipeline.SiteLangInputs{SiteLangs: current}, page.ExcludedLangs).SiteLangs
	added, removed := langSetDiff(plan.SiteLangs, current)
	if added == nil && removed == nil {
		return
	}
	sc := logger.Scene("publication")
	ev := sc.With("pageId", page.ID).With("lang", lang).
		With("frozen", strings.Join(plan.SiteLangs, ",")).
		With("current", strings.Join(current, ","))

	if len(added) > 0 {
		ev.With("added", strings.Join(added, ",")).
			Info("站点语言清单新增了语言，但本页的冻结发布计划不含它：既有产物按冻结值发布，新语言需在草稿改动后的重新发布中生效")
	}
	if len(removed) > 0 {
		ev.With("removed", strings.Join(removed, ",")).
			Warn("站点语言清单已移除本页冻结计划里的语言：既有产物仍按冻结值声明互指，请确认该语言的路由已下线或安排重新发布")
	}
}

func langSetDiff(frozen, current []string) (added, removed []string) {
	frozenSet := make(map[string]bool, len(frozen))
	for _, l := range frozen {
		frozenSet[strings.TrimSpace(l)] = true
	}
	currentSet := make(map[string]bool, len(current))
	for _, l := range current {
		currentSet[strings.TrimSpace(l)] = true
	}
	for _, l := range current {
		if l = strings.TrimSpace(l); l != "" && !frozenSet[l] {
			added = append(added, l)
		}
	}
	for _, l := range frozen {
		if l = strings.TrimSpace(l); l != "" && !currentSet[l] {
			removed = append(removed, l)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return nil, nil
	}
	return added, removed
}

func (s *Service) sitePathOf(ctx context.Context, lang string, page *pagemodel.PageEntity) (string, error) {
	return s.sitePathOfWithPlan(ctx, lang, page, nil)
}

func (s *Service) sitePathOfWithPlan(ctx context.Context, lang string, page *pagemodel.PageEntity, plan *pipeline.PublicationPlan) (string, error) {
	rule := s.langURLRuleOf(ctx, page.ProjectID)
	if plan != nil && strings.TrimSpace(plan.DefaultLang) != "" {
		rule = pipeline.LangURLRuleForProjectWithDefault(ctx, s.project, page.ProjectID, plan.DefaultLang)
	}
	path, err := sitePath(rule, lang, page.DraftPath)
	if err != nil {
		return "", ErrInvalidPath
	}
	return path, nil
}

func (s *Service) UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}

	newPath, err := normalizePagePath(req.NewPath)
	if err != nil {
		return nil, err
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err == nil {
			s.invalidateActionTarget(ctx, page.ID)
		}
	}()

	routeLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		logger.Scene("page").With("pageId", page.ID).
			Error(lerr, "改 URL 中止：站点语言清单不可读，不带着不完整的语言集合去切访问面")
		return nil, lerr
	}
	lang := buildLang(req.Lang)
	rule := s.langURLRuleOf(ctx, page.ProjectID)
	kernelNewPath, err := sitePath(rule, lang, newPath)
	if err != nil {
		return nil, ErrInvalidPath
	}
	oldPath := page.DraftPathValue()
	oldRoutePath, err := sitePath(rule, lang, oldPath)
	if err != nil {
		return nil, ErrInvalidPath
	}

	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, kernelNewPath, page.ID); err != nil {
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).Warn("改 URL 被拒绝：新路径已被占用")
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("lang", lang).With("newPath", kernelNewPath).Info("开始修改 URL")

	publishedPath, perr := s.publishedPathOf(ctx, page, lang)
	if perr != nil {
		return nil, perr
	}
	if publishedPath == "" {
		publishedPath = oldRoutePath
	}

	if publishedPath != "" {

		var owner *pubcontract.RouteOwnerResp
		oerr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
			row, rerr := s.routes.RouteOwnerTx(ctx, tx, page.ProjectID, publishedPath)
			owner = row
			return rerr
		})
		switch {
		case errors.Is(oerr, gorm.ErrRecordNotFound):

		case oerr != nil:
			return nil, oerr
		case owner != nil && owner.RouteKind != publisherRouteKindReserved &&
			(owner.PageID == nil || *owner.PageID != page.ID):
			logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).
				Warn("改 URL 被拒绝：旧路径已被其它实体接管，不能在本页名下处置")
			return nil, pubenums.Err(pubenums.ErrRouteActiveRename)
		}
	}

	plan, persistPlan, err := s.publicationPlanFor(ctx, page, lang)
	if err != nil {
		return nil, err
	}

	if err = s.restoreKernelForUpdate(ctx, page, publishedPath, &plan); err != nil {
		return nil, err
	}
	stBefore, _ := s.publisher.Status(page.ID)

	if !pageHasPublishedState(stBefore) {
		if _, err = s.publisher.UpdateURL(ctx, page.ID, kernelNewPath, req.WithRedirect); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改失败")
			return nil, mapPublishError(err)
		}
		now := time.Now().UTC()
		if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
			if merr := s.model.MoveDraftPathTx(ctx, tx, page.ProjectID, page.ID, newPath, now); merr != nil {
				return merr
			}
			return s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
				ProjectID: page.ProjectID, PageID: page.ID,
				OldLogical: oldPath, NewLogical: newPath, TargetLang: lang, Langs: routeLangs,
			})
		}); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "纯草稿路径迁移失败")
			return nil, err
		}
		st, _ := s.publisher.Status(page.ID)
		status := pipeline.StateDraft
		if st != nil && st.Status != "" {
			status = st.Status
		}
		logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).With("newPath", newPath).
			Info("纯草稿 URL 修改完成（仅迁移路径与保留路由）")
		return &pagedto.PublishResp{
			PageID: page.ID, Status: status, DraftPath: newPath,
			PublishedAt: now.Format(time.RFC3339),
		}, nil
	}

	receiptID, rerr := s.beginPublishReceipt(ctx, publishReceiptInput{
		Action:    pubcontract.ReceiptActionUpdateURL,
		ProjectID: page.ProjectID, PageID: page.ID,
		Path: kernelNewPath, Lang: lang,
		FromArtifactID: s.publishedArtifactIDOf(ctx, page, lang),
		OldPath:        publishedPath,
		Redirect:       req.WithRedirect,

		CapturedInputVersion: page.InputVersion,
		FromReceiptID:        s.capturedFromReceiptID(ctx, page.ProjectID, page.ID, lang),
	})
	if rerr != nil {
		return nil, rerr
	}
	receiptSeq, seqErr := parseReceiptID(receiptID)
	if seqErr != nil {
		s.abortPublishReceipt(ctx, receiptID, "回执 id 无法解析为数值身份")
		return nil, ErrPublishLedgerUnavailable
	}

	var st *pipeline.PageRecord
	var artifactRowID string
	var deps []pipeline.Dependency
	var updated bool
	var txErr error
	lockErr := s.withPaths([]string{kernelNewPath, publishedPath}, func() error {

		if _, uerr := s.publisher.UpdateURLInLock(ctx, page.ID, kernelNewPath, req.WithRedirect); uerr != nil {

			s.abortPublishReceipt(ctx, receiptID, "访问面切换失败")
			logger.Scene("page").With("pageId", page.ID).Error(uerr, "URL 修改失败")
			return uerr
		}
		st, _ = s.publisher.Status(page.ID)

		var aerr error
		artifactRowID, deps, aerr = s.ensureArtifactRow(ctx, page, activeHashOf(st), st.DocumentJSON, lang)
		if aerr != nil {

			s.keepPublishReceiptPending(receiptID, "产物归档失败")
			logger.Scene("page").With("pageId", page.ID).With("hash", activeHashOf(st)).Error(aerr, "URL 修改后产物归档失败")
			return fmt.Errorf("产物归档失败: %w", aerr)
		}
		updated, txErr = s.updateURLCommitUnderLock(ctx, page, lang, st, kernelNewPath, newPath, oldPath,
			publishedPath, req.WithRedirect, routeLangs, plan, persistPlan, receiptID, receiptSeq, artifactRowID, deps)
		return nil
	})
	if lockErr != nil {
		return nil, mapPublishError(lockErr)
	}

	if txErr != nil {
		if handled := s.settleUpdateURLFailure(ctx, page, lang, kernelNewPath, receiptID, updated, txErr); handled != nil {
			return nil, handled
		}
	}

	oldPathSettleFailed := false
	if derr := s.settleOldPathLocked(ctx, page.ID, page.ProjectID, kernelNewPath, publishedPath, req.WithRedirect); derr != nil {
		if errors.Is(derr, errOldPathTaken) {
			logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).
				Warn("改 URL 完成后旧路径已不归本页：不处置（由当前归属者负责）")
		} else {
			s.keepPublishReceiptPending(receiptID, "旧路径访问面处置失败")
			logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).
				Error(derr, "改 URL 后旧路径处置失败（DB 已一致，回执留 pending 重放）")

			oldPathSettleFailed = true
		}
	}
	if !oldPathSettleFailed {
		if cerr := s.completePublishReceipt(ctx, receiptID); cerr != nil {
			logger.Scene("page").With("pageId", page.ID).With("receiptId", receiptID).
				Error(cerr, "改 URL 回执结案失败（状态已一致，收敛例程会幂等收尾）")

			s.NotifyPendingReceipt()
		}
	}
	status := pipeline.StatePublished
	if st != nil && st.Status != "" {
		status = st.Status
	}
	logger.Scene("page").With("pageId", page.ID).With("oldPath", publishedPath).With("newPath", newPath).Info("URL 修改完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: status, ActiveHash: activeHashOf(st),
		OldPath: publishedPath, DraftPath: newPath,
		PublishedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (s *Service) publishedPathOf(ctx context.Context, page *pagemodel.PageEntity, lang string) (string, error) {
	pub, err := s.model.GetPublication(ctx, page.ID, lang)
	if err == nil && pub != nil {
		return pub.ActivePath, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if !i18n.SiteLangURLsSeparated(pipeline.SiteLangURLModeOf(ctx, s.project, page.ProjectID)) {
		return page.ActivePathValue(), nil
	}
	return "", nil
}

func (s *Service) updateURLCommitUnderLock(ctx context.Context, page *pagemodel.PageEntity, lang string, st *pipeline.PageRecord, kernelNewPath, newPath, oldPath, publishedPath string, withRedirect bool, routeLangs []string, plan pipeline.PublicationPlan, persistPlan bool, receiptID string, receiptSeq int64, artifactRowID string, deps []pipeline.Dependency) (updated bool, txErr error) {

	activationPlan := (*pipeline.PublicationPlan)(nil)
	if persistPlan {
		activationPlan = &plan
	}

	txErr = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		snap, lerr := s.model.LockPageForActivationTx(ctx, tx, page.ProjectID, page.ID)
		if lerr != nil {
			return lerr
		}

		withdrawArt, withdrawHash, ok := s.updateURLArtifactAt(kernelNewPath)
		if !ok || withdrawHash != activeHashOf(st) {
			return errUpdateURLWithdrawn
		}

		if publishedPath != "" && publishedPath != kernelNewPath && s.activeArtifactHashAt(publishedPath) != "" &&
			s.activeKindAt(publishedPath) != pipeline.PublicationRedirect && publishedPath != withdrawArt.CanonicalPath {
			return errUpdateURLWithdrawn
		}
		if snap.InputVersion != page.InputVersion {
			logger.Scene("publication").With("pageId", page.ID).With("lang", lang).
				With("captured", page.InputVersion).With("current", snap.InputVersion).
				Warn("改 URL 放弃：捕获的输入代次已过期（期间内容或依赖变更）")
			return ErrInputMoved
		}
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}

		if aerr := s.applyUpdateURL(ctx, tx, updateURLApplyInput{
			Page: page, ArtifactRowID: artifactRowID, ArtifactHash: activeHashOf(st), Lang: lang,
			KernelNewPath: kernelNewPath, NewLogicalPath: newPath,
			OldLogicalPath: oldPath, OldKernelPath: publishedPath,
			WithRedirect: withRedirect, Deps: deps, RouteLangs: routeLangs,
			Plan: activationPlan, ReceiptID: receiptSeq,
		}); aerr != nil {
			return aerr
		}
		updated = true
		return nil
	})
	return updated, txErr
}

func (s *Service) settleUpdateURLFailure(ctx context.Context, page *pagemodel.PageEntity, lang, kernelNewPath, receiptID string, updated bool, txErr error) error {
	switch {
	case errors.Is(txErr, errUpdateURLWithdrawn):

		s.keepPublishReceiptPending(receiptID, "新路径访问面已被别的激活替代")
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).
			Warn("改 URL 放弃：锁内发现新路径上的产物已不是本次激活的那一份")
	case !updated && errors.Is(txErr, ErrInputMoved):

		s.abortPublishReceipt(ctx, receiptID, "捕获的输入代次已过期")
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).
			Error(txErr, "URL 修改放弃（输入已变，未落定数据库）")
	case !updated:

		s.keepPublishReceiptPending(receiptID, "改 URL 提交段失败")
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).
			Error(txErr, "URL 修改失败（数据库未落定，回执保持 pending 交收敛重放）")
	default:

		s.keepPublishReceiptPending(receiptID, "DB 状态写入失败")
		logger.Scene("page").With("pageId", page.ID).With("newPath", kernelNewPath).
			Error(txErr, "URL 修改 FS 已切换，但 DB 事务失败（线上已生效，恢复会补齐）")
	}
	if errors.Is(txErr, ErrInputMoved) || errors.Is(txErr, errUpdateURLWithdrawn) {
		return mapPublishError(txErr)
	}
	if updated {
		return fmt.Errorf("URL 修改已生效但数据库状态同步失败: %w", txErr)
	}
	return mapPublishError(txErr)
}

func (s *Service) ensureRouteNotOccupied(ctx context.Context, projectID, path, selfPageID string) error {
	if s.routes == nil {
		return nil
	}
	occupied, err := s.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: projectID, Path: path, ExcludePageID: selfPageID,
	})
	if err != nil {
		return err
	}
	if occupied {
		return ErrPathOccupied
	}
	return nil
}

func siteBaseURL() string {
	return strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
}

func (s *Service) SetExternalArtifactOwners(provider func(ctx context.Context) ([]string, error)) {
	s.externalArtifactOwners = provider
}

func (s *Service) auditOrphanArtifacts(ctx context.Context) (orphans []pagedto.OrphanArtifactResp, checked int, err error) {
	if s == nil || s.store == nil {
		return nil, 0, nil
	}
	owners := []pipeline.OwnerProvider{
		func(ctx context.Context) ([]string, error) {
			return s.pageArtifactHashes(ctx)
		},
	}
	if s.externalArtifactOwners != nil {
		owners = append(owners, s.externalArtifactOwners)
	}
	found, n, aerr := pipeline.AuditOrphanArtifacts(ctx, s.store.Root, owners...)
	if aerr != nil {
		return nil, n, aerr
	}
	orphans = make([]pagedto.OrphanArtifactResp, 0, len(found))
	var bytes int64
	for _, o := range found {
		orphans = append(orphans, pagedto.OrphanArtifactResp{Hash: o.Hash, Path: o.Path, Bytes: o.Bytes, Files: o.Files})
		bytes += o.Bytes
	}
	if len(orphans) > 0 {

		logger.Scene("page").With("count", len(orphans)).With("bytes", bytes).
			Info("产物对账发现无人认领的目录（交由产物 GC 按保留期回收）")
	}
	return orphans, n, nil
}

func (s *Service) notifyIndexNow(ctx context.Context, projectID string, paths ...string) {
	if s.project == nil || strings.TrimSpace(projectID) == "" || len(paths) == 0 {
		return
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: projectID})
	if err != nil || project == nil {
		return
	}
	settings := projectcontract.ParseSiteSettings(project.Settings)
	seo.NotifyIndexNowAsync(siteBaseURL(), settings.IndexNowKey, paths)
}

type publishActivationInput struct {
	Page         *pagemodel.PageEntity
	Lang         string
	Path         string
	ArtifactID   string
	ArtifactHash string

	OldPath string

	Plan *pipeline.PublicationPlan
}

func (s *Service) applyPublishActivation(ctx context.Context, in publishActivationInput) error {
	now := time.Now().UTC()
	return s.model.TransactionScoped(ctx, in.Page.ProjectID, func(tx *gorm.DB) error {
		if merr := s.model.MarkPublishedLangTx(ctx, tx, in.Page.ProjectID, pagemodel.PublicationRecord{
			PageID: in.Page.ID, Lang: in.Lang, ActivePath: in.Path,
			ArtifactID: in.ArtifactID, ArtifactHash: in.ArtifactHash, PublishedAt: now,
		}); merr != nil {
			return merr
		}

		if in.Plan != nil {
			if perr := s.model.UpdatePublicationPlanRecordTx(ctx, tx, in.Page.ID, in.Lang, *in.Plan, in.Page.DraftVersion, now); perr != nil {
				return perr
			}
		}

		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if s.routes == nil {
			return nil
		}
		if in.OldPath != "" && in.OldPath != in.Path {
			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: in.Page.ProjectID, Path: in.OldPath, PageID: in.Page.ID,
			}); derr != nil {
				return derr
			}
		}
		_, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.Path, PageID: in.Page.ID, ArtifactID: in.ArtifactID,
		})
		return aerr
	})
}

type updateURLApplyInput struct {
	Page           *pagemodel.PageEntity
	ArtifactRowID  string
	ArtifactHash   string
	Lang           string
	KernelNewPath  string
	NewLogicalPath string

	OldLogicalPath string

	OldKernelPath string
	WithRedirect  bool

	RouteLangs []string

	Deps []pipeline.Dependency

	Plan *pipeline.PublicationPlan

	ReceiptID int64
}

func (s *Service) applyUpdateURL(ctx context.Context, tx *gorm.DB, in updateURLApplyInput) error {
	now := time.Now().UTC()
	{
		if merr := s.model.MoveDraftPathTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.NewLogicalPath, now); merr != nil {
			return merr
		}
		if ferr := s.publishWindowFaultHit(); ferr != nil {
			return ferr
		}
		if merr := s.model.MarkPublishedLangTx(ctx, tx, in.Page.ProjectID, pagemodel.PublicationRecord{
			PageID: in.Page.ID, Lang: in.Lang, ActivePath: in.KernelNewPath,
			ArtifactID: in.ArtifactRowID, ArtifactHash: in.ArtifactHash, PublishedAt: now,
			ReceiptID: in.ReceiptID,
		}); merr != nil {
			return merr
		}

		if len(in.Deps) > 0 {
			if derr := s.persistDependenciesTx(ctx, tx, in.Page.ProjectID, in.Page.ID, in.ArtifactRowID, in.Deps); derr != nil {
				return derr
			}
		}

		if in.Plan != nil {
			if perr := s.model.UpdatePublicationPlanRecordTx(ctx, tx, in.Page.ID, in.Lang, *in.Plan, in.Page.DraftVersion, now); perr != nil {
				return perr
			}
		}
		if s.routes == nil {
			return nil
		}
		if rerr := s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
			ProjectID: in.Page.ProjectID, PageID: in.Page.ID,
			OldLogical: in.OldLogicalPath, NewLogical: in.NewLogicalPath,
			TargetLang: in.Lang, Langs: in.RouteLangs,
		}); rerr != nil {
			return rerr
		}
		if _, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
			ProjectID: in.Page.ProjectID, Path: in.KernelNewPath,
			PageID: in.Page.ID, ArtifactID: in.ArtifactRowID,
		}); aerr != nil {
			return aerr
		}
		if in.OldKernelPath == "" || in.OldKernelPath == in.KernelNewPath {
			return nil
		}
		if in.WithRedirect {

			res, rerr := s.routes.RedirectTx(ctx, tx, &pubcontract.RedirectReq{
				ProjectID: in.Page.ProjectID, OldPath: in.OldKernelPath, PageID: in.Page.ID,
			})
			if rerr != nil {
				return rerr
			}
			if res == nil {
				return fmt.Errorf("%w: %s（redirect 未命中本页占用行）", errOldPathTaken, in.OldKernelPath)
			}
			return nil
		}
		return s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
			ProjectID: in.Page.ProjectID, Path: in.OldKernelPath, PageID: in.Page.ID,
		})
	}
}

func (s *Service) applyRollbackTx(ctx context.Context, tx *gorm.DB, projectID string, rec pagemodel.PublicationRecord, oldPath string) error {
	if merr := s.model.MarkPublishedLangTx(ctx, tx, projectID, rec); merr != nil {
		return merr
	}
	if s.routes == nil {
		return nil
	}
	if oldPath != "" && oldPath != rec.ActivePath {
		if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
			ProjectID: projectID, Path: oldPath, PageID: rec.PageID,
		}); derr != nil {
			return derr
		}
	}
	_, aerr := s.routes.ActivateTx(ctx, tx, &pubcontract.ActivateReq{
		ProjectID: projectID, Path: rec.ActivePath, PageID: rec.PageID, ArtifactID: rec.ArtifactID,
	})
	return aerr
}

func (s *Service) settleOldPathLocked(ctx context.Context, pageID, projectID, newPath, oldPath string, withRedirect bool) error {
	if strings.TrimSpace(oldPath) == "" || oldPath == newPath {
		return nil
	}
	loc, perr := s.prepareOldPathSettle(newPath, oldPath, withRedirect)
	if perr != nil {
		return perr
	}

	txErr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		if _, lerr := s.model.LockPageForActivationTx(ctx, tx, projectID, pageID); lerr != nil {
			return lerr
		}
		return s.withPaths([]string{newPath, oldPath}, func() error {

			ledgerPaths, cerr := s.model.PublicationActivePathsTx(ctx, tx, pageID)
			if cerr != nil {
				return cerr
			}
			stillOurs := false
			for _, p := range ledgerPaths {
				if p == oldPath {
					stillOurs = false
					break
				}
				if p == newPath {
					stillOurs = true
				}
			}
			if !stillOurs {
				logger.Scene("publication").With("pageId", pageID).With("newPath", newPath).
					Warn("旧路径处置复核：账本已不指向本次目标，不处置")
				return errOldPathTaken
			}
			return s.writeOldPathSettleTx(ctx, tx, projectID, pageID, oldPath, withRedirect)
		})
	})
	if txErr != nil {
		return txErr
	}

	if ferr := s.settleOldPathFS(oldPath, withRedirect, loc); ferr != nil {
		return fmt.Errorf("旧路径访问面处置失败 %s: %w", oldPath, ferr)
	}
	return nil
}

func (s *Service) prepareOldPathSettle(newPath, oldPath string, withRedirect bool) (*pipeline.Locator, error) {
	if strings.TrimSpace(oldPath) == "" || oldPath == newPath {
		return nil, nil
	}
	if !withRedirect {
		return nil, nil
	}
	if s.store == nil || s.publication == nil {
		return nil, nil
	}
	ra, rerr := pipeline.NewRedirectArtifact(newPath, 301)
	if rerr != nil {
		return nil, rerr
	}
	loc, perr := s.store.PutRedirect(ra)
	if perr != nil {
		return nil, perr
	}
	return &loc, nil
}

var errOldPathTaken = errors.New("旧路径已被其它归属者占用或改写")

func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

func (s *Service) writeOldPathSettleTx(ctx context.Context, tx *gorm.DB, projectID, pageID, oldPath string,
	withRedirect bool) error {
	if strings.TrimSpace(oldPath) == "" {
		return nil
	}
	return s.settleRouteWrite(ctx, tx, projectID, pageID, oldPath, withRedirect)
}

func (s *Service) settleRouteWrite(ctx context.Context, tx *gorm.DB, projectID, pageID, oldPath string, withRedirect bool) error {
	if s.routes != nil {
		if withRedirect {

			res, rerr := s.routes.RedirectTx(ctx, tx, &pubcontract.RedirectReq{
				ProjectID: projectID, OldPath: oldPath, PageID: pageID,
			})
			if isRouteOccupiedErr(rerr) {
				return fmt.Errorf("%w: %s（redirect 未命中本页占用行，已归他人）", errOldPathTaken, oldPath)
			}
			if rerr != nil {
				return rerr
			}
			if res == nil {
				return fmt.Errorf("%w: %s（redirect 未命中本页占用行）", errOldPathTaken, oldPath)
			}
		} else {

			owned, oerr := s.routes.ListActivePaths(ctx, &pubcontract.ListActivePathsReq{
				ProjectID: projectID, PageID: pageID,
			})
			if oerr != nil {
				return oerr
			}
			if !containsPath(owned, oldPath) {
				return fmt.Errorf("%w: %s", errOldPathTaken, oldPath)
			}
			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: projectID, Path: oldPath, PageID: pageID,
			}); derr != nil {
				return derr
			}
		}
	}
	return nil
}

func isRouteOccupiedErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), pubenums.ErrRouteOccupied)
}

func (s *Service) settleOldPathFS(oldPath string, withRedirect bool, loc *pipeline.Locator) error {
	if !withRedirect {
		if s.publication == nil {
			return nil
		}
		if derr := s.publication.Deactivate(oldPath); derr != nil {
			return fmt.Errorf("解除访问面激活失败 %s: %w", oldPath, derr)
		}
		return nil
	}
	if loc == nil || s.publication == nil {
		return nil
	}
	if aerr := s.publication.Activate(oldPath, *loc); aerr != nil {
		return aerr
	}
	logger.Scene("page").With("oldPath", oldPath).Info("旧路径的 301 已落盘并激活")
	return nil
}

func (s *Service) InspectPublicationPath(path string) (string, bool) {
	if s == nil || s.publication == nil {
		return "", false
	}
	st, err := s.publication.Inspect(path)
	if err != nil || st == nil || st.Kind == pipeline.PublicationNone || st.Locator == nil {
		return "", false
	}
	return strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key), true
}

func (s *Service) activeKindAt(path string) string {
	if s == nil || s.publication == nil {
		return ""
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil {
		return ""
	}
	return state.Kind
}

func (s *Service) activeArtifactHashAt(path string) string {
	if s == nil || s.publication == nil {
		return ""
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Locator == nil {
		return ""
	}
	return strings.TrimPrefix(state.Locator.Key, "artifacts/")
}

func (s *Service) updateURLArtifactAt(path string) (*pipeline.Artifact, string, bool) {
	if s == nil || s.publication == nil || s.store == nil {
		return nil, "", false
	}
	state, err := s.publication.Inspect(path)
	if err != nil || state == nil || state.Kind != pipeline.PublicationPage || state.Locator == nil {
		return nil, "", false
	}
	art, aerr := s.store.GetArtifact(*state.Locator)
	if aerr != nil || art == nil || art.CanonicalPath != path {
		return nil, "", false
	}
	hash := strings.TrimPrefix(state.Locator.Key, "artifacts/")
	if hash == "" {
		hash = art.Hash
	}
	return art, hash, true
}

func (s *Service) ensureUpdateURLArtifactRow(ctx context.Context, page *pagemodel.PageEntity, hash, fromArtifactID, lang string) (string, []pipeline.Dependency, error) {
	if strings.TrimSpace(hash) == "" {
		return "", nil, errors.New("改 URL 恢复：产物 hash 为空")
	}
	if existing, derr := s.artifacts.Detail(ctx, &artifactcontract.DetailReq{PageID: page.ID, Hash: hash}); derr == nil && existing != nil && existing.ID != "" {
		return existing.ID, nil, nil
	}
	source := json.RawMessage(nil)
	if id := strings.TrimSpace(fromArtifactID); id != "" {
		if old, oerr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: id}); oerr == nil && old != nil {
			source = old.SourceDocument
		}
	}
	if len(source) == 0 {
		source = page.DraftDocument
		logger.Scene("page").With("pageId", page.ID).With("hash", hash).
			Warn("改 URL 恢复：取不到活动产物的冻结源文档，回退当前草稿归档（该产物行的 source_document 可能与产物字节不符）")
	}
	rowID, deps, aerr := s.ensureArtifactRow(ctx, page, hash, source, lang)
	if aerr != nil {
		return "", nil, aerr
	}

	return rowID, deps, nil
}

func (s *Service) recoverSwitchActiveReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {

	expectedVersion := item.CapturedInputVersion
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	expectedHash, herr := s.pageArtifactHashByID(ctx, item.ToArtifactID)
	if herr != nil || expectedHash == "" {

		s.abortPublishReceipt(ctx, item.ID, "读取回执产物失败")
		return false, nil
	}

	if s.activeArtifactHashAt(item.Path) != expectedHash {
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("expected", expectedHash).Warn("未结案的发布回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次产物")
		return false, nil
	}
	lang := buildLang(item.Lang)

	ownSeq, seqErr := parseReceiptID(item.ID)
	if seqErr != nil {
		return false, seqErr
	}
	converged, superseded, expired := false, false, false

	var versionAtLock int64
	txErr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {

		snap, lerr := s.model.LockPageForActivationTx(ctx, tx, page.ProjectID, page.ID)
		if lerr != nil {
			return lerr
		}

		curID, cerr := s.model.PublicationArtifactIDTx(ctx, tx, page.ID, lang)
		if cerr != nil {
			return cerr
		}
		curSeq, serr := s.model.PublicationReceiptIDTx(ctx, tx, page.ID, lang)
		if serr != nil {
			return serr
		}

		if s.activeArtifactHashAt(item.Path) != expectedHash {
			superseded = true
			return nil
		}
		if curID != "" && curID != item.FromArtifactID {

			superseded = true
			return nil
		}

		switch receiptIdentityVerdict(item.FromReceiptID, ownSeq, curSeq) {
		case identitySuperseded:
			logger.Scene("publication").With("receiptId", item.ID).With("pageId", page.ID).
				With("ledgerReceiptId", curSeq).With("fromReceiptId", item.FromReceiptID).
				Warn("账本上的成功身份属于另一条回执：本次判定为已被替代")
			superseded = true
			return nil
		}

		versionAtLock = snap.InputVersion
		if expectedVersion > 0 && snap.InputVersion != expectedVersion {

			expired = true
			return nil
		}
		written, werr := s.model.WritePageForwardCompletionTx(ctx, tx, page.ProjectID, pagemodel.PublicationRecord{
			PageID: page.ID, Lang: lang, ActivePath: item.Path,
			ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash, PublishedAt: time.Now().UTC(),

			ReceiptID: ownSeq,
		}, snap.InputVersion, item.FromArtifactID)
		if werr != nil {
			return werr
		}
		if !written {

			superseded = true
			return nil
		}

		converged = true
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	if superseded {
		logger.Scene("publication").With("receiptId", item.ID).With("pageId", page.ID).
			Warn("发布回执判定为已被替代：不写数据库，只结案")
		s.abortPublishReceipt(ctx, item.ID, "本次激活已被替代")
		return false, nil
	}
	if expired || !converged {

		if s.enqueueStalePageRebuild(ctx, page) {
			logger.Scene("publication").With("receiptId", item.ID).With("pageId", page.ID).
				With("captured", expectedVersion).With("actual", versionAtLock).
				Warn("发布回执的产物已过期（own 链接仍在线），已入队重建")
			s.abortPublishReceipt(ctx, item.ID, "产物已过期，已入队重建")
			return false, nil
		}
		s.keepPublishReceiptPending(item.ID, "产物已过期但未入队，等待重建收敛")
		return false, nil
	}

	if derr := s.settleOldPathLocked(ctx, page.ID, page.ProjectID, item.Path, item.OldPath, false); derr != nil && !errors.Is(derr, errOldPathTaken) {
		s.keepPublishReceiptPending(item.ID, "旧路径访问面处置失败")
		logger.Scene("publication").With("receiptId", item.ID).With("oldPath", item.OldPath).
			Error(derr, "收敛补齐后旧路径处置失败（回执留 pending 重放）")

		return false, nil
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	logger.Scene("publication").With("pageId", page.ID).With("path", item.Path).
		Info("发布在崩溃前已生效，已在实体锁内补齐数据库状态")
	return true, nil
}

func (s *Service) enqueueStalePageRebuild(ctx context.Context, page *pagemodel.PageEntity) bool {
	if s == nil || page == nil {
		return false
	}
	if s.buildQueue == nil {
		logger.Scene("publication").With("pageId", page.ID).
			Warn("收敛入队失败：构建队列未接入，页面保持 stale 等待后续触发")
		return false
	}
	inputHash := hash(page.DraftDocument)
	if qerr := s.buildQueue.EnqueuePageBuild(ctx, page.ID, page.ProjectID, "",
		pagecontract.BuildIntentDependency, page.DraftVersion, inputHash); qerr != nil {
		logger.Scene("publication").With("pageId", page.ID).
			Error(qerr, "收敛入队失败：重建任务未排上队")
		return false
	}
	return true
}

func (s *Service) ledgerReceiptIDOf(ctx context.Context, projectID, pageID, lang string) (int64, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(pageID) == "" {
		return 0, nil
	}
	var seq int64
	terr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		cur, cerr := s.model.PublicationReceiptIDTx(ctx, tx, pageID, lang)
		if cerr != nil {
			return cerr
		}
		seq = cur
		return nil
	})
	return seq, terr
}

func (s *Service) recoverUpdateURLReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	_, hash, ok := s.updateURLArtifactAt(item.Path)
	if !ok {

		seq, seqErr := parseReceiptID(item.ID)
		if seqErr != nil {
			return false, seqErr
		}
		cur, cerr := s.ledgerReceiptIDOf(ctx, item.ProjectID, item.SourceID, buildLang(item.Lang))
		if cerr != nil {
			return false, cerr
		}
		if cur == 0 || cur == seq {
			s.keepPublishReceiptPending(item.ID, "新路径暂无产物，且账本尚未记入其它回执身份")
			logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
				Warn("改 URL 回执：新路径暂无产物但账本未前进，保留 pending 交下一轮")
			return false, nil
		}
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("ledgerReceiptId", cur).Warn("未结案的改 URL 回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向本次改 URL 的新产物")
		return false, nil
	}
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	lang := buildLang(item.Lang)
	itemSeq, seqErr := parseReceiptID(item.ID)
	if seqErr != nil {
		return false, seqErr
	}

	rowID, deps, aerr := s.ensureUpdateURLArtifactRow(ctx, page, hash, item.FromArtifactID, lang)
	if aerr != nil {
		return false, aerr
	}
	rule := s.langURLRuleOf(ctx, page.ProjectID)

	routeLangs, lerr := s.publishLangsOf(ctx, page.ProjectID)
	if lerr != nil {
		return false, lerr
	}

	var superseded, expired bool
	var versionAtLock int64
	txErr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		snap, lerr := s.model.LockPageForActivationTx(ctx, tx, page.ProjectID, page.ID)
		if lerr != nil {
			return lerr
		}
		versionAtLock = snap.InputVersion

		_, curHash, live := s.updateURLArtifactAt(item.Path)
		if !live || curHash != hash {
			superseded = true
			return nil
		}

		curID, cerr := s.model.PublicationArtifactIDTx(ctx, tx, page.ID, lang)
		if cerr != nil {
			return cerr
		}
		curSeq, serr := s.model.PublicationReceiptIDTx(ctx, tx, page.ID, lang)
		if serr != nil {
			return serr
		}
		if curID != "" && curID != item.FromArtifactID && curID != rowID {
			superseded = true
			return nil
		}

		if receiptIdentityVerdict(item.FromReceiptID, itemSeq, curSeq) == identitySuperseded {
			logger.Scene("publication").With("receiptId", item.ID).With("pageId", page.ID).
				With("ledgerReceiptId", curSeq).With("fromReceiptId", item.FromReceiptID).
				Warn("改 URL 回执：账本上的成功身份属于另一条回执，本次判定为已被替代")
			superseded = true
			return nil
		}

		if item.CapturedInputVersion > 0 && snap.InputVersion != item.CapturedInputVersion {

			expired = true
			return nil
		}

		aerr := s.applyUpdateURL(ctx, tx, updateURLApplyInput{
			Page: page, ArtifactRowID: rowID, ArtifactHash: hash, Lang: lang,
			KernelNewPath:  item.Path,
			NewLogicalPath: rule.Strip(lang, item.Path),

			OldLogicalPath: page.DraftPath,
			OldKernelPath:  item.OldPath,
			WithRedirect:   item.Redirect,
			Deps:           deps,
			RouteLangs:     routeLangs,

			ReceiptID: itemSeq,
		})
		if aerr != nil {
			return aerr
		}
		return nil
	})
	if txErr != nil {

		return false, txErr
	}
	if superseded {
		logger.Scene("publication").With("receiptId", item.ID).With("pageId", page.ID).
			Warn("改 URL 回执判定为已被替代 / 访问面已非本次产物：不写数据库，只结案")
		s.abortPublishReceipt(ctx, item.ID, "改 URL 已被替代")
		return false, nil
	}
	if expired {
		if s.enqueueStalePageRebuild(ctx, page) {
			logger.Scene("publication").With("receiptId", item.ID).With("pageId", page.ID).
				With("captured", item.CapturedInputVersion).With("actual", versionAtLock).
				Warn("改 URL 回执的产物已过期（新路径仍在线），已入队重建")
			s.abortPublishReceipt(ctx, item.ID, "产物已过期，已入队重建")
			return false, nil
		}
		s.keepPublishReceiptPending(item.ID, "产物已过期但未入队，等待重建收敛")
		return false, nil
	}
	if derr := s.settleOldPathLocked(ctx, page.ID, page.ProjectID, item.Path, item.OldPath, item.Redirect); derr != nil && !errors.Is(derr, errOldPathTaken) {
		s.keepPublishReceiptPending(item.ID, "旧路径访问面处置失败")
		logger.Scene("publication").With("receiptId", item.ID).With("oldPath", item.OldPath).
			Error(derr, "改 URL 收敛后旧路径处置失败（回执留 pending 重放）")

		return false, nil
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	logger.Scene("page").With("pageId", page.ID).With("path", item.Path).
		Info("改 URL 在崩溃前已生效，已补齐数据库状态")
	s.invalidateActionTarget(ctx, page.ID)
	return true, nil
}

func (s *Service) recoverRollbackReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	page, perr := s.locatePageInProjects(ctx, item.SourceID)
	if perr != nil {
		s.abortPublishReceipt(ctx, item.ID, "页面不存在")
		return false, nil
	}
	expectedHash, herr := s.pageArtifactHashByID(ctx, item.ToArtifactID)
	if herr != nil {
		s.abortPublishReceipt(ctx, item.ID, "读取回执产物失败")
		return false, nil
	}
	if expectedHash == "" {

		s.abortPublishReceipt(ctx, item.ID, "回滚目标产物缺失")
		return false, nil
	}
	if s.activeArtifactHashAt(item.Path) != expectedHash {
		logger.Scene("publication").With("receiptId", item.ID).With("path", item.Path).
			With("expected", expectedHash).Warn("未结案的回滚回执判定为「未生效」，标记回滚")
		s.abortPublishReceipt(ctx, item.ID, "访问面未指向回滚目标产物")
		return false, nil
	}
	lang := buildLang(item.Lang)
	itemSeq, seqErr := parseReceiptID(item.ID)
	if seqErr != nil {
		return false, seqErr
	}
	converged, superseded := false, false
	txErr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if _, lerr := s.model.LockPageForActivationTx(ctx, tx, page.ProjectID, page.ID); lerr != nil {
			return lerr
		}
		curID, cerr := s.model.PublicationArtifactIDTx(ctx, tx, page.ID, lang)
		if cerr != nil {
			return cerr
		}

		if s.activeArtifactHashAt(item.Path) != expectedHash {
			superseded = true
			return nil
		}
		if curID != "" && curID != item.FromArtifactID {

			superseded = true
			return nil
		}
		if aerr := s.applyRollbackTx(ctx, tx, page.ProjectID, pagemodel.PublicationRecord{
			PageID: page.ID, Lang: lang, ActivePath: item.Path,
			ArtifactID: item.ToArtifactID, ArtifactHash: expectedHash, PublishedAt: time.Now().UTC(),

			ReceiptID: itemSeq,
		}, item.OldPath); aerr != nil {
			return aerr
		}

		converged = true
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	if superseded {
		logger.Scene("publication").With("receiptId", item.ID).With("pageId", page.ID).
			Warn("回滚回执判定为已被替代：不写数据库，只结案")
		s.abortPublishReceipt(ctx, item.ID, "回滚已被替代")
		return false, nil
	}
	if !converged {
		s.keepPublishReceiptPending(item.ID, "回滚未收敛")
		return false, nil
	}
	if derr := s.settleOldPathLocked(ctx, page.ID, page.ProjectID, item.Path, item.OldPath, false); derr != nil && !errors.Is(derr, errOldPathTaken) {
		s.keepPublishReceiptPending(item.ID, "旧路径访问面处置失败")
		logger.Scene("publication").With("receiptId", item.ID).With("oldPath", item.OldPath).
			Error(derr, "回滚收敛后旧路径处置失败（回执留 pending 重放）")

		return false, nil
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	logger.Scene("publication").With("pageId", page.ID).With("path", item.Path).
		Info("回滚在崩溃前已生效，已在实体锁内补齐数据库状态")
	return true, nil
}

var errUpdateURLWithdrawn = errors.New("改 URL 提交段：新路径访问面已被其它激活替代")

var ErrPublishLedgerUnavailable = errors.New("发布回执登记失败，未切换访问面")

type publishReceiptInput struct {
	Action         string
	ProjectID      string
	PageID         string
	Path           string
	Lang           string
	FromArtifactID string

	ToArtifactID string

	OldPath string

	Redirect bool

	CapturedInputVersion int64

	PathArtifacts map[string]pubcontract.ReceiptPathIdentity

	FromReceiptID string
}

func (s *Service) capturedFromReceiptID(ctx context.Context, projectID, pageID, lang string) string {
	if s == nil || s.model == nil {
		return ""
	}
	id, err := s.model.PublicationReceiptIDTxAny(ctx, projectID, pageID, lang)
	if err != nil {
		logger.Scene("publication").With("pageId", pageID).With("lang", lang).
			Warn("捕获段读取账本成功身份失败：按未知（空串）登记")
		return ""
	}
	if id <= 0 {
		return "0"
	}
	return strconv.FormatInt(id, 10)
}

func (s *Service) beginPublishReceipt(ctx context.Context, in publishReceiptInput) (string, error) {
	if s == nil || s.routes == nil {
		return "", nil
	}
	action := strings.TrimSpace(in.Action)
	if action == "" {
		action = pubcontract.ReceiptActionSwitchActive
	}
	id, err := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: in.ProjectID, Path: in.Path, PageID: in.PageID,
		FromArtifactID: in.FromArtifactID, ToArtifactID: in.ToArtifactID, Lang: in.Lang,
		Action: action, OldPath: in.OldPath, Redirect: in.Redirect,

		CapturedInputVersion: in.CapturedInputVersion,

		FromReceiptID: in.FromReceiptID,

		PathArtifacts: in.PathArtifacts,
	})
	if err == nil && strings.TrimSpace(id) == "" {
		err = errors.New("发布回执登记未返回 id")
	}

	if err != nil {
		logger.Scene("publication").With("pageId", in.PageID).With("path", in.Path).With("action", action).
			Error(err, "发布回执登记失败（不切换访问面）")
		return "", ErrPublishLedgerUnavailable
	}
	return id, nil
}

func (s *Service) publishedArtifactIDOf(ctx context.Context, page *pagemodel.PageEntity, lang string) string {
	if s == nil || page == nil {
		return ""
	}
	pub, err := s.model.GetPublication(ctx, page.ID, lang)
	if err != nil || pub == nil || pub.ArtifactID == nil {
		return ""
	}
	return *pub.ArtifactID
}

func (s *Service) postSwitchFaultHit() error {
	if s == nil || s.postSwitchFault == nil {
		return nil
	}
	return s.postSwitchFault()
}

func (s *Service) publishWindowFaultHit() error {
	if s == nil || s.publishWindowFault == nil {
		return nil
	}
	return s.publishWindowFault()
}

func (s *Service) keepPublishReceiptPending(receiptID, reason string) {
	if s == nil || strings.TrimSpace(receiptID) == "" {
		return
	}
	logger.Scene("publication").With("receiptId", receiptID).
		Warn("发布中断在「已切换访问面、数据库未跟上」窗口，回执保持 pending 交收敛例程判定：" + reason)
	s.NotifyPendingReceipt()
}

type receiptIdentity int

const (
	identityAccepted receiptIdentity = iota

	identitySuperseded
)

func receiptIdentityVerdict(fromRaw string, ownSeq, curSeq int64) receiptIdentity {
	if ownSeq > 0 && curSeq == ownSeq {
		return identityAccepted
	}
	if ownSeq <= 0 {

		if curSeq > 0 {
			return identitySuperseded
		}
		return identityAccepted
	}
	raw := strings.TrimSpace(fromRaw)
	if raw == "" {
		if curSeq > 0 {
			return identitySuperseded
		}
		return identityAccepted
	}
	fromSeq, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil || fromSeq < 0 {

		if curSeq > 0 && curSeq != ownSeq {
			return identitySuperseded
		}
		return identityAccepted
	}
	if curSeq == fromSeq {
		return identityAccepted
	}
	return identitySuperseded
}

func (s *Service) CaptureFromReceiptIDForTest(ctx context.Context, projectID, pageID, lang string) string {
	return s.capturedFromReceiptID(ctx, projectID, pageID, lang)
}

func ReceiptIdentityVerdictForTest(fromRaw string, ownSeq, curSeq int64) string {
	if receiptIdentityVerdict(fromRaw, ownSeq, curSeq) == identitySuperseded {
		return "superseded"
	}
	return "accepted"
}

func ParseReceiptID(receiptID string) (int64, error) { return parseReceiptID(receiptID) }

func parseReceiptID(receiptID string) (int64, error) {
	raw := strings.TrimSpace(receiptID)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("发布回执 id 不是十进制数值: %q: %w", raw, err)
	}
	if id <= 0 {
		return 0, fmt.Errorf("发布回执 id 必须为正数: %q", raw)
	}
	return id, nil
}

func (s *Service) completePublishReceipt(ctx context.Context, receiptID string) error {
	if s == nil || s.routes == nil || receiptID == "" {
		return nil
	}
	return s.routes.CompletePublishReceipt(ctx, receiptID)
}

func (s *Service) abortPublishReceipt(ctx context.Context, receiptID, reason string) {
	if s == nil || s.routes == nil || receiptID == "" {
		return
	}
	if err := s.routes.AbortPublishReceipt(ctx, receiptID); err != nil {
		logger.Scene("publication").With("receiptId", receiptID).
			Error(err, "发布回执结案失败（"+reason+"）")
	}
}

var recoverableReceiptActions = []string{
	pubcontract.ReceiptActionSwitchActive,
	pubcontract.ReceiptActionUpdateURL,
	pubcontract.ReceiptActionRollback,

	pubcontract.ReceiptActionDeactivate,
}

func recoverableReceiptAction(action string) bool {
	for _, known := range recoverableReceiptActions {
		if known == action {
			return true
		}
	}
	return false
}

func (s *Service) recoverOne(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {

	switch item.Action {
	case pubcontract.ReceiptActionUpdateURL:
		return s.recoverUpdateURLReceipt(ctx, item)
	case pubcontract.ReceiptActionRollback:
		return s.recoverRollbackReceipt(ctx, item)
	case pubcontract.ReceiptActionDeactivate:
		return s.recoverDeactivateReceipt(ctx, item)
	}
	return s.recoverSwitchActiveReceipt(ctx, item)
}

const (
	pendingReceiptConvergeBatch = 20

	pendingReceiptConvergeMaxBatches = 50

	pendingReceiptStartupMaxBatches = 0

	pendingReceiptConvergeInterval = time.Minute

	pendingReceiptConvergeTimeout = 2 * time.Minute

	pendingReceiptStartupTimeout = 5 * time.Minute
)

type pendingReceiptConvergeResult struct {
	claimed    int
	completed  int
	rolledBack int
	failed     int
	batches    int
}

func (r pendingReceiptConvergeResult) converged() int { return r.completed + r.rolledBack }

func (s *Service) ConvergePendingReceipts(ctx context.Context) (converged int, err error) {
	res, claimErr, itemErrs := s.convergePendingReceipts(ctx, pendingReceiptConvergeMaxBatches)
	return res.converged(), errors.Join(append([]error{claimErr}, itemErrs...)...)
}

func (s *Service) RecoverPendingPublications(ctx context.Context) (recovered, rolledBack int, err error) {
	res, claimErr, _ := s.convergePendingReceipts(ctx, pendingReceiptStartupMaxBatches)
	return res.completed, res.rolledBack, claimErr
}

func (s *Service) convergePendingReceipts(ctx context.Context, maxBatches int) (res pendingReceiptConvergeResult, claimErr error, itemErrs []error) {

	if s == nil || s.routes == nil || s.publication == nil {
		return res, nil, nil
	}
	start := time.Now()

	for _, sourceType := range receiptClaimSourceTypes {
		s.convergeSource(ctx, sourceType, maxBatches, &res, &itemErrs, &claimErr)
	}
	s.lastConvergeAt.Store(time.Now().Unix())
	s.logPendingReceiptConverge(res, time.Since(start))
	return res, claimErr, itemErrs
}

var receiptClaimSourceTypes = []string{
	pubcontract.ReceiptSourcePage,
	pubcontract.ReceiptSourceProject,
}

func claimableReceiptSource(sourceType, action string) bool {
	switch sourceType {
	case pubcontract.ReceiptSourcePage:
		return true
	case pubcontract.ReceiptSourceProject:
		return action == pubcontract.ReceiptActionDeactivate
	default:
		return false
	}
}

func (s *Service) convergeSource(ctx context.Context, sourceType string, maxBatches int, res *pendingReceiptConvergeResult, itemErrs *[]error, claimErr *error) {

	lastBatch := ""
	for batch := 0; maxBatches <= 0 || batch < maxBatches; batch++ {
		if cerr := ctx.Err(); cerr != nil {
			*itemErrs = append(*itemErrs, cerr)
			return
		}
		items, cerr := s.routes.ClaimPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
			SourceType: sourceType,
			Actions:    recoverableReceiptActions,
			Limit:      pendingReceiptConvergeBatch,
		})
		if cerr != nil {
			*claimErr = cerr
			logger.Scene("publication").With("sourceType", sourceType).Error(cerr, "领取未结案发布回执失败（本轮收敛中止）")
			return
		}
		if len(items) == 0 {
			break
		}

		batchKey := receiptBatchKey(items)
		if batchKey == lastBatch {
			logger.Scene("publication").With("sourceType", sourceType).With("batch", batch).
				With("items", len(items)).Debug("发布回执收敛：本批与上一批完全相同（本轮无进展，收手等下一轮）")
			break
		}
		lastBatch = batchKey
		res.claimed += len(items)
		res.batches++
		handled, failed := 0, 0
		for _, item := range items {

			if !claimableReceiptSource(item.SourceType, item.Action) || !recoverableReceiptAction(item.Action) {
				continue
			}
			handled++
			done, rerr := s.recoverOne(ctx, item)
			if rerr != nil {
				failed++
				logger.Scene("publication").With("receiptId", item.ID).With("action", item.Action).
					With("path", item.Path).Error(rerr, "发布回执收敛失败（保留 pending，下一轮重试）")
				*itemErrs = append(*itemErrs, fmt.Errorf("回执 %s(%s): %w", item.ID, item.Action, rerr))
				continue
			}
			if done {
				res.completed++
			} else {
				res.rolledBack++
			}
		}
		res.failed += failed

		if failed > 0 {
			break
		}

		if handled == 0 {
			return
		}
	}
}

func receiptBatchKey(items []pubcontract.PendingReceiptResp) string {
	if len(items) == 0 {
		return ""
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

func (s *Service) logPendingReceiptConverge(res pendingReceiptConvergeResult, cost time.Duration) {
	entry := logger.Scene("publication").
		With("claimed", res.claimed).
		With("converged", res.converged()).
		With("completed", res.completed).
		With("rolledBack", res.rolledBack).
		With("failed", res.failed).
		With("batches", res.batches).
		With("costMs", cost.Milliseconds())
	switch {
	case res.failed > 0:
		entry.Warn("发布回执收敛完成（有仍未成功的行，保留 pending 等待下一轮）")
	case res.claimed > 0:
		entry.Info("发布回执收敛完成")
	default:
		entry.Debug("发布回执收敛空转（没有未结案的回执）")
	}
}

func (s *Service) NotifyPendingReceipt() {
	if s == nil || s.convergeWake == nil {
		return
	}
	select {
	case s.convergeWake <- struct{}{}:
	default:
	}
}

func (s *Service) PendingReceiptStatus(ctx context.Context) (pending int64, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePage,
		Actions:    recoverableReceiptActions,
	})
	if err != nil {
		return 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	return pending, lastConvergeAt, nil
}

func (s *Service) PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error) {
	if s == nil || s.routes == nil {
		return 0, 0, time.Time{}, nil
	}
	pending, err = s.routes.CountPendingReceipts(ctx, &pubcontract.ReceiptsQueryReq{
		SourceType: pubcontract.ReceiptSourcePage,
		Actions:    recoverableReceiptActions,
	})
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	if unix := s.lastConvergeAt.Load(); unix > 0 {
		lastConvergeAt = time.Unix(unix, 0).UTC()
	}
	if pending <= 0 {
		return 0, 0, lastConvergeAt, nil
	}
	return pending, s.oldestPendingReceiptAge(ctx), lastConvergeAt, nil
}

func (s *Service) oldestPendingReceiptAge(ctx context.Context) time.Duration {
	items, err := s.routes.ListPendingReceipts(ctx)
	if err != nil {
		logger.Scene("publication").With("err", err).
			Warn("读取最老未结案回执失败（积压条数仍可用，年龄暂缺）")
		return 0
	}
	for _, item := range items {

		if item.SourceType != pubcontract.ReceiptSourcePage || !recoverableReceiptAction(item.Action) {
			continue
		}
		ts, perr := time.Parse(time.RFC3339, item.CreatedAt)
		if perr != nil || ts.IsZero() {
			return 0
		}
		if age := time.Since(ts); age > 0 {
			return age
		}
		return 0
	}
	return 0
}

func StartPendingReceiptConvergenceScheduler(svc *Service) {
	if utils.IsTestProcess() {
		return
	}
	startPendingReceiptConvergenceScheduler(svc, pendingReceiptConvergeInterval)
}

func StartPendingReceiptConvergenceSchedulerWithInterval(svc *Service, interval time.Duration) {
	startPendingReceiptConvergenceScheduler(svc, interval)
}

func startPendingReceiptConvergenceScheduler(svc *Service, interval time.Duration) {
	if svc == nil {
		return
	}
	if interval <= 0 {
		interval = pendingReceiptConvergeInterval
	}
	go func() {

		startupCtx, cancelStartup := context.WithTimeout(context.Background(), pendingReceiptStartupTimeout)
		_, _, _ = svc.RecoverPendingPublications(startupCtx)
		cancelStartup()

		converge := func() {
			ctx, cancel := context.WithTimeout(context.Background(), pendingReceiptConvergeTimeout)
			defer cancel()
			_, _ = svc.ConvergePendingReceipts(ctx)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				converge()
			case <-svc.convergeWake:
				converge()
			}
		}
	}()
}

const maxAutoRebuildPages = 20

type pageRebuildPlan struct {
	Page *pagemodel.PageEntity

	Langs []string

	PublishLangs []string

	DraftVersion int64
	InputHash    string

	InputVersion int64

	Intent string
}

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

func normalizeRebuildIntent(raw string) string {
	if strings.TrimSpace(raw) == pagecontract.BuildIntentManual {
		return pagecontract.BuildIntentManual
	}
	return pagecontract.BuildIntentDependency
}

func (s *Service) planPageRebuild(ctx context.Context, page *pagemodel.PageEntity, langs []string, intent string) (*pageRebuildPlan, error) {
	if page == nil {
		return nil, ErrPageNotFound
	}
	plan := &pageRebuildPlan{
		Page: page, Intent: normalizeRebuildIntent(intent),
		DraftVersion: page.DraftVersion, InputHash: hash(page.DraftDocument),

		InputVersion: page.InputVersion,
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

		if _, perr := s.publish(ctx, &pagedto.PublishReq{ID: plan.Page.ID, Lang: lang}, false); perr != nil {
			errs = append(errs, fmt.Errorf("语言 %s 重新发布失败: %w", lang, perr))
			continue
		}
		published++
	}
	return rebuilt, published, errors.Join(errs...)
}

func (s *Service) RunPageBuildJob(ctx context.Context, req *pagedto.PageBuildJobReq) error {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {

		return err
	}
	intent := normalizeRebuildIntent(req.Intent)
	var langs []string
	if lang := strings.TrimSpace(req.Lang); lang != "" {
		langs = []string{lang}
	} else if intent == pagecontract.BuildIntentManual {

		langs = []string{buildLang("")}
	}

	plan, perr := s.planPageRebuild(ctx, page, langs, intent)
	if perr != nil {
		s.markRebuildFailure(ctx, page, pageRebuildStagePlan)
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
		s.markRebuildFailure(ctx, page, pageRebuildStageBuild)
		return rerr
	}

	logger.Scene("build").With("pageId", page.ID).With("lang", req.Lang).With("intent", plan.Intent).
		With("rebuilt", rebuilt).With("published", published).Info("构建任务执行完成")
	return nil
}

func (s *Service) SetBuildQueue(q pagecontract.BuildQueueEnqueuer) {
	if s == nil {
		return
	}
	s.buildQueue = q
}

func (s *Service) enqueueOverflowBuildJobs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if s.buildQueue == nil {
		logger.Scene("dependency").With("affected", len(ids)).
			Warn("自动重建超出单次上限且构建队列未接入，剩余页面保持 stale 等待后续触发")
		return fmt.Errorf("构建队列未接入，%d 个超限页面未排上队: %w", len(ids), ErrRebuildOverloaded)
	}
	queued := 0
	var errs []error
	for _, id := range ids {

		page, err := s.locatePageInProjects(ctx, id)
		if err != nil {
			errs = append(errs, fmt.Errorf("超限重建入队：页面 %s 定位失败: %w", id, err))
			continue
		}
		langs, lerr := s.publishLangsOf(ctx, page.ProjectID)
		if lerr != nil {
			logger.Scene("dependency").With("page_id", id).
				Error(lerr, "超限重建入队跳过：站点语言清单不可读，入队默认语言一种会让其余语言永久停在旧字节")
			errs = append(errs, fmt.Errorf("超限重建入队：页面 %s 的站点语言清单不可读: %w", id, lerr))
			continue
		}
		inputHash := hash(page.DraftDocument)
		for _, lang := range langs {
			if qerr := s.buildQueue.EnqueuePageBuild(ctx, id, page.ProjectID, lang,
				pagecontract.BuildIntentDependency, page.DraftVersion, inputHash); qerr != nil {
				logger.Scene("dependency").With("page_id", id).With("lang", lang).
					Error(qerr, "超限重建任务入队失败")
				errs = append(errs, fmt.Errorf("超限重建入队：页面 %s 语言 %s: %w", id, lang, qerr))
				continue
			}
			queued++
		}
	}
	logger.Scene("dependency").With("queued", queued).With("affected", len(ids)).
		Info("超限的自动重建已交给构建队列")
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (s *Service) RebuildArtifact(ctx context.Context, req *pagedto.RebuildArtifactReq) (res *pagedto.RebuildArtifactResp, err error) {
	if req == nil || strings.TrimSpace(req.ArtifactID) == "" {
		return nil, ErrInvalidParam
	}
	art, err := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: req.ArtifactID})
	if err != nil {
		return nil, err
	}
	if len(art.SourceDocument) == 0 {
		return nil, fmt.Errorf("产物 %s 缺少冻结源文档，无法重建", art.ID)
	}
	res = &pagedto.RebuildArtifactResp{
		ArtifactID:   art.ID,
		Lang:         art.Lang,
		Path:         art.CanonicalPath,
		ExpectedHash: art.ArtifactHash,
	}

	if verr := s.publisher.ArtifactExists(art.ArtifactHash); verr == nil {
		res.Restored, res.HashMatched, res.AlreadyThere = true, true, true
		res.ActualHash = art.ArtifactHash
		return res, nil
	}

	rebuilt, rerr := s.publisher.RestoreArtifact(ctx, pipeline.BuildInput{
		PageID:  art.PageID,
		Lang:    art.Lang,
		Path:    art.CanonicalPath,
		DocJSON: art.SourceDocument,
		Plan:    publicationPlanFromManifest(art.Manifest),
	})
	if rerr != nil {
		res.Reason = "重新编译失败: " + rerr.Error()
		return res, fmt.Errorf("重建产物失败: %w", rerr)
	}
	res.ActualHash = rebuilt.Hash
	if rebuilt.Hash != art.ArtifactHash {
		res.Reason = fmt.Sprintf(
			"重建产物 hash 与元数据不一致（期望 %s，实际 %s）：构建输入已变化 —— "+
				"典型原因是组件注册表更新，或编译期依赖内容（CMS/主题/导航）变动。"+
				"该产物无法原样恢复；如需上线新版本请走正常发布流程（build + publish）。",
			art.ArtifactHash, rebuilt.Hash)
		logger.Scene("page").With("artifactId", art.ID).
			With("expected", art.ArtifactHash).With("actual", rebuilt.Hash).
			Warn("产物重建 hash 不一致（构建输入已变化）")
		return res, nil
	}
	res.Restored, res.HashMatched = true, true
	logger.Scene("page").With("artifactId", art.ID).With("hash", rebuilt.Hash).
		Info("产物文件重建成功（hash 一致）")
	return res, nil
}

func (s *Service) AuditPublication(ctx context.Context) (res *pagedto.PublicationAuditResp, err error) {
	issues, checked, aerr := s.publication.AuditActiveLinks()
	if aerr != nil {
		return nil, aerr
	}
	res = &pagedto.PublicationAuditResp{Checked: checked, Healthy: len(issues) == 0}
	res.Issues = make([]pagedto.PublicationIssueResp, 0, len(issues))
	for _, it := range issues {
		res.Issues = append(res.Issues, pagedto.PublicationIssueResp{URLPath: it.URLPath, Link: it.Link, Reason: it.Reason})
	}

	orphans, orphanChecked, oerr := s.auditOrphanArtifacts(ctx)
	if oerr != nil {
		logger.Scene("page").Error(oerr, "产物反向对账失败")
	} else {
		res.Orphans = orphans
		res.OrphanChecked = orphanChecked
	}
	if len(issues) > 0 {
		logger.Scene("page").With("count", len(issues)).Warn("激活面巡检发现异常链接")
	}
	return res, nil
}

const defaultArtifactRetentionDays = 30

func (s *Service) GarbageCollectArtifacts(ctx context.Context, req *pagedto.GCArtifactsReq) (res *pagedto.GCArtifactsResp, err error) {
	retention, dryRun := defaultArtifactRetentionDays, true
	if req != nil {
		if req.RetentionDays > 0 {
			retention = req.RetentionDays
		}
		if req.DryRun != nil {
			dryRun = *req.DryRun
		}
	}
	before := time.Now().UTC().AddDate(0, 0, -retention)
	res = &pagedto.GCArtifactsResp{RetentionDays: retention, DryRun: dryRun}

	defer func() {
		if res == nil {
			return
		}
		objRes, oerr := s.collectOrphanContentObjects(ctx, retention, dryRun)
		if oerr != nil {

			logger.Scene("artifact").Error(oerr, "回收孤儿内容对象失败")
			return
		}
		if objRes == nil {
			return
		}
		res.OrphanObjects = objRes.Orphans
		res.ObjectsDeleted = objRes.Deleted
		res.ObjectsSkippedExternal = objRes.SkippedExtern
		res.ObjectsFailed = objRes.Failed
	}()

	protected, err := s.model.ListProtectedArtifactIDs(ctx)
	if err != nil {
		return nil, err
	}
	if s.routes != nil {
		refs, rerr := s.routes.ListReferencedArtifactIDs(ctx)
		if rerr != nil {
			return nil, rerr
		}
		protected = append(protected, refs...)
	}
	if len(protected) == 0 {

		return res, nil
	}

	cands, err := s.artifacts.ListGCCandidates(ctx, before, protected)
	if err != nil {
		return nil, err
	}
	res.Scanned = len(cands)
	for _, c := range cands {
		item := pagedto.GCRecoveredArtifactResp{ID: c.ID, ArtifactHash: c.ArtifactHash, Lang: c.Lang}
		others, cerr := s.artifacts.CountOtherAvailableByHash(ctx, c.ArtifactHash, c.ID)
		if cerr != nil {
			item.Action, item.Reason = "skipped", "同 hash 引用检查失败: "+cerr.Error()
			res.Failed++
			res.Items = append(res.Items, item)
			continue
		}
		if others > 0 {
			item.Action = "kept_shared"
			item.Reason = fmt.Sprintf("同 hash 仍被 %d 条产物行引用，文件保留", others)
			res.SkippedShared++
			if !dryRun {
				_, _ = s.artifacts.MarkPayloadState(ctx, []string{c.ID}, artifactcontract.PayloadStateGCPending)
			}
			res.Items = append(res.Items, item)
			continue
		}
		if dryRun {
			item.Action = "would_delete"
			res.Items = append(res.Items, item)
			continue
		}
		if derr := s.store.DeleteArtifact(pipeline.ArtifactLocator(c.ArtifactHash), c.ArtifactHash); derr != nil {
			item.Action, item.Reason = "delete_failed", derr.Error()
			res.Failed++
			logger.Scene("artifact").With("id", c.ID).With("hash", c.ArtifactHash).Error(derr, "产物文件删除失败")
			res.Items = append(res.Items, item)
			continue
		}
		if _, merr := s.artifacts.MarkPayloadState(ctx, []string{c.ID}, artifactcontract.PayloadStateDeleted); merr != nil {
			item.Action, item.Reason = "state_failed", "文件已删但状态未更新: "+merr.Error()
			res.Failed++
		} else {
			item.Action = "deleted"
			res.Deleted++
		}
		res.Items = append(res.Items, item)
	}
	logger.Scene("artifact").With("scanned", res.Scanned).With("deleted", res.Deleted).
		With("skippedShared", res.SkippedShared).With("dryRun", dryRun).Info("产物回收完成")
	return res, nil
}

func (s *Service) collectOrphanContentObjects(ctx context.Context, retention int, dryRun bool) (res *artifactcontract.ContentObjectGCResp, err error) {
	if s.artifacts == nil {
		return nil, nil
	}
	return s.artifacts.GarbageCollectContentObjects(ctx, &artifactcontract.ContentObjectGCReq{
		RetentionDays: retention, DryRun: &dryRun,
	})
}

const seoLangProbePath = "/__seo_lang_probe__"

func (s *Service) seoLangs(ctx context.Context, projectID string) []compliance.LangRule {
	if s == nil || s.project == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	langs := pipeline.EnabledLangs(ctx, s.project, projectID)
	if len(langs) < 2 {
		return nil
	}
	return langRulesOf(pipeline.LangURLRuleForProject(ctx, s.project, projectID), langs)
}

func langRulesOf(rule pipeline.LangURLRule, langs []string) []compliance.LangRule {
	out := make([]compliance.LangRule, 0, len(langs))
	for _, lang := range langs {
		p, err := rule.Path(lang, seoLangProbePath)
		if err != nil {

			continue
		}
		out = append(out, compliance.LangRule{
			Code:   lang,
			Prefix: strings.TrimSuffix(p, seoLangProbePath),
		})
	}
	return out
}

func (s *Service) inspectArtifactSEO(hash, url, lang string, sitemapListed bool, langs []compliance.LangRule) (*compliance.Report, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("产物存储未就绪")
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("产物 hash 为空")
	}
	art, err := s.store.GetArtifact(pipeline.ArtifactLocator(hash))
	if err != nil {
		return nil, err
	}
	return compliance.Inspect(compliance.Artifact{
		URL: url, Lang: lang, ArtifactHash: hash,
		HTML: art.Entries["index.html"], Langs: langs, SitemapListed: sitemapListed,
		SiteBaseURL: seo.SiteBaseURL(),
	}), nil
}

func (s *Service) inspectBuiltArtifact(ctx context.Context, projectID, hash, url, lang string, sitemapListed bool) {
	langs := s.seoLangs(ctx, projectID)
	rep, err := s.inspectArtifactSEO(hash, url, lang, sitemapListed, langs)
	if err != nil {
		logger.Scene("build").With("hash", hash).With("url", url).Warn("构建期 SEO 合规校验跳过：产物字节不可读")
		return
	}
	logSEOReport("build", rep)
}

func (s *Service) pathListedInSitemap(path string) bool {
	if s == nil || s.publication == nil || strings.TrimSpace(path) == "" {
		return false
	}
	state, err := s.publication.Inspect(path)
	return err == nil && state != nil && state.Kind != "none"
}

func logSEOReport(scene string, rep *compliance.Report) {
	if rep == nil {
		return
	}
	for _, f := range rep.Findings {
		entry := logger.Scene(scene).
			With("url", f.URL).With("lang", f.Lang).With("rule", f.Rule).
			With("artifactHash", f.ArtifactHash).With("evidence", f.Evidence)
		if f.Level == compliance.LevelError {
			entry.Warn("构建期 SEO 合规校验：产物自相矛盾")
			continue
		}
		entry.Info("构建期 SEO 合规校验：需人工确认")
	}
}

func (s *Service) SEOPatrol(ctx context.Context, req *pagedto.SEOPatrolReq) (res *pagedto.SEOPatrolResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	projectID := strings.TrimSpace(req.ProjectID)
	langs := s.seoLangs(ctx, projectID)
	routes, err := s.model.ListActiveRoutesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	arts := make([]compliance.Artifact, 0, len(routes))
	res = &pagedto.SEOPatrolResp{
		ProjectID: projectID,
		SampledAt: utils.NewJSONTime(time.Now().UTC()),
	}
	unchecked := func(url, reason string) {
		res.Unchecked = append(res.Unchecked, pagedto.SEOUncheckedRouteResp{URL: url, Reason: reason})
	}
	for _, rt := range routes {
		url := seo.CanonicalPublicPath(rt.Path)
		if rt.PresentationID != nil && strings.TrimSpace(*rt.PresentationID) != "" {

			unchecked(url, "归属自动发布实例：其产物元数据不属于 page 模块（本轮未纳入）")
			continue
		}
		if rt.ArtifactID == nil || strings.TrimSpace(*rt.ArtifactID) == "" {
			unchecked(url, "激活路由没有指向产物")
			continue
		}
		art, derr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: *rt.ArtifactID})
		if derr != nil || art == nil || art.ArtifactHash == "" {
			unchecked(url, "产物元数据读取失败（产物行不存在或不可读）")
			continue
		}
		loaded, lerr := s.store.GetArtifact(pipeline.ArtifactLocator(art.ArtifactHash))
		if lerr != nil {
			unchecked(url, "产物文件缺失或不可读："+art.ArtifactHash)
			continue
		}
		arts = append(arts, compliance.Artifact{
			URL: url, Lang: art.Lang, ArtifactHash: art.ArtifactHash,
			HTML: loaded.Entries["index.html"], Langs: langs,
			SiteBaseURL:   seo.SiteBaseURL(),
			SitemapListed: true,
		})
	}

	site := compliance.InspectSite(arts)
	res.ChecksPerArtifact = site.Checks
	res.Checked = site.Checked
	res.Healthy = site.Healthy()
	res.Errors = site.ErrorCount()
	res.Warnings = site.WarnCount()
	res.Artifacts = make([]pagedto.SEOArtifactItemResp, 0, len(site.Reports))
	for _, r := range site.Reports {
		item := pagedto.SEOArtifactItemResp{
			URL: r.URL, Lang: r.Lang, ArtifactHash: r.ArtifactHash, Checks: r.Checks,
			Index: r.Index.Index, Follow: r.Index.Follow,
			RobotsExplicit: r.Index.Explicit, Robots: r.Index.Raw,
		}
		for _, f := range r.Findings {
			item.Findings = append(item.Findings, toSEOFindingItem(f))
		}
		res.Artifacts = append(res.Artifacts, item)
	}
	for _, f := range site.Findings {
		res.CrossFindings = append(res.CrossFindings, toSEOFindingItem(f))
	}
	if res.Errors > 0 {
		logger.Scene("page").With("projectId", projectID).
			With("checked", res.Checked).With("errors", res.Errors).With("warnings", res.Warnings).
			Warn("SEO 合规巡检发现产物自相矛盾")
	}
	return res, nil
}

func toSEOFindingItem(f compliance.Finding) pagedto.SEOFindingItemResp {
	return pagedto.SEOFindingItemResp{
		URL: f.URL, Lang: f.Lang, ArtifactHash: f.ArtifactHash,
		Rule: f.Rule, Level: string(f.Level), Evidence: f.Evidence, Expect: f.Expect,
	}
}

type deactivationIntent struct {
	ReceiptID     string
	ProjectID     string
	PageID        string
	Paths         []string
	PathArtifacts map[string]pubcontract.ReceiptPathIdentity
}

func pageReceiptOwner(pageID string) pubcontract.ReceiptOwner {
	return pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourcePage, SourceID: pageID}
}

func projectReceiptOwner(projectID string) pubcontract.ReceiptOwner {
	return pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourceProject, SourceID: projectID}
}

func (s *Service) pathIdentities(ctx context.Context, projectID, pageID string, paths []string) map[string]pubcontract.ReceiptPathIdentity {
	out := make(map[string]pubcontract.ReceiptPathIdentity, len(paths))

	pubs, perr := s.model.ListPublicationsInProject(ctx, projectID, pageID)
	if perr != nil {
		logger.Scene("publication").With("pageId", pageID).
			Warn("读取页面语言账本失败，下线意图的路径归属按链接事实记录: " + perr.Error())
		pubs = nil
	}
	for _, p := range paths {
		key := strings.TrimSpace(p)
		if key == "" {
			continue
		}
		var id pubcontract.ReceiptPathIdentity
		for _, pub := range pubs {
			if strings.TrimSpace(pub.ActivePath) != key {
				continue
			}
			if pub.ArtifactID != nil {
				id.ArtifactID = strings.TrimSpace(*pub.ArtifactID)
			}
			if pub.ReceiptID != nil && *pub.ReceiptID > 0 {
				id.ReceiptID = strconv.FormatInt(*pub.ReceiptID, 10)
			}
			break
		}
		if s.publication != nil {
			if st, ierr := s.publication.Inspect(key); ierr == nil && st != nil && st.Locator != nil {
				id.Locator = strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key)
			}
		}
		if id.ArtifactID != "" || id.ReceiptID != "" || id.Locator != "" {
			out[key] = id
		}
	}
	return out
}

func (s *Service) registerDeactivationIntent(ctx context.Context, projectID, pageID, primaryPath string, paths []string) (*deactivationIntent, error) {
	if s == nil || s.routes == nil {
		return nil, errors.New("下线意图登记端口未装配")
	}
	cleaned := normalizeOldPaths(paths)

	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(pageID) == "" {
		return nil, errors.New("下线意图缺少页面归属事实")
	}
	ids := s.pathIdentities(ctx, projectID, pageID, cleaned)
	path := strings.TrimSpace(primaryPath)
	if path == "" {
		path = firstNonEmptyPath(cleaned)
	}
	id, rerr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID: projectID, Path: path, PageID: pageID,
		Action:        pubcontract.ReceiptActionDeactivate,
		OldPaths:      cleaned,
		OldPath:       firstNonEmptyPath(cleaned),
		PathArtifacts: ids,
	})
	if rerr != nil {
		return nil, fmt.Errorf("登记下线意图失败（未改动访问面）: %w", rerr)
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("登记下线意图失败（未改动访问面）: 回执登记未返回 id")
	}
	return &deactivationIntent{
		ReceiptID: id, ProjectID: projectID, PageID: pageID,
		Paths: cleaned, PathArtifacts: ids,
	}, nil
}

func (s *Service) registerProjectDeactivationIntent(ctx context.Context, projectID, path string) (*deactivationIntent, error) {
	if s == nil || s.routes == nil {
		return nil, errors.New("下线意图登记端口未装配")
	}
	projectID = strings.TrimSpace(projectID)
	path = strings.TrimSpace(path)
	if projectID == "" || path == "" {
		return nil, errors.New("下线意图缺少工程或路径事实")
	}
	ids := s.redirectPathIdentities(ctx, path)
	id, rerr := s.routes.BeginPublishReceipt(ctx, &pubcontract.BeginPublishReceiptReq{
		ProjectID:     projectID,
		Path:          path,
		Action:        pubcontract.ReceiptActionDeactivate,
		OldPaths:      []string{path},
		OldPath:       path,
		PathArtifacts: ids,
		Owner: &pubcontract.ReceiptOwner{
			SourceType: pubcontract.ReceiptSourceProject,
			SourceID:   projectID,
		},
	})
	if rerr != nil {
		return nil, fmt.Errorf("登记下线意图失败（未改动访问面）: %w", rerr)
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("登记下线意图失败（未改动访问面）: 回执登记未返回 id")
	}
	return &deactivationIntent{
		ReceiptID: id, ProjectID: projectID, Paths: []string{path}, PathArtifacts: ids,
	}, nil
}

func (s *Service) redirectPathIdentities(ctx context.Context, path string) map[string]pubcontract.ReceiptPathIdentity {
	out := make(map[string]pubcontract.ReceiptPathIdentity, 1)
	key := strings.TrimSpace(path)
	if key == "" {
		return out
	}
	var id pubcontract.ReceiptPathIdentity
	if s.publication != nil {
		if st, ierr := s.publication.Inspect(key); ierr == nil && st != nil && st.Locator != nil {
			id.Locator = strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key)
		}
	}
	out[key] = id
	return out
}

func (s *Service) recoverProjectDeactivateReceipt(ctx context.Context, item pubcontract.PendingReceiptResp, projectID string) (bool, error) {
	if _, perr := parseReceiptID(item.ID); perr != nil {
		return false, perr
	}
	if werr := s.withdrawProjectDeactivatedLinks(ctx, projectID, item.ID); werr != nil {
		logger.Scene("publication").With("receiptId", item.ID).With("projectId", projectID).
			Warn("项目级下线回执的撤链被归属复验拒绝，保持 pending 待下一轮: " + werr.Error())
		return false, werr
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	return true, nil
}

func normalizeOldPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func firstNonEmptyPath(paths []string) string {
	for _, p := range paths {
		if v := strings.TrimSpace(p); v != "" {
			return v
		}
	}
	return ""
}

func deactivationPathsUnchanged(registered []string, current []string) error {
	want := normalizeOldPaths(registered)
	got := normalizeOldPaths(current)
	if len(want) != len(got) {
		return fmt.Errorf("%w: 登记的下线路径集合与当前不一致", errOldPathTaken)
	}
	index := make(map[string]struct{}, len(got))
	for _, p := range got {
		index[p] = struct{}{}
	}
	for _, p := range want {
		if _, ok := index[p]; !ok {
			return fmt.Errorf("%w: 登记的下线路径集合与当前不一致", errOldPathTaken)
		}
	}
	return nil
}

func (s *Service) markDeactivationCommitted(ctx context.Context, tx *gorm.DB, in *deactivationIntent) error {
	if s == nil || s.routes == nil || in == nil {
		return nil
	}
	if tx == nil {
		return errors.New("下线意图提交标记必须在业务事务内")
	}

	owner := projectReceiptOwner(in.ProjectID)
	if strings.TrimSpace(in.PageID) != "" {
		owner = pageReceiptOwner(in.PageID)
	}
	return s.routes.MarkDeactivationCommittedTx(ctx, tx, owner,
		in.ProjectID, in.ReceiptID, in.PathArtifacts)
}

func (s *Service) recoverDeactivateReceipt(ctx context.Context, item pubcontract.PendingReceiptResp) (bool, error) {
	if s == nil || s.publication == nil || s.routes == nil {
		return false, nil
	}
	projectID := strings.TrimSpace(item.ProjectID)
	pageID := strings.TrimSpace(item.SourceID)
	if item.SourceType == pubcontract.ReceiptSourceProject {

		pageID = ""
	}

	if projectID == "" || (item.SourceType == pubcontract.ReceiptSourcePage && pageID == "") {

		s.abortPublishReceipt(ctx, item.ID, "下线意图缺少工程/页面事实")
		return false, nil
	}
	if item.SourceType == pubcontract.ReceiptSourceProject {
		return s.recoverProjectDeactivateReceipt(ctx, item, projectID)
	}
	if _, perr := parseReceiptID(item.ID); perr != nil {
		return false, perr
	}
	var (
		abandon   bool
		committed bool
	)
	txErr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		if aerr := s.model.LockPageEntityAdvisoryTx(ctx, tx, pageID); aerr != nil {
			return aerr
		}
		row, lerr := s.routes.LoadDeactivationReceiptTx(ctx, tx, pageReceiptOwner(pageID), projectID, item.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				abandon = true
				return nil
			}
			return lerr
		}
		if row.ReceiptState != "pending" {

			abandon = true
			return nil
		}
		if !deactivationBusinessCommittedFromData(row.ReceiptData) {

			return nil
		}
		committed = true
		return nil
	})
	if txErr != nil {
		return false, txErr
	}
	if abandon {
		return false, nil
	}
	if !committed {
		s.keepPublishReceiptPending(item.ID, "下线意图的业务变更尚未提交")
		return false, nil
	}
	if werr := s.withdrawDeactivatedLinks(ctx, projectID, pageID, item.ID); werr != nil {

		logger.Scene("publication").With("receiptId", item.ID).With("pageId", pageID).
			Warn("下线回执的撤链被归属复验拒绝，保持 pending 待下一轮: " + werr.Error())
		return false, werr
	}
	if cerr := s.completePublishReceipt(ctx, item.ID); cerr != nil {
		return false, cerr
	}
	return true, nil
}
