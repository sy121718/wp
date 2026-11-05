package blockservice

import (
	"context"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/source"
)

func (s *Service) ListActionTargetReferences(ctx context.Context, projectID, targetKind, targetID string) ([]source.ActionTargetReference, error) {
	rows, err := s.model.ListByProject(ctx, projectID, "", "", "")
	if err != nil {
		return nil, err
	}
	var out []source.ActionTargetReference
	for _, row := range rows {
		hit, err := builder.ReferencesActionTarget(row.Document, targetKind, targetID)
		if err != nil {
			return nil, err
		}
		if hit {
			out = append(out, source.ActionTargetReference{SourceKind: "block", SourceID: row.ID})
		}
	}

	return out, nil
}
