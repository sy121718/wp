package projectservice

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
)

// List 列出全部站点工程。
func (s *Service) List(ctx context.Context) (res []projectdto.ProjectResp, err error) {
	entities, err := s.model.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.ProjectResp, 0, len(entities))
	for i := range entities {
		res = append(res, projectdto.ProjectResp{
			ID: entities[i].ID, Name: entities[i].Name, Settings: entities[i].Settings,
			CreatedAt: entities[i].CreatedAt, UpdatedAt: entities[i].UpdatedAt,
		})
	}
	return res, nil
}
