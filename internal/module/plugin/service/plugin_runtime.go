package pluginservice

// plugin_runtime.go — 编译装配：启用插件集 → 模板 FS + 组件规格 + 检查器 schema。
// 供 dashboard 预览与 page 构建路径注入（WithPluginResolver + NewCompositeSet）。
// 确定性：同一 (plugin_id, version, manifest) 集构建结果恒定（ListEnabled 字典序）。

import (
	"context"
	"os"
	"sort"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/plugincomp"
	plugincontract "go_wp/internal/module/plugin/contract"
	plugindto "go_wp/internal/module/plugin/dto"
	"go_wp/internal/templates"
)

// EnabledAssembly 构建启用插件的编译装配素材。
func (s *Service) EnabledAssembly(ctx context.Context) (asm *plugincontract.Assembly, err error) {
	rows, err := s.m.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	asm = &plugincontract.Assembly{
		PluginFS:         make([]templates.PluginFS, 0, len(rows)),
		Specs:            make(map[string]*core.PluginComponentSpec),
		InspectorSchemas: make(map[string][]byte),
		Components:       make([]plugindto.ComponentSummary, 0),
		Presets:          make([]plugindto.PresetSummary, 0),
	}
	for _, row := range rows {
		// 存储目录缺失（被手动清理）→ 跳过该插件并保持注册行（管理员可重装）。
		if st, serr := os.Stat(row.StoragePath); serr != nil || !st.IsDir() {
			continue
		}
		manifest, perr := plugincomp.ParseManifest(row.Manifest)
		if perr != nil {
			continue // 注册时已校验；此处防御（manifest 篡改）静默跳过
		}
		asm.PluginFS = append(asm.PluginFS, templates.PluginFS{
			ID: manifest.ID, FS: os.DirFS(row.StoragePath),
		})
		for t, spec := range plugincomp.BuildSpecs(manifest) {
			asm.Specs[t] = spec
		}
		for t, data := range plugincomp.InspectorSchema(manifest) {
			asm.InspectorSchemas[t] = data
		}
		for _, c := range manifest.Components {
			asm.Components = append(asm.Components, plugindto.ComponentSummary{
				Type:  plugincomp.TypeOf(manifest.ID, c.Name),
				Label: c.Label,
				Hint:  orDefault(c.Hint, "插件组件"),
				Props: defaultProps(c.Props),
			})
		}
		// 区块预设：保持 manifest 声明顺序；跨插件按 ListEnabled 字典序自然有序。
		for _, p := range manifest.Presets {
			asm.Presets = append(asm.Presets, plugindto.PresetSummary{
				ID:        p.ID,
				Label:     p.Label,
				Category:  p.Category,
				Thumbnail: p.Thumbnail,
				Document:  p.Document,
			})
		}
	}
	// 组件摘要按类型排序（palette 注入确定性）。
	sort.Slice(asm.Components, func(i, j int) bool { return asm.Components[i].Type < asm.Components[j].Type })
	return asm, nil
}

// defaultProps 组件插入时的初始 props（manifest 各控件 default）。
func defaultProps(props map[string]plugincomp.PropControl) map[string]any {
	out := make(map[string]any, len(props))
	for k, ctl := range props {
		if ctl.Default != nil {
			out[k] = ctl.Default
		}
	}
	return out
}

// orDefault 空串兜底。
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// mapResolver Assembly.Specs 的 core.PluginResolver 实现（供注入 builder）。
type mapResolver map[string]*core.PluginComponentSpec

// LookupPluginComponent 见 core.PluginResolver。
func (m mapResolver) LookupPluginComponent(typeName string) (*core.PluginComponentSpec, bool) {
	spec, ok := m[typeName]
	return spec, ok
}

// AssemblyResolver 构建 core.PluginResolver（nil 安全：无插件时返回空 resolver）。
// 独立函数而非 Assembly 方法（类型定义在 contract 包，方法应贴近定义处）。
func AssemblyResolver(a *plugincontract.Assembly) core.PluginResolver {
	if a == nil || len(a.Specs) == 0 {
		return mapResolver{}
	}
	return mapResolver(a.Specs)
}
