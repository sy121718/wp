package pageservice

import (
	"context"
	"fmt"
	"go_wp/internal/builder"
	"go_wp/internal/builder/source"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/pipeline"
	"strings"
)

func (s *Service) ListActionTargetReferences(ctx context.Context, projectID, targetKind, targetID string) ([]source.ActionTargetReference, error) {
	rows, err := s.model.ListDraftDocuments(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var out []source.ActionTargetReference
	for _, row := range rows {
		hit, err := builder.ReferencesActionTarget(row.DraftDocument, targetKind, targetID)
		if err != nil {
			return nil, err
		}
		if hit {
			out = append(out, source.ActionTargetReference{SourceKind: "page", SourceID: row.ID})
		}
	}

	key := pipeline.PageTargetKey(targetID)
	if targetKind == "presentation" {
		key = pipeline.PresentationTargetKey(targetID)
	}
	compiled, err := s.model.ListPagesByDependency(ctx, projectID, key.Kind, key.Key)
	if err != nil {
		return nil, err
	}
	for _, row := range compiled {
		out = append(out, source.ActionTargetReference{SourceKind: "page", SourceID: row.ID})
	}

	return out, nil
}

func (s *Service) SetActionTargetReferences(reader source.ActionTargetReferenceReader) {
	s.actionTargetReferences = reader
}
func (s *Service) SetActionTargetInvalidator(invalidator source.ActionTargetInvalidator) {
	s.actionTargetInvalidator = invalidator
}

func (s *Service) ensureActionTargetUnused(ctx context.Context, projectID, id string) error {
	reader := s.actionTargetReferences

	if reader == nil {
		reader = s
	}
	refs, err := reader.ListActionTargetReferences(ctx, projectID, "page", id)
	if err != nil {
		return err
	}
	var items []string
	for _, ref := range refs {
		if ref.SourceKind == "page" && ref.SourceID == id {
			continue
		}
		items = append(items, ref.SourceKind+"/"+ref.SourceID)
	}
	if len(items) > 0 {
		return fmt.Errorf("%w: %s", pageenums.ActionTargetInUse(), strings.Join(items, ", "))
	}
	return nil
}

func (s *Service) invalidateActionTarget(ctx context.Context, id string) {
	if s.actionTargetInvalidator != nil {
		key := pipeline.PageTargetKey(id)
		s.actionTargetInvalidator.Invalidate(ctx, key.Kind, key.Key)
	}
}
