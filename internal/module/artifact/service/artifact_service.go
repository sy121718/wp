package artifactservice

import (
	"context"

	artifactdto "go_wp/internal/module/artifact/dto"

	artifactcontract "go_wp/internal/module/artifact/contract"
	artifactmodel "go_wp/internal/module/artifact/model"
)

var (
	_ artifactcontract.ArtifactService    = (*Service)(nil)
	_ artifactcontract.PageArtifactReader = (*Service)(nil)
)

// Service 构建产物归档业务服务。
type Service struct {
	model *artifactmodel.Model
	// externalRefs 其它模块对共享内容对象的引用来源（见 artifact_content_gc.go）。
	// nil = 确认没有外部引用来源，不是「没接上」。
	externalRefs ExternalContentRefs
}

// NewService 创建 Artifact 服务。
func NewService(model *artifactmodel.Model) *Service {
	return &Service{model: model}
}

// Model 暴露底层 model 供测试检查闭包表。
func (s *Service) Model() *artifactmodel.Model {
	return s.model
}

// ---- PageArtifactReader：页面产物元数据的只读视图（page 模块经契约读取） ----

// ListPageArtifactHashes 列出全部认领中的产物 hash。
func (s *Service) ListPageArtifactHashes(ctx context.Context) (hashes []string, err error) {
	return s.model.ListPageArtifactHashes(ctx)
}

// PageArtifactHashByID 按产物行 id 取 hash（不存在时返回空串）。
func (s *Service) PageArtifactHashByID(ctx context.Context, id string) (hash string, err error) {
	return s.model.PageArtifactHashByID(ctx, id)
}

// PageArtifactPageID 取产物行挂在哪张页面上（不存在时返回空串）。
func (s *Service) PageArtifactPageID(ctx context.Context, id string) (pageID string, err error) {
	return s.model.PageArtifactPageID(ctx, id)
}

// TranslationMisses 取给定页面「每个 (page_id, lang) 最新产物」的缺译计数（只含 misses > 0）。
func (s *Service) TranslationMisses(ctx context.Context, pageIDs []string) (rows []artifactdto.PageArtifactMissRow, err error) {
	raw, err := s.model.TranslationMisses(ctx, pageIDs)
	if err != nil {
		return nil, err
	}
	rows = make([]artifactdto.PageArtifactMissRow, 0, len(raw))
	for i := range raw {
		rows = append(rows, artifactdto.PageArtifactMissRow{
			PageID:     raw[i].PageID,
			Lang:       raw[i].Lang,
			Version:    raw[i].Version,
			Misses:     raw[i].Misses,
			Candidates: raw[i].Candidates,
		})
	}
	return rows, nil
}
