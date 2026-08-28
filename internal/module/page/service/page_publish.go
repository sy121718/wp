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
	if err = s.syncKernel(page.DraftPath, page.DraftDocument, page.ID); err != nil {
		return nil, err
	}
	hash, err := s.publisher.Build(page.ID, s.kernelVersion(page.ID))
	if err != nil {
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

	// 确定性构建保证与暂存一致；不一致说明草稿在构建后又被保存，需要重新构建。
	if err = s.syncKernel(stagedArt.CanonicalPath, page.DraftDocumentFor(stagedArt.SourceDocument), page.ID); err != nil {
		return nil, err
	}
	version := s.kernelVersionOrOne(page.ID)
	built, buildErr := s.publisher.Build(page.ID, version)
	if buildErr != nil {
		return nil, mapPublishError(buildErr)
	}
	if built != stagedArt.ArtifactHash {
		return nil, errors.New(pageenums.ErrRebuildRequired)
	}
	hash, err := s.publisher.Publish(page.ID)
	if err != nil {
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
			return nil, err
		}
	}
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
	targetArt, err := s.artifacts.Detail(ctx, &artifactdto.DetailReq{PageID: page.ID, Hash: req.TargetHash})
	if err != nil {
		return nil, errors.New(pageenums.ErrRollbackTargetMiss)
	}
	if err = s.restoreKernelForHistory(page, targetArt); err != nil {
		return nil, err
	}
	if err = s.publisher.Rollback(page.ID, req.TargetHash); err != nil {
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
			return nil, err
		}
	}
	st, _ := s.publisher.Status(page.ID)
	respStatus := pipeline.StatePublished
	if st != nil && st.Status != "" {
		respStatus = st.Status
	}
	return &pagedto.PublishResp{
		PageID: page.ID, Status: respStatus, ActiveHash: req.TargetHash,
		DraftPath: page.DraftPath, PublishedAt: now.Format(time.RFC3339),
	}, nil
}

// UpdateURL 修改访问路径：新 URL 构建激活后，旧 URL 注册 301 或取消激活。
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
		return nil, mapPublishError(err)
	}
	st, _ := s.publisher.Status(page.ID)

	now := time.Now().UTC()
	if err = s.model.MoveDraftPath(ctx, page.ID, newPath, now); err != nil {
		return nil, err
	}
	if s.routes != nil {
		_ = s.routes.RenameReserved(ctx, &pubdto.RenameReservedReq{
			ProjectID: page.ProjectID, PageID: page.ID,
			OldPath: oldPath, NewPath: newPath,
		})
		if _, err = s.routes.Activate(ctx, &pubdto.ActivateReq{
			ProjectID: page.ProjectID, Path: newPath,
			PageID: page.ID, ArtifactID: artifactIDOf(st),
		}); err != nil {
			return nil, err
		}
		if oldPath != newPath {
			if req.WithRedirect {
				if err = s.ensureRedirectRoute(ctx, page, publishedPath); err != nil {
					return nil, err
				}
			} else if err = s.routes.Deactivate(ctx, &pubdto.DeactivateReq{
				ProjectID: page.ProjectID, Path: publishedPath,
			}); err != nil {
				return nil, err
			}
		}
	}
	status := pipeline.StatePublished
	if st != nil && st.Status != "" {
		status = st.Status
	}
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
	recorded, err := s.artifacts.Record(ctx, &artifactdto.RecordReq{
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

func artifactIDOf(rec *pipeline.PageRecord) string {
	if rec == nil || rec.ActiveHash == "" {
		return ""
	}
	return rec.ActiveHash
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
