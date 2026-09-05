package pageservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	pubdto "go_wp/internal/module/publication/dto"

	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
)

// 发布链路（docs/03-pipeline.md §6 / 0-A1 §2）：
//
//	Build   草稿确定性编译 → 不可变产物落盘 → 元数据入库 → 暂存指针回写；
//	Publish 校验暂存 → 原子激活符号链接 → active 指针与路由 active 化；
//	Rollback 内核按 hash 直接重激活历史产物（秒级，无重新编译）;
//	UpdateURL 新 URL 构建并激活，旧 URL 按 301 / 取消激活处理。
//
// 内核 pipeline.Publisher 的内存态可由数据库随时重建（LoadRecord），
// 因此进程重启不影响发布正确性；多实例队列化属后续 build 模块。
// page_artifacts.created_by 为 uuid 列：系统操作留空，
// artifact.Record 的 defaultCreator 会兜底为全零 UUID。
// 此前写入 "system" 字面量导致构建入库 500（uuid 解析失败），发布主链无法走通。
const systemCreator = ""

// syncKernel 把页面当前草稿同步进内核记录（幂等；版本号以内核为准续增）。
func (s *Service) syncKernel(path string, doc json.RawMessage, pageID string) error {
	st, err := s.publisher.Status(pageID)
	if errors.Is(err, pipeline.ErrPageNotFound) {
		_, err = s.publisher.SaveDraft(pageID, 0, path, doc)
		return err
	}
	if err != nil {
		return err
	}
	if st.Path != path {
		// 内核记录路径落后于数据库（如改 URL 中断恢复）：整体重建。
		s.publisher.LoadRecord(&pipeline.PageRecord{ID: pageID})
		_, err = s.publisher.SaveDraft(pageID, 0, path, doc)
		return err
	}
	_, err = s.publisher.SaveDraft(pageID, st.Version, path, doc)
	return err
}

// Build 基于当前草稿构建并暂存产物。
func (s *Service) Build(ctx context.Context, req *pagedto.BuildReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion > 0 && req.ExpectedVersion != page.DraftVersion {
		return nil, errors.New(pageenums.ErrDraftVersionConflict)
	}
	logger.Scene("build").With("pageId", page.ID).Info("开始构建")
	if err = s.syncKernel(page.DraftPath, page.DraftDocument, page.ID); err != nil {
		return nil, err
	}
	hash, err := s.publisher.Build(page.ID, s.kernelVersion(page.ID))
	if err != nil {
		logger.Scene("build").With("pageId", page.ID).Error(err, "构建失败")
		return nil, mapPublishError(err)
	}

	artifactID, err := s.ensureArtifactRow(ctx, page, hash)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err = s.model.MarkStaged(ctx, page.ID, artifactID, now); err != nil {
		return nil, err
	}
	logger.Scene("build").With("pageId", page.ID).With("hash", hash).Info("构建完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StateReady,
		StagedHash: hash, DraftPath: page.DraftPath,
	}, nil
}

// Publish 激活暂存产物：二次构建校验一致性后原子切换活跃指针。
func (s *Service) Publish(ctx context.Context, req *pagedto.PublishReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	logger.Scene("publication").With("pageId", page.ID).Info("开始发布")
	if page.StagedArtifactID == nil || *page.StagedArtifactID == "" {
		return nil, errors.New(pageenums.ErrNoStagedArtifact)
	}
	stagedArt, err := s.artifacts.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: *page.StagedArtifactID})
	if err != nil {
		return nil, errors.New(pageenums.ErrNoStagedArtifact)
	}
	if stagedArt.ArtifactKey == "" || stagedArt.PageID != page.ID {
		return nil, errors.New(pageenums.ErrNoStagedArtifact)
	}

	// FS 激活前预检：目标路径被其他页面/展示实例占用时提前失败（H7），
	// 避免内核先把 FS 覆盖成本页产物、DB 路由写入才报错的状态分裂。
	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, page.DraftPath, page.ID); err != nil {
		logger.Scene("publication").With("pageId", page.ID).With("path", page.DraftPath).Warn("发布被拒绝：路径已被占用")
		return nil, err
	}

	// 确定性构建保证与暂存一致；用「当前草稿」（路径+文档）重建内核——
	// 若草稿在构建后又被 SaveDraft 修改（含改路径），重建 hash 必与暂存不同，
	// 走 ErrRebuildRequired 拒绝发布，避免发布旧内容后界面误报「已发布最新」。
	if err = s.syncKernel(page.DraftPath, page.DraftDocument, page.ID); err != nil {
		return nil, err
	}
	version := s.kernelVersionOrOne(page.ID)
	built, buildErr := s.publisher.Build(page.ID, version)
	if buildErr != nil {
		logger.Scene("build").With("pageId", page.ID).Error(buildErr, "发布前复构建失败")
		return nil, mapPublishError(buildErr)
	}
	if built != stagedArt.ArtifactHash {
		return nil, errors.New(pageenums.ErrRebuildRequired)
	}
	hash, err := s.publisher.Publish(page.ID)
	if err != nil {
		logger.Scene("publication").With("pageId", page.ID).Error(err, "发布失败")
		return nil, mapPublishError(err)
	}

	now := time.Now().UTC()
	if err = s.model.MarkPublished(ctx, page.ID, page.DraftPath, stagedArt.ID, now); err != nil {
		return nil, err
	}
	if s.routes != nil {
		if _, err = s.routes.Activate(ctx, &pubdto.ActivateReq{
			ProjectID: page.ProjectID, Path: page.DraftPath,
			PageID: page.ID, ArtifactID: stagedArt.ID,
		}); err != nil {
			logger.Scene("publication").With("pageId", page.ID).Error(err, "发布路由激活失败")
			return nil, err
		}
	}
	logger.Scene("publication").With("pageId", page.ID).With("hash", hash).Info("发布完成")
	return &pagedto.PublishResp{
		PageID: page.ID, Status: pipeline.StatePublished, ActiveHash: hash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

// Rollback 秒级回滚到历史产物：指针切换，不重新编译。
func (s *Service) Rollback(ctx context.Context, req *pagedto.RollbackReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.TargetHash) == "" {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("targetHash", req.TargetHash).Info("开始回滚")
	targetArt, err := s.artifacts.Detail(ctx, &artifactdto.DetailReq{PageID: page.ID, Hash: req.TargetHash})
	if err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "回滚目标产物缺失")
		return nil, errors.New(pageenums.ErrRollbackTargetMiss)
	}
	if err = s.restoreKernelForHistory(page, targetArt); err != nil {
		return nil, err
	}
	if err = s.publisher.Rollback(page.ID, req.TargetHash); err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "回滚失败")
		return nil, mapPublishError(err)
	}

	now := time.Now().UTC()
	if err = s.model.MarkPublished(ctx, page.ID, targetArt.CanonicalPath, targetArt.ID, now); err != nil {
		return nil, err
	}
	if s.routes != nil {
		if _, err = s.routes.Activate(ctx, &pubdto.ActivateReq{
			ProjectID: page.ProjectID, Path: targetArt.CanonicalPath,
			PageID: page.ID, ArtifactID: targetArt.ID,
		}); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "回滚路由激活失败")
			return nil, err
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

// UpdateURL 修改访问路径：新 URL 构建激活后，旧 URL 注册 301 或取消激活。
// FS 激活发生在 publisher.UpdateURL 内部（先于 DB 路由写入），因此新路径
// 占用检查必须在此之前完成，避免「FS 先覆盖、DB 后报错」的状态分裂（H2/H7）。
func (s *Service) UpdateURL(ctx context.Context, req *pagedto.UpdateURLReq) (res *pagedto.PublishResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(pageenums.ErrPageNotFound)
	}
	newPath, err := normalizePagePath(req.NewPath)
	if err != nil {
		return nil, err
	}
	page, err := s.getExistingPage(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	// FS 激活前预检：新路径已被其他页面/展示实例占用（active/redirect/
	// reserved 任一 kind）时提前失败，绝不触发内核的 FS 覆盖。
	if err = s.ensureRouteNotOccupied(ctx, page.ProjectID, newPath, page.ID); err != nil {
		logger.Scene("page").With("pageId", page.ID).With("newPath", newPath).Warn("改 URL 被拒绝：新路径已被占用")
		return nil, err
	}
	logger.Scene("page").With("pageId", page.ID).With("newPath", newPath).Info("开始修改 URL")
	oldPath := page.DraftPathValue()
	publishedPath := oldPath
	if page.ActivePath != nil && *page.ActivePath != "" {
		publishedPath = *page.ActivePath
	}

	// 内核以旧发布路径为基线执行 UpdateURL（内部完成构建+激活+旧路径处置）。
	if err = s.restoreKernelForUpdate(ctx, page, publishedPath); err != nil {
		return nil, err
	}
	if _, err = s.publisher.UpdateURL(page.ID, newPath, req.WithRedirect); err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改失败")
		return nil, mapPublishError(err)
	}
	st, _ := s.publisher.Status(page.ID)

	// 新产物归档换取 page_artifacts 行 ID：路由 artifact_id 是 uuid 列，
	// 必须写产物行主键而非内容 hash（生产 DDL 下写 hash 必然 22P02 失败）。
	artifactRowID, err := s.ensureArtifactRow(ctx, page, activeHashOf(st))
	if err != nil {
		logger.Scene("page").With("pageId", page.ID).With("hash", activeHashOf(st)).Error(err, "URL 修改后产物归档失败")
		return nil, err
	}

	now := time.Now().UTC()
	if err = s.model.MoveDraftPath(ctx, page.ID, newPath, now); err != nil {
		logger.Scene("page").With("pageId", page.ID).Error(err, "草稿路径迁移失败")
		return nil, err
	}
	if s.routes != nil {
		// 改名失败必须中止：reserved 行滞留旧路径会让路由表与 pages 表脱节，
		// 后续 SaveDraft 基于错误基线增删路由（不得仅记日志继续）。
		if rerr := s.routes.RenameReserved(ctx, &pubdto.RenameReservedReq{
			ProjectID: page.ProjectID, PageID: page.ID,
			OldPath: oldPath, NewPath: newPath,
		}); rerr != nil {
			logger.Scene("page").With("pageId", page.ID).Error(rerr, "URL 修改后重命名保留路由失败，中止流程")
			return nil, rerr
		}
		if _, err = s.routes.Activate(ctx, &pubdto.ActivateReq{
			ProjectID: page.ProjectID, Path: newPath,
			PageID: page.ID, ArtifactID: artifactRowID,
		}); err != nil {
			logger.Scene("page").With("pageId", page.ID).Error(err, "URL 修改后路由激活失败")
			return nil, err
		}
		if oldPath != newPath {
			if req.WithRedirect {
				if err = s.ensureRedirectRoute(ctx, page, publishedPath); err != nil {
					logger.Scene("page").With("pageId", page.ID).Error(err, "重定向路由注册失败")
					return nil, err
				}
			} else if err = s.routes.Deactivate(ctx, &pubdto.DeactivateReq{
				ProjectID: page.ProjectID, Path: publishedPath,
			}); err != nil {
				logger.Scene("page").With("pageId", page.ID).Error(err, "旧 URL 取消激活失败")
				return nil, err
			}
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
		PublishedAt: now.Format(time.RFC3339),
	}, nil
}

// ---- 内核记录重建辅助 ----

// restoreKernelForHistory 以目标产物为基线重建内核记录（回滚前置）。
func (s *Service) restoreKernelForHistory(page *pagemodel.PageEntity, target *artifactdto.ArtifactResp) error {
	doc := page.DraftDocumentFor(target.SourceDocument)
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: target.CanonicalPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc,
		Histories: []*pipeline.HistoryEntry{{
			Hash: target.ArtifactHash, Path: target.CanonicalPath,
			Status: pipeline.StateSuperseded, Order: 1,
		}},
	}
	s.publisher.LoadRecord(rec)
	return nil
}

// restoreKernelForUpdate 以当前发布路径重建内核记录并预激活现有产物（URL 变更前置）。
func (s *Service) restoreKernelForUpdate(ctx context.Context, page *pagemodel.PageEntity, publishedPath string) error {
	doc := page.DraftDocument
	activeHash := ""
	histories := []*pipeline.HistoryEntry{}
	if page.ActiveArtifactID != nil && *page.ActiveArtifactID != "" {
		art, err := s.artifacts.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: *page.ActiveArtifactID})
		if err == nil {
			activeHash = art.ArtifactHash
			histories = append(histories, &pipeline.HistoryEntry{
				Hash: art.ArtifactHash, Path: art.CanonicalPath,
				Status: pipeline.StatePublished, Order: 1,
			})
			doc = page.DraftDocumentFor(art.SourceDocument)
		} else {
			logger.Scene("page").With("pageId", page.ID).With("artifactID", *page.ActiveArtifactID).With("err", err).Warn("回滚/改URL 时活动产物缺失")
		}
	}
	rec := &pipeline.PageRecord{
		ID: page.ID, Path: publishedPath, Version: 1, Status: pipeline.StatePublished,
		DocumentJSON: doc, ActiveHash: activeHash, Histories: histories,
	}
	s.publisher.LoadRecord(rec)
	return nil
}

// ensureArtifactRow 返回该 hash 对应的产物元数据行 ID；不存在则归档新建。
func (s *Service) ensureArtifactRow(ctx context.Context, page *pagemodel.PageEntity, hash string) (string, error) {
	existing, err := s.artifacts.Detail(ctx, &artifactdto.DetailReq{PageID: page.ID, Hash: hash})
	if err == nil {
		return existing.ID, nil
	}
	loc := pipeline.Locator{Provider: "local", Key: "artifacts/" + hash}
	art, err := s.store.GetArtifact(loc)
	if err != nil {
		return "", err
	}
	manifestJSON, err := json.Marshal(art.Manifest)
	if err != nil {
		return "", err
	}
	recorded, err := s.artifacts.EnsureRecord(ctx, &artifactdto.RecordReq{
		ArtifactID:       uuid.NewString(),
		PageID:           page.ID,
		Version:          page.DraftVersion,
		SourceDocument:   page.DraftDocument,
		SchemaVersion:    art.Manifest.PageDocumentSchemaVersion,
		SourceHash:       art.Manifest.SourceHash,
		BuildInputHash:   art.Manifest.BuildInputHash,
		ArtifactProvider: "local",
		ArtifactKey:      loc.Key,
		ArtifactHash:     hash,
		CompilerVersion:  art.Manifest.CompilerVersion,
		RegistryVersion:  art.Manifest.CompilerVersion,
		Manifest:         manifestJSON,
		CreatedBy:        systemCreator,
	})
	if err != nil {
		return "", err
	}
	return recorded.ID, nil
}

// ensureRedirectRoute 把旧发布路径占用标记为 redirect 并落盘重定向产物。
func (s *Service) ensureRedirectRoute(ctx context.Context, page *pagemodel.PageEntity, publishedPath string) error {
	ra, raErr := pipeline.NewRedirectArtifact(page.ActivePathValue(), 301)
	if raErr != nil {
		return raErr
	}
	if _, saErr := s.store.PutRedirect(ra); saErr != nil {
		return saErr
	}
	_, rerr := s.routes.Redirect(ctx, &pubdto.RedirectReq{
		ProjectID: page.ProjectID, OldPath: publishedPath, PageID: page.ID,
	})
	return rerr
}

// kernelVersion 读取内核记录当前版本（不存在视为 1）。
func (s *Service) kernelVersion(pageID string) int {
	if st, err := s.publisher.Status(pageID); err == nil && st.Version > 0 {
		return st.Version
	}
	return 1
}

func (s *Service) kernelVersionOrOne(pageID string) int { return s.kernelVersion(pageID) }

// ensureRouteNotOccupied 校验目标路径未被其他页面/展示实例占用
// （page_routes 中 active/redirect/reserved 任一 kind；page_id 为空即展示
// 实例占用）。本页面自己的占用行不算冲突。
// 该检查必须在触发 FS 激活的 publisher 调用之前执行（H7 前置防线），
// 并发抢占窗口由 publication Activate 事务内的归属校验兜底。
func (s *Service) ensureRouteNotOccupied(ctx context.Context, projectID, path, selfPageID string) error {
	var foreign int64
	if err := s.model.RouteDB(ctx).
		Where("project_id = ? AND path = ? AND (page_id IS NULL OR page_id <> ?)", projectID, path, selfPageID).
		Count(&foreign).Error; err != nil {
		return err
	}
	if foreign > 0 {
		return errors.New(pageenums.ErrPathOccupied)
	}
	return nil
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
		return errors.New(pageenums.ErrDraftVersionConflict)
	case errors.Is(err, pipeline.ErrNoStagedArtifact):
		return errors.New(pageenums.ErrNoStagedArtifact)
	case errors.Is(err, pipeline.ErrRollbackPathMismatch):
		return errors.New(pageenums.ErrRebuildRequired)
	case errors.Is(err, pipeline.ErrPageNotFound):
		return errors.New(pageenums.ErrPageNotFound)
	default:
		return err
	}
}
