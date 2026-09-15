// Package plugincontract 插件模块对外契约。
package plugincontract

import (
	"context"

	"go_wp/internal/builder/plugincomp"
	"go_wp/internal/builder/source"
	admincontract "go_wp/internal/module/admin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	"go_wp/internal/templates"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import plugin/dto。
type (
	PluginResp       = plugindto.PluginResp
	ToggleReq        = plugindto.ToggleReq
	UninstallReq     = plugindto.UninstallReq
	DetailReq        = plugindto.DetailReq
	ComponentSummary = plugindto.ComponentSummary
	PresetSummary    = plugindto.PresetSummary
)

// PluginService 插件管理契约：安装/列表/启停/卸载 + 编译装配查询。
// 英文入口：docs/plugin-development.en.md。
//
// 编译装配（EnabledAssembly）供 dashboard 预览与 page 构建路径注入：
// 返回启用插件的模板文件系统 + 组件规格 + 检查器 schema 的合并素材。
type PluginService interface {
	// Install 安装/升级插件（zip 字节）。
	Install(ctx context.Context, zipBytes []byte) (res *plugindto.PluginResp, err error)
	// List 全部插件列表。
	List(ctx context.Context) (list []*plugindto.PluginResp, err error)
	// Toggle 启停插件。
	Toggle(ctx context.Context, req *plugindto.ToggleReq) (err error)
	// Uninstall 卸载插件（删注册行 + 级联删存储目录）。
	Uninstall(ctx context.Context, req *plugindto.UninstallReq) (err error)
	// Detail 插件详情（含 manifest）。
	Detail(ctx context.Context, req *plugindto.DetailReq) (res *plugindto.PluginResp, err error)
	// EnabledAssembly 启用插件的编译装配素材（模板 FS + 组件规格 + 检查器 schema）。
	EnabledAssembly(ctx context.Context) (asm *Assembly, err error)
	// AdminAuthz 返回注入的 admin 权限上下文查询服务。
	// 插件是外部插件宿主：插件运行时经此读取当前用户角色/权限/超管上下文，
	// 不直接依赖 admin 的 model/service。未注入时返回 nil，调用方需判空降级。
	AdminAuthz() admincontract.AuthzContextService
}

// Assembly 编译装配素材（按启用插件集构建，确定性：同插件版本集恒定）。
// 英文入口：docs/plugin-development.en.md。
type Assembly struct {
	// Fingerprint 启用插件集指纹（由 plugin service 计算，审计 PERF-006）。
	//
	// 用途是让下游按「同一启用集」复用重型派生物（Jet Set = 模板解析结果）。
	// 空串表示调用方没有提供指纹 —— 下游必须退化为「每次都重建」，
	// 不能把空串当成一个合法版本（否则所有实例会共享同一个缓存槽）。
	Fingerprint string
	// PluginFS 插件模板文件系统（templates.NewCompositeSet 的输入）。
	PluginFS []templates.PluginFS
	// Specs 组件规格（构建 resolver 的素材，键 = 完整类型标识）。
	//
	// 类型取 source.PluginComponentSpec（零依赖共享形状包）而不是 core 里的同名类型：
	// 契约包不得反向 import builder/core（AGENTS.md 不变量 7，一旦反向即成环，
	// core 就再也无法持有业务契约）。core 侧的同名类型是它的别名，
	// 装配层写 core.Xxx 与这里指的是**同一份定义**，不需要任何转换。
	Specs map[string]*source.PluginComponentSpec
	// InspectorSchemas 检查器 schema（与内置 ComponentSchemas 合并进 wb-schemas）。
	InspectorSchemas map[string][]byte
	// Components 工作台组件库摘要（palette 注入）。
	Components []plugindto.ComponentSummary
	// Presets 区块预设摘要（palette「区块预设」分组注入，document 为预组合 AST 片段）。
	Presets []plugindto.PresetSummary
	// ExtraCSS 插件静态样式（assets/*.css，按启用插件字典序拼接；
	// 构建期经 WithExtraCSS 注入产物主 CSS 之后，docs/06 §5.1 资产规范）。
	ExtraCSS []string
}

// ManifestAlias manifest 类型的模块间传递形态（详情接口暴露）。
// 英文入口：docs/plugin-development.en.md。
type ManifestAlias = plugincomp.Manifest

// pluginSpecResolver 实现 source.PluginResolver：按类型标识查组件规格。
type pluginSpecResolver map[string]*source.PluginComponentSpec

// LookupPluginComponent 见 source.PluginResolver。
func (m pluginSpecResolver) LookupPluginComponent(typeName string) (*source.PluginComponentSpec, bool) {
	spec, ok := m[typeName]
	return spec, ok
}

// AssemblyResolver 构建 source.PluginResolver（nil 安全：无插件时返回空 resolver）。
// 英文入口：docs/plugin-development.en.md。
//
// 返回值可直接喂 builder.WithPluginResolver：core.PluginResolver 是
// source.PluginResolver 的别名（共享形状在 source，见 AGENTS.md 不变量 7），
// 两边是同一个接口类型，装配处不需要适配层。
//
// 放在 contract 包而非 service 包：page / dashboard 构建路径需要把 Assembly
// 注入 builder，若直接依赖 plugin/service 的工厂函数，将违反「跨模块只依赖
// contract」约束。Assembly 类型本就在 contract 包，工厂贴近定义处更合理。
func AssemblyResolver(a *Assembly) source.PluginResolver {
	if a == nil || len(a.Specs) == 0 {
		return pluginSpecResolver{}
	}
	return pluginSpecResolver(a.Specs)
}
