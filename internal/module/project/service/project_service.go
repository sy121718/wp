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
	// structureTemplateOpts 结构模板候选端口（主题设置页的「选结构模板」下拉，装配期注入）。
	//
	// 可空（非 required-port）：未注入时下拉只有「不绑定」一项、并留一行 Warn，
	// 而不是把「保存主题设置」整件事挡住 —— 这是配置面变窄，不是数据风险。
	structureTemplateOpts projectcontract.StructureTemplateOptionsPort
}

// SetStructureTemplateOptionsPort 注入结构模板候选端口（装配期调用）。
func (s *Service) SetStructureTemplateOptionsPort(p projectcontract.StructureTemplateOptionsPort) {
	s.structureTemplateOpts = p
}

// SetLocaleRetirePort 注入语言下线端口（装配期调用；**必须注入**，理由见字段注释）。
//
// 装配自检：routes.go 在提供方未实现该契约时 panic（审计 CQ-019），
// 清单登记在 internal/routers/wiring.go 的 wiringManifest。
func (s *Service) SetLocaleRetirePort(port projectcontract.LocaleRetirePort) { s.retire = port }

// NewService 创建站点工程服务。
func NewService(model *projectmodel.Model) *Service {
	return &Service{model: model}
}
