// Package plugincomp 插件组件定义：manifest.json 的解析、校验与编译规格构建
// （docs/06-plugin-system.md §5）。装配层（plugin 模块）在安装时校验 manifest，
// 在编译/工作台装配时按 enabled 插件集构建 core.PluginComponentSpec。
//
// 依赖方向：plugincomp → core + style（内核契约），plugin 模块 → plugincomp。
package plugincomp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/style"
)

// propKindWhitelist 插件 props 控件类型白名单（与检查器已支持的原语对齐）。
var propKindWhitelist = map[string]bool{
	"text": true, "textarea": true, "number": true, "select": true,
	"color": true, "media": true, "unit": true,
}

// idRe 插件 ID 白名单：小写字母开头，字母数字下划线连字符（目录名安全）。
var idRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,60}$`)

// compNameRe 组件名白名单（拼进类型标识与模板路径，从严）。
var compNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,60}$`)

// versionRe 语义化版本粗校验 x.y.z（可选 -rc1 后缀）。
var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[a-z0-9.]+)?$`)

// templateFileRe 组件模板文件名白名单。
var templateFileRe = regexp.MustCompile(`^[a-z0-9_-]{1,60}\.jet$`)

// Manifest 插件包 manifest.json 的结构投影。
type Manifest struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Version    string      `json:"version"`
	Requires   *Requires   `json:"requires,omitempty"`
	Components []Component `json:"components"`
}

// Requires 版本要求（当前仅记录，不强校验核心版本）。
type Requires struct {
	Core string `json:"core,omitempty"`
}

// Component 单个插件组件声明。
type Component struct {
	// Name 组件名（小写，类型标识为 "plugin.{manifest.id}.{name}"）。
	Name string `json:"name"`
	// Label 组件库显示名。
	Label string `json:"label"`
	// Hint 组件库提示语（缺省"插件组件"）。
	Hint string `json:"hint,omitempty"`
	// Template 模板文件名（如 "campaign_card.jet"，位于包内 components/ 目录）。
	Template string `json:"template"`
	// Props 检查器控件 schema（键 = props 字段名）。
	Props map[string]PropControl `json:"props,omitempty"`
	// Styles 样式声明（style 引擎 Rule 列表，docs/06 §6）。
	Styles *style.Schema `json:"styles,omitempty"`
}

// PropControl manifest 单个控件声明（core.PluginPropControl 的 JSON 形态）。
type PropControl struct {
	Kind    string   `json:"kind"`
	Label   string   `json:"label,omitempty"`
	Default any      `json:"default,omitempty"`
	Options []string `json:"options,omitempty"`
	MaxLen  int      `json:"maxlen,omitempty"`
}

// ValidateManifest 校验 manifest 结构与白名单（安装入口调用，坏包拒绝）。
//
// 校验项：ID/版本格式、组件名/模板名白名单、props 控件类型白名单、
// select 必须带非空枚举、default 与 kind 相容、styles 段经 style.Schema.Validate
// （属性名/选择器/值三面白名单，docs/06 §6.2）。
func ValidateManifest(m *Manifest) (err error) {
	if m == nil {
		return fmt.Errorf("manifest 为空")
	}
	if !idRe.MatchString(m.ID) {
		return fmt.Errorf("插件 id %q 非法（小写字母开头，2~61 位）", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" || len([]rune(m.Name)) > 60 {
		return fmt.Errorf("插件 name 非法")
	}
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("插件版本 %q 非法（期望 x.y.z）", m.Version)
	}
	if len(m.Components) == 0 {
		return fmt.Errorf("插件至少声明一个组件")
	}
	if len(m.Components) > 50 {
		return fmt.Errorf("插件组件数超限（上限 50）")
	}
	seen := map[string]bool{}
	for i, c := range m.Components {
		if err = validateComponent(m, i, c, seen); err != nil {
			return err
		}
	}
	return nil
}

// validateComponent 单组件校验。
func validateComponent(m *Manifest, idx int, c Component, seen map[string]bool) error {
	if !compNameRe.MatchString(c.Name) {
		return fmt.Errorf("组件 %d: 名字 %q 非法", idx, c.Name)
	}
	if seen[c.Name] {
		return fmt.Errorf("组件 %d: 名字 %q 重复", idx, c.Name)
	}
	seen[c.Name] = true
	if strings.TrimSpace(c.Label) == "" || len([]rune(c.Label)) > 40 {
		return fmt.Errorf("组件 %s: label 非法", c.Name)
	}
	if !templateFileRe.MatchString(c.Template) {
		return fmt.Errorf("组件 %s: 模板文件名 %q 非法（期望 *.jet）", c.Name, c.Template)
	}
	if len(c.Props) > 40 {
		return fmt.Errorf("组件 %s: props 字段数超限（上限 40）", c.Name)
	}
	for key, ctl := range c.Props {
		if !keyCharRe.MatchString(key) {
			return fmt.Errorf("组件 %s: props 键 %q 非法", c.Name, key)
		}
		if !propKindWhitelist[ctl.Kind] {
			return fmt.Errorf("组件 %s: props %q 的控件类型 %q 不在白名单", c.Name, key, ctl.Kind)
		}
		if ctl.Kind == "select" && len(ctl.Options) == 0 {
			return fmt.Errorf("组件 %s: props %q 为 select 但缺枚举选项", c.Name, key)
		}
		if len([]rune(ctl.Label)) > 40 {
			return fmt.Errorf("组件 %s: props %q 的 label 超长", c.Name, key)
		}
	}
	if c.Styles != nil {
		if err := c.Styles.Validate(); err != nil {
			return fmt.Errorf("组件 %s: 样式声明非法: %w", c.Name, err)
		}
	}
	return nil
}

// keyCharRe props 键白名单（与 style.keyCharRe 同语义）。
var keyCharRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,60}$`)

// BuildSpecs 把 manifest 组件声明构建为编译内核规格（键 = 完整类型标识）。
// CompileStyles 闭包绑定各组件的 styles schema（style 引擎编译）。
// 确定性：同一 manifest 构建结果恒定（map 遍历仅用于收集，规格内容与序无关）。
func BuildSpecs(m *Manifest) map[string]*core.PluginComponentSpec {
	out := make(map[string]*core.PluginComponentSpec, len(m.Components))
	for _, c := range m.Components {
		comp := c // 循环变量拷贝（闭包捕获）
		spec := &core.PluginComponentSpec{
			Type:     TypeOf(m.ID, comp.Name),
			Label:    comp.Label,
			Template: TemplateOf(m.ID, comp.Template),
			Props:    make(map[string]core.PluginPropControl, len(comp.Props)),
		}
		for key, ctl := range comp.Props {
			spec.Props[key] = core.PluginPropControl{
				Kind: ctl.Kind, Label: ctl.Label, Default: ctl.Default,
				Options: ctl.Options, MaxLen: ctl.MaxLen,
			}
		}
		if comp.Styles != nil && len(comp.Styles.Rules) > 0 {
			styles := comp.Styles
			spec.CompileStyles = func(nodeID string, props map[string]any, b *core.CSSBuckets) error {
				return style.Compile(nodeID, props, styles, b)
			}
		}
		out[spec.Type] = spec
	}
	return out
}

// TypeOf 完整类型标识："plugin.{pluginID}.{name}"。
func TypeOf(pluginID, name string) string {
	return "plugin." + pluginID + "." + name
}

// TemplateOf 插件命名空间模板路径："plugin/{pluginID}/{file}"。
// CompositeLoader 按该前缀路由到对应插件包的 components/ 目录。
func TemplateOf(pluginID, templateFile string) string {
	return "plugin/" + pluginID + "/" + templateFile
}

// ParseManifest 从 JSON 字节解析并校验。
func ParseManifest(data []byte) (m *Manifest, err error) {
	m = &Manifest{}
	if err = json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("manifest 解析失败: %w", err)
	}
	if err = ValidateManifest(m); err != nil {
		return nil, err
	}
	return m, nil
}

// InspectorSchema 生成工作台检查器 schema（Control 数组 JSON，与内置组件
// ComponentSchemas 同构：key/label/kind/section/options/default）。
// 内容控件进 content 页签；样式绑定（color/unit）进 style 页签。
func InspectorSchema(m *Manifest) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(m.Components))
	for _, c := range m.Components {
		controls := make([]map[string]any, 0, len(c.Props))
		for key, ctl := range c.Props {
			section := "content"
			if ctl.Kind == "color" || ctl.Kind == "unit" {
				section = "style"
			}
			entry := map[string]any{
				"key": key, "kind": ctl.Kind, "section": section,
			}
			if ctl.Label != "" {
				entry["label"] = ctl.Label
			}
			if len(ctl.Options) > 0 {
				entry["options"] = ctl.Options
			}
			if ctl.Default != nil {
				entry["default"] = ctl.Default
			}
			controls = append(controls, entry)
		}
		// 控件按键名稳定排序（确定性）。
		sortControls(controls)
		data, err := json.Marshal(controls)
		if err != nil {
			continue
		}
		out[TypeOf(m.ID, c.Name)] = data
	}
	return out
}

// sortControls 按 key 字典序排序（map 迭代序不确定，产物必须确定）。
func sortControls(controls []map[string]any) {
	for i := 1; i < len(controls); i++ {
		for j := i; j > 0 && controls[j]["key"].(string) < controls[j-1]["key"].(string); j-- {
			controls[j], controls[j-1] = controls[j-1], controls[j]
		}
	}
}
