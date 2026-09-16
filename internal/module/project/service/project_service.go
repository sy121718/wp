// Package projectservice 实现 project 模块业务用例。
package projectservice

import (
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
)

var _ projectcontract.ProjectService = (*Service)(nil)

// Service 站点工程业务服务。
type Service struct {
	model *projectmodel.Model
	// retire 语言下线端口（审计 I18N-017，装配期注入）。
	//
	// **必须注入**（审计 CQ-019 判为 required-port）：为空时禁用语言只记日志、不清路由 ——
	// 运营以为某个语言已下线，其实它的站点仍在线上可访问，且全程没有任何报错。
	// 实现方 page.Service 有编译期断言（var _ projectcontract.LocaleRetirePort），
	// 装配方 routes.go 在断言失败时直接 panic，故生产路径恒非 nil；
	// 本字段的 nil 分支只服务「不经装配、直接构造 Service」的纯单测。
	retire projectcontract.LocaleRetirePort
	// assets 主题包资产端口（审计 VIS-014，装配期注入）。
	//
	// 主题包要装的是「引用到的块文档 + 可选页面文档」，这两类资产分别属于 block / page
	// 模块；project 不越权碰它们的表，所以按既有形状声明窄端口由装配层注入（样板同为 retire）。
	// 未注入时导出/导入返回 ErrThemeBundlePortUnavailable —— 不静默降级成「只导令牌」，
	// 那会让第三方拿到一个看起来正常、实际缺页眉页脚的主题包。
	assets projectcontract.ThemeBundleAssetPort
}

// SetLocaleRetirePort 注入语言下线端口（装配期调用；**必须注入**，理由见字段注释）。
//
// 装配自检：routes.go 在提供方未实现该契约时 panic（审计 CQ-019），
// 清单登记在 internal/routers/wiring.go 的 wiringManifest。
func (s *Service) SetLocaleRetirePort(port projectcontract.LocaleRetirePort) { s.retire = port }

// SetThemeBundleAssetPort 注入主题包资产端口（装配期调用，见字段注释）。
//
// 装配方在 block / page 两侧都构造完成后调用：
//
//	projectService.SetThemeBundleAssetPort(projectservice.NewThemeBundleAssetPort(blockSvc, pageSvc))
func (s *Service) SetThemeBundleAssetPort(port projectcontract.ThemeBundleAssetPort) { s.assets = port }

// NewService 创建站点工程服务。
func NewService(model *projectmodel.Model) *Service {
	return &Service{model: model}
}
