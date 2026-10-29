package productservice

import (
	"context"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

func ProjectExists(projects []projectcontract.ProjectResp, id string) bool {
	for i := range projects {
		if projects[i].ID == id {
			return true
		}
	}
	return false
}

func (s *Service) SetWarehouseOptionsPort(port productcontract.WarehouseOptionsPort) {
	s.pageWarehouses = port
}
func (s *Service) SetEntityTemplatePort(port productcontract.EntityTemplatePort) {
	s.pageTemplates = port
}

func (s *Service) CatalogWarehouses(ctx context.Context, projectID string) ([]productdto.WarehouseOptionResp, error) {
	if s.pageWarehouses == nil || projectID == "" {
		return []productdto.WarehouseOptionResp{}, nil
	}
	return s.pageWarehouses.WarehouseOptions(ctx, projectID)
}

func (s *Service) CatalogWarehouseSKUs(ctx context.Context, projectID string, warehouses []productdto.WarehouseOptionResp) ([]productdto.WarehouseSKUGroupResp, error) {
	if s.pageWarehouses == nil || projectID == "" {
		return []productdto.WarehouseSKUGroupResp{}, nil
	}
	return s.pageWarehouses.WarehouseSKUOptions(ctx, projectID, warehouses)
}

func (s *Service) CatalogEntityTemplates(ctx context.Context, projectID string) (*productdto.EntityTemplateCatalogResp, error) {
	result := &productdto.EntityTemplateCatalogResp{Items: []productdto.EntityTemplateOptionResp{}}
	if s.pageTemplates == nil || projectID == "" {
		return result, nil
	}
	result.Available = true
	var err error
	result.Items, err = s.pageTemplates.EntityTemplates(ctx, projectID, productcontract.EntityTypeProduct)
	if err != nil {
		return nil, err
	}
	result.DefaultID, err = s.pageTemplates.DefaultEntityTemplate(ctx, projectID, productcontract.EntityTypeProduct)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// QueryDetailTemplatePanel 直接返回发布侧原始绑定事实，不复制成页面面板形状。
func (s *Service) QueryDetailTemplatePanel(ctx context.Context, projectID, productID string) (productcontract.DetailTemplatePanelFacts, error) {
	if s.pageDetailTemplates == nil {
		return productcontract.DetailTemplatePanelFacts{}, nil
	}
	return s.pageDetailTemplates.DetailTemplatePanel(ctx, projectID, productcontract.EntityTypeProduct, productID)
}
