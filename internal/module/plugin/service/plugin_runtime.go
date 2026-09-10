package pluginservice

// plugin_runtime.go — 编译装配：启用插件集 → 模板 FS + 组件规格 + 检查器 schema。
// 供 dashboard 预览与 page 构建路径注入（WithPluginResolver + NewCompositeSet）。
// 确定性：同一 (plugin_id, version, manifest) 集构建结果恒定（ListEnabled 字典序）。

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

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
		// 插件静态样式（docs/06 §5.1 资产规范）：assets/*.css 按文件名序拼接，
		// 构建期注入产物主 CSS 之后。文件缺失/目录缺失 = 无样式，静默跳过。
		if css := pluginExtraCSS(manifest.ID, row.StoragePath); css != "" {
			asm.ExtraCSS = append(asm.ExtraCSS, css)
		}
	}
	// 组件摘要按类型排序（palette 注入确定性）。
	slices.SortFunc(asm.Components, func(a, b plugindto.ComponentSummary) int {
		return strings.Compare(a.Type, b.Type)
	})
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

// cssImportRe 匹配 @import 规则（大小写不敏感，覆盖 @import url(...) 与 @import "..."）。
var cssImportRe = regexp.MustCompile(`(?i)@import[^;]*;?`)

// pluginExtraCSS 读取插件包 assets/*.css 并按文件名序拼接（含来源注释头）。
// 清洗 </style 防止逃逸产物 <style> 块（管理员级信任仍做防御性清洗）。
// 确定性：文件名序 + 拼接顺序固定，同一插件版本恒同字节。
func pluginExtraCSS(pluginID, storagePath string) string {
	entries, err := os.ReadDir(filepath.Join(storagePath, "assets"))
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".css") {
			continue
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		data, rerr := os.ReadFile(filepath.Join(storagePath, "assets", name))
		if rerr != nil {
			continue // 单文件读取失败跳过，不阻断其他资产
		}
		css := strings.ReplaceAll(string(data), "</style", "")
		// 禁 @import：外部样式引用构成数据外泄/追踪通道（插件为管理员级信任，
		// 仍做纵深防御；站内资产请用 <link> 由平台统一管理）。
		css = cssImportRe.ReplaceAllString(css, "")
		sb.WriteString("/* plugin:" + pluginID + ":" + name + " */\n")
		sb.WriteString(css)
		sb.WriteString("\n")
	}
	return sb.String()
}
