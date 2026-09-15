package pageservice

// page_publish_kernel.go — 编译内核与历史内核还原（内核版本号、快照还原、发布态判定）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pagemodel "go_wp/internal/module/page/model"

	"go_wp/internal/pipeline"
)

// syncKernel 把页面当前草稿同步进内核记录（幂等；版本号以内核为准续增）。
// path 为实际访问路径（多语言下带 /{lang}/ 前缀），lang 为构建语言：
// 两者一起进入内核记录，决定 Manifest.lang 与激活路径。
func (s *Service) syncKernel(path, lang string, doc json.RawMessage, pageID string) error {
	draft := pipeline.Draft{Path: path, Lang: lang, DocJSON: doc}
	st, err := s.publisher.Status(pageID)
	if errors.Is(err, pipeline.ErrPageNotFound) {
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	if err != nil {
		return err
	}
	if st.Path != path || st.Lang != lang {
		// 内核记录路径/语言落后于数据库（如改 URL 中断恢复、语言切换）：整体重建。
		s.publisher.LoadRecord(&pipeline.PageRecord{ID: pageID})
		_, err = s.publisher.SaveDraftInput(pageID, 0, draft)
		return err
	}
	_, err = s.publisher.SaveDraftInput(pageID, st.Version, draft)
	return err
}

// restoreKernelForHistory 以目标产物为基线重建内核记录（回滚前置）。
func (s *Service) restoreKernelForHistory(page *pagemodel.PageEntity, target *artifactcontract.ArtifactResp) error {
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
		art, err := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: *page.ActiveArtifactID})
		if err != nil {
			// 活动产物行缺失是数据不一致（产物行被删而指针未清）：显式失败而非
			// 降级为纯草稿——否则 histories/activeHash 留空，UpdateURL 误判纯草稿，
			// 只迁 draft_path 不构建不激活，线上旧 URL 继续出旧内容。
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
	}
	s.publisher.LoadRecord(rec)
	return nil
}

// kernelVersion 读取内核记录当前版本（不存在视为 1）。
func (s *Service) kernelVersion(pageID string) int {
	if st, err := s.publisher.Status(pageID); err == nil && st.Version > 0 {
		return st.Version
	}
	return 1
}

// pageHasPublishedState 判断页面是否曾上线（与 pipeline.PageRecord.hasPublishedHistory 口径一致）。
// UpdateURL 对纯草稿（从未发布）页面只迁移路径与保留路由，不构建不激活。
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
