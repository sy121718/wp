package projectservice

import (
	"context"

	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/utils"
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
			CreatedAt: utils.NewJSONTime(entities[i].CreatedAt), UpdatedAt: utils.NewJSONTime(entities[i].UpdatedAt),
		})
	}
	return res, nil
}

// ListRetentionPolicies 列出启用访问明细自动清理的工程（保留期 > 0）。
//
// analytics 的保留期清理（PurgeExpiredViews）经契约消费这份清单：保留期这一列长在
// projects 表上，读它就该留在本模块，而不是让 analytics 的 model 越过模块边界查表。
//
// 只返回两个字段：调用方要的是「清哪个工程、保留多少天」，其余工程字段与它无关。
func (s *Service) ListRetentionPolicies(ctx context.Context) (res []projectdto.RetentionPolicyResp, err error) {
	entities, err := s.model.ListRetentionPolicies(ctx)
	if err != nil {
		return nil, err
	}
	res = make([]projectdto.RetentionPolicyResp, 0, len(entities))
	for i := range entities {
		res = append(res, projectdto.RetentionPolicyResp{
			ProjectID:     entities[i].ID,
			RetentionDays: entities[i].AnalyticsRetentionDays,
		})
	}
	return res, nil
}
