// Package service 承载 workbench 模块的编排与纯计算：跨模块取数、面板与画布数据装配、
// 预览文档分类、画布元数据构建。
//
// 边界（与 internal/module/CLAUDE.md 一致）：
//   - 本包不知道 HTTP 的存在 —— 不 import gin、不写响应、不读请求、不持 *gorm.DB；
//   - 取词只经 Translate 回调（由 inbound/http 侧按当前请求语言固化，本包不管兜底与词条来源）；
//   - 跨模块取数只经各模块 contract，不直连别人的 model。
//
// 依赖全部可为「未装配」：取数缺失时按各调用点既有的降级口径处理（返回空列表 / nil），
// 与搬迁前 handler 内联实现逐字一致。
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

// Translate 是 service 层唯一需要的取词能力：key → 当前语言文案。
//
// 由 handler 绑定请求语言与中文兜底（workbenchTrFunc），本包只负责调用 ——
// 面板 / 结构树 / 重复项的 HTML 由 Go 拼串产出，模板层不参与这些句子，
// 所以取词必须在拼串处完成；而包本身不认识 gin 上下文。
type Translate func(key string) string

// NavigationPickerPort 导航菜单项能力（消费者侧最窄接口：列 + 建）。
//
// 与 inbound/http 的同名接口形状一致 —— 装配侧注入的实现可直接赋给两者。
type NavigationPickerPort interface {
	List(ctx context.Context, req *navigationdto.ListReq) (list []*navigationdto.NavigationResp, err error)
	Create(ctx context.Context, req *navigationdto.CreateReq) (res *navigationdto.NavigationResp, err error)
}

// Service 工作台编排服务；由 inbound/http 的 Handle 持有，
// handler 只做「解析请求 → 调 service → 渲染」。
type Service struct {
	pages            pagecontract.PageService
	projects         projectcontract.ProjectService
	blocks           blockcontract.BlockService
	contentTemplates contenttemplatecontract.ContentTemplateService
	products         productcontract.ProductDataSource
	navigations      NavigationPickerPort
	plugins          plugincontract.PluginService
}

// New 构造编排服务（未装配的依赖传 nil，调用点按既有口径降级）。
func New(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, contentTemplates contenttemplatecontract.ContentTemplateService,
	products productcontract.ProductDataSource, navigations NavigationPickerPort) *Service {
	return &Service{
		pages: pages, projects: projects, blocks: blocks,
		contentTemplates: contentTemplates, products: products, navigations: navigations,
	}
}

// SetNavigationPicker 注入导航菜单项端口（装配期调用；未注入时下拉为空）。
func (s *Service) SetNavigationPicker(p NavigationPickerPort) {
	if s == nil {
		return
	}
	s.navigations = p
}

// SetProducts 注入商品数据源（entityref 下拉的动态来源）。
func (s *Service) SetProducts(p productcontract.ProductDataSource) {
	if s == nil {
		return
	}
	s.products = p
}

// SetPlugins 注入插件契约（画布装配素材的来源）。
func (s *Service) SetPlugins(p plugincontract.PluginService) {
	if s == nil {
		return
	}
	s.plugins = p
}

// NavigationPicker 导航菜单项端口（未装配时为 nil）。
//
// 端口留在 service，但**HTTP 出口**（检查器内就地新建菜单项的 JSON 响应、错误文案出口
// 要断言 navigationcontract.FacingTexter）由 handler 自己完成 —— 那是「写哪种状态码、
// 取哪一条文案」的关切，不属于编排。
func (s *Service) NavigationPicker() NavigationPickerPort {
	if s == nil {
		return nil
	}
	return s.navigations
}

// ContentTemplatesReady 内容模板契约是否已装配。
//
// 模板画布入口据此给 503（而不是 deref nil 打成 500）：装配缺失是部署形态，
// 不是请求错误。
func (s *Service) ContentTemplatesReady() bool {
	return s != nil && s.contentTemplates != nil
}
