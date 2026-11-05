package contenttemplateservice

import (
	"context"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/source"
)

func (s *Service) ListActionTargetReferences(ctx context.Context, projectID, targetKind, targetID string) ([]source.ActionTargetReference, error) {
	rows, err := s.m.List(ctx, projectID, "")
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
			out = append(out, source.ActionTargetReference{SourceKind: "template", SourceID: row.ID})
		}
	}

	return out, nil
}
