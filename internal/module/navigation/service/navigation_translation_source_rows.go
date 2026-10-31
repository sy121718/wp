package navigationservice

import (
	"context"

	navigationdto "go_wp/internal/module/navigation/dto"
	navigationmodel "go_wp/internal/module/navigation/model"
)

type sourceRows interface {
	ListForKind(ctx context.Context, projectID, kind string) (rows []*navigationmodel.NavigationEntity, err error)
}

type navigationRowsModel struct {
	m *navigationmodel.Model
}

func (r navigationRowsModel) ListForKind(ctx context.Context, projectID, kind string) ([]*navigationmodel.NavigationEntity, error) {
	return r.m.List(ctx, projectID, kind)
}

var _ sourceRows = navigationRowsModel{}

func navNodesOfRows(rows []*navigationmodel.NavigationEntity) []*navigationdto.NavigationNode {
	return buildTree(rows)
}
