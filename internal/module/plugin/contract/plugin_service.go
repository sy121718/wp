// Package plugincontract 插件模块对外契约。
package plugincontract

import (
	"context"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/plugincomp"
	plugindto "go_wp/internal/module/plugin/dto"
	"go_wp/internal/templates"
)

// PluginService 插件管理契约：安装/列表/启停/卸载 + 编译装配查询。
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
}

// Assembly 编译装配素材（按启用插件集构建，确定性：同插件版本集恒定）。
type Assembly struct {
	// PluginFS 插件模板文件系统（templates.NewCompositeSet 的输入）。
	PluginFS []templates.PluginFS
	// Specs 组件规格（构建 resolver 的素材，键 = 完整类型标识）。
	Specs map[string]*core.PluginComponentSpec
	// InspectorSchemas 检查器 schema（与内置 ComponentSchemas 合并进 wb-schemas）。
	InspectorSchemas map[string][]byte
	// Components 工作台组件库摘要（palette 注入）。
	Components []plugindto.ComponentSummary
	// Presets 区块预设摘要（palette「区块预设」分组注入，document 为预组合 AST 片段）。
	Presets []plugindto.PresetSummary
}

// ManifestAlias manifest 类型的模块间传递形态（详情接口暴露）。
type ManifestAlias = plugincomp.Manifest

// pluginSpecResolver 实现 core.PluginResolver：按类型标识查组件规格。
type pluginSpecResolver map[string]*core.PluginComponentSpec

// LookupPluginComponent 见 core.PluginResolver。
func (m pluginSpecResolver) LookupPluginComponent(typeName string) (*core.PluginComponentSpec, bool) {
	spec, ok := m[typeName]
	return spec, ok
}

// AssemblyResolver 构建 core.PluginResolver（nil 安全：无插件时返回空 resolver）。
//
// 放在 contract 包而非 service 包：page / dashboard 构建路径需要把 Assembly
// 注入 builder，若直接依赖 plugin/service 的工厂函数，将违反「跨模块只依赖
// contract」约束。Assembly 类型本就在 contract 包，工厂贴近定义处更合理。
func AssemblyResolver(a *Assembly) core.PluginResolver {
	if a == nil || len(a.Specs) == 0 {
		return pluginSpecResolver{}
	}
	return pluginSpecResolver(a.Specs)
}
