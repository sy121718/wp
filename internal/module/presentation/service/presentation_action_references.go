package presentationservice

import (
	"context"
	"fmt"
	"go_wp/internal/builder"
	"go_wp/internal/builder/source"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/internal/pipeline"
	"strings"
)

func (s *Service) ListActionTargetReferences(ctx context.Context, projectID, targetKind, targetID string) ([]source.ActionTargetReference, error) {
	rows, err := s.m.ListInstances(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	var out []source.ActionTargetReference
	for _, row := range rows {
		if len(row.OverrideDocument) == 0 {
			continue
		}
		hit, err := builder.ReferencesActionTarget(row.OverrideDocument, targetKind, targetID)
		if err != nil {
			return nil, err
		}
		if hit {
			out = append(out, source.ActionTargetReference{SourceKind: "presentation", SourceID: row.ID})
		}
	}

	key := pipeline.PageTargetKey(targetID)
	if targetKind == "presentation" {
		key = pipeline.PresentationTargetKey(targetID)
	}
	compiled, err := s.m.ListTargetDependencyInstances(ctx, projectID, key.Kind, key.Key)
	if err != nil {
		return nil, err
	}
	for _, id := range compiled {
		out = append(out, source.ActionTargetReference{SourceKind: "presentation", SourceID: id})
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
	refs, err := reader.ListActionTargetReferences(ctx, projectID, "presentation", id)
	if err != nil {
		return err
	}
	var items []string
	for _, ref := range refs {
		if ref.SourceKind == "presentation" && ref.SourceID == id {
			continue
		}
		items = append(items, ref.SourceKind+"/"+ref.SourceID)
	}
	if len(items) > 0 {
		return fmt.Errorf("%w: %s", presentationenums.ActionTargetInUse(), strings.Join(items, ", "))
	}
	return nil
}

func (s *Service) invalidateActionTarget(ctx context.Context, id string) {
	if s.actionTargetInvalidator != nil {
		key := pipeline.PresentationTargetKey(id)
		s.actionTargetInvalidator.Invalidate(ctx, key.Kind, key.Key)
	}
}
