package service

import (
	"context"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

type Translate func(key string) string

type NavigationPickerPort interface {
	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)
	Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error)
}

type Service struct {
	pages            pagecontract.PageService
	projects         projectcontract.ProjectService
	blocks           blockcontract.BlockService
	contentTemplates contenttemplatecontract.ContentTemplateService
	products         productcontract.ProductDataSource
	navigations      NavigationPickerPort
	plugins          plugincontract.PluginService
}

func New(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, contentTemplates contenttemplatecontract.ContentTemplateService,
	products productcontract.ProductDataSource, navigations NavigationPickerPort) *Service {
	return &Service{
		pages: pages, projects: projects, blocks: blocks,
		contentTemplates: contentTemplates, products: products, navigations: navigations,
	}
}

func (s *Service) SetNavigationPicker(p NavigationPickerPort) {
	if s == nil {
		return
	}
	s.navigations = p
}

func (s *Service) SetProducts(p productcontract.ProductDataSource) {
	if s == nil {
		return
	}
	s.products = p
}

func (s *Service) SetPlugins(p plugincontract.PluginService) {
	if s == nil {
		return
	}
	s.plugins = p
}

func (s *Service) NavigationPicker() NavigationPickerPort {
	if s == nil {
		return nil
	}
	return s.navigations
}

func (s *Service) ContentTemplatesReady() bool {
	return s != nil && s.contentTemplates != nil
}
