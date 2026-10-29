package productservice

import (
	"context"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"strings"
)

// ResolveCatalogScope 解析商品查询的工程范围；显式无效工程不能降级成空清单。
func (s *Service) ResolveCatalogScope(ctx context.Context, projectID string) (*productcontract.CatalogScope, error) {
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	if projects == nil {
		projects = []projectcontract.ProjectResp{}
	}
	projectID = strings.TrimSpace(projectID)
	if projectID == "" && len(projects) > 0 {
		projectID = projects[0].ID
	}
	if projectID != "" {
		found := false
		for _, project := range projects {
			if project.ID == projectID {
				found = true
				break
			}
		}
		if !found {
			return nil, productenums.Err(productenums.ErrInvalidParam)
		}
	}
	return &productcontract.CatalogScope{Projects: projects, ProjectID: projectID}, nil
}
