package pageservice

import (
	"context"

	"go_wp/internal/pipeline"
)

func (s *Service) freezeReferences(ctx context.Context, projectID string, doc []byte) (*pipeline.ReferenceSnapshot, error) {
	if frozen := pipeline.ReferenceSnapshotFromContext(ctx, projectID); frozen != nil {
		return frozen, nil
	}
	var navigation *pipeline.NavigationAdapter
	if s.navigation != nil {
		navigation = &pipeline.NavigationAdapter{Svc: s.navigation, Project: s.project, Ctx: ctx, ProjectID: projectID}
	}
	if navigation != nil {
		return pipeline.FreezeReferences(ctx, projectID, doc, s.blocks, s.structureTemplates, navigation)
	}
	return pipeline.FreezeReferences(ctx, projectID, doc, s.blocks, s.structureTemplates, nil)
}
