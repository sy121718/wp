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

type Service struct {
	model *artifactmodel.Model

	externalRefs ExternalContentRefs
}

func NewService(model *artifactmodel.Model) *Service {
	return &Service{model: model}
}

func (s *Service) Model() *artifactmodel.Model {
	return s.model
}

func (s *Service) ListPageArtifactHashes(ctx context.Context) (hashes []string, err error) {
	return s.model.ListPageArtifactHashes(ctx)
}

func (s *Service) PageArtifactHashByID(ctx context.Context, id string) (hash string, err error) {
	return s.model.PageArtifactHashByID(ctx, id)
}

func (s *Service) PageArtifactPageID(ctx context.Context, id string) (pageID string, err error) {
	return s.model.PageArtifactPageID(ctx, id)
}

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
