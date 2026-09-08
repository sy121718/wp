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
	// Migrations L1 数据层迁移目录名（如 "migrations"，含版本化 SQL；docs/06 §8）。
	// 空 = 纯展示插件（无自有表）。
	Migrations string `json:"migrations,omitempty"`
	// SchemaVersion L1 数据层 schema 版本（迁移执行器记账；0 = 无自有表）。
	SchemaVersion int `json:"schemaVersion,omitempty"`
	// Presets 区块预设（预组合 AST 片段，一键插入组件库；docs/06 §5.2）。
	Presets []Preset `json:"presets,omitempty"`
}

// Preset 区块预设声明（对标 GrapesJS Block Manager，docs/06 §5.2）。
type Preset struct {
	// ID 预设标识（组件库去重键，白名单字符）。
	ID string `json:"id"`
	// Label 预设显示名。
	Label string `json:"label"`
	// Category 分组名（如 "营销区块"）。
	Category string `json:"category,omitempty"`
	// Thumbnail 缩略图路径（包内相对路径，可选）。
	Thumbnail string `json:"thumbnail,omitempty"`
	// Document 预组合 AST 片段（Node 数组，插入时 ID 重写）。
	Document json.RawMessage `json:"document"`
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
	// Collection 集合绑定声明（docs/06 §9）：组件渲染列表数据。source 为
	// 集合源（"content:{entityType}" 等），fields 为渲染字段白名单，filter
	// 为可选固定过滤。声明后构建期经 CollectionResolver 展开为 .V.items。
	Collection *CollectionBinding `json:"collection,omitempty"`
}

// CollectionBinding 组件集合绑定声明（构建期展开列表数据）。
type CollectionBinding struct {
	// Source 集合源标识（"content:product" / 未来 "plugin:{id}.{table}"）。
	Source string `json:"source"`
	// Fields 渲染字段白名单（不变量 4：模板只能渲染声明字段）。
	Fields []string `json:"fields"`
	// Filter 可选固定过滤（键值等值，键在集合源白名单内）。
	Filter map[string]string `json:"filter,omitempty"`
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
	// L1 迁移目录名与 schema 版本（docs/06 §8）。
	if m.Migrations != "" && !dirNameRe.MatchString(m.Migrations) {
		return fmt.Errorf("迁移目录名 %q 非法（小写字母数字下划线连字符）", m.Migrations)
	}
	if m.SchemaVersion < 0 {
		return fmt.Errorf("schema 版本不能为负")
	}
	// 区块预设（docs/06 §5.2）。
	if len(m.Presets) > 100 {
		return fmt.Errorf("预设数超限（上限 100）")
	}
	presetSeen := map[string]bool{}
	for i, p := range m.Presets {
		if err = validatePreset(i, p, presetSeen); err != nil {
			return err
		}
	}
	return nil
}

// dirNameRe 目录名白名单（迁移目录等）。
var dirNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,60}$`)

// presetIDRe 预设 ID 白名单。
var presetIDRe = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

// presetCategoryRe 预设分组名白名单（字面中文字符范围，Go regexp 不支持 \uXXXX 转义）。
var presetCategoryRe = regexp.MustCompile(`^[A-Za-z0-9一-龥_-]{1,40}$`)

// validatePreset 单个区块预设校验：ID/label/分组白名单 + document 解析为
// Node 数组（结构合法、深度上限、组件类型可识别）。
func validatePreset(idx int, p Preset, seen map[string]bool) error {
	if !presetIDRe.MatchString(p.ID) {
		return fmt.Errorf("预设 %d: id %q 非法", idx, p.ID)
	}
	if seen[p.ID] {
		return fmt.Errorf("预设 %d: id %q 重复", idx, p.ID)
	}
	seen[p.ID] = true
	if strings.TrimSpace(p.Label) == "" || len([]rune(p.Label)) > 40 {
		return fmt.Errorf("预设 %d: label 非法", idx)
	}
	if p.Category != "" && !presetCategoryRe.MatchString(p.Category) {
		return fmt.Errorf("预设 %d: 分组名 %q 非法", idx, p.Category)
	}
	if len(p.Document) == 0 {
		return fmt.Errorf("预设 %d: 缺少 document", idx)
	}
	// document 解析为 Node 数组并做结构校验（深度/类型/ID 合法性）。
	return validatePresetDocument(p.Document, idx)
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
		if ctl.Default != nil && !defaultKindCompatible(ctl.Kind, ctl.Default) {
			return fmt.Errorf("组件 %s: props %q 的 default 与 kind %q 不相容", c.Name, key, ctl.Kind)
		}
	}
	if c.Styles != nil {
		if err := c.Styles.Validate(); err != nil {
			return fmt.Errorf("组件 %s: 样式声明非法: %w", c.Name, err)
		}
	}
	if c.Collection != nil {
		if err := validateCollectionBinding(c.Name, c.Collection); err != nil {
			return err
		}
	}
	return nil
}

// defaultKindCompatible default 与 kind 相容校验：number 需数值默认值，
// 其余控件（text/textarea/select/color/media/unit）需字符串默认值。
// JSON 反序列化到 any 后 number 为 float64，string 为 string。
func defaultKindCompatible(kind string, def any) bool {
	if kind == "number" {
		_, ok := def.(float64)
		return ok
	}
	_, ok := def.(string)
	return ok
}

// collectionSourceRe 集合源标识白名单（"content:product" / "plugin:{id}.{table}"）。
var collectionSourceRe = regexp.MustCompile(`^(content:[a-z]+|plugin:[a-z][a-z0-9_-]*\.[a-z][a-z0-9_]*)$`)

// validateCollectionBinding 集合绑定声明校验（source/fields/filter 白名单）。
func validateCollectionBinding(compName string, cb *CollectionBinding) error {
	if !collectionSourceRe.MatchString(cb.Source) {
		return fmt.Errorf("组件 %s: 集合源 %q 非法", compName, cb.Source)
	}
	if len(cb.Fields) == 0 || len(cb.Fields) > 40 {
		return fmt.Errorf("组件 %s: 集合字段数非法（1~40）", compName)
	}
	for _, f := range cb.Fields {
		if !keyCharRe.MatchString(f) {
			return fmt.Errorf("组件 %s: 集合字段 %q 非法", compName, f)
		}
	}
	if len(cb.Filter) > 20 {
		return fmt.Errorf("组件 %s: 过滤维度超限（上限 20）", compName)
	}
	for k := range cb.Filter {
		if !keyCharRe.MatchString(k) {
			return fmt.Errorf("组件 %s: 过滤键 %q 非法", compName, k)
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
		if comp.Collection != nil {
			spec.Collection = &core.CollectionBinding{
				Source: comp.Collection.Source,
				Fields: comp.Collection.Fields,
				Filter: comp.Collection.Filter,
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

// presetMaxDepth 预设 document 深度上限（与 builder.MaxNodeDepth 对齐，
// 避免预设内联展开后叠加页面挂载链超限；插件预设深度更保守）。
const presetMaxDepth = 8

// validatePresetDocument 解析预设 document（Node 数组）并做结构校验：
// 数组非空、节点类型可识别（内置或 plugin.*）、ID 白名单唯一、深度上限。
// 不执行 props 值校验（预设只含初始 props，运行时用户编辑再校验）。
func validatePresetDocument(raw json.RawMessage, idx int) error {
	var nodes []*core.Node
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return fmt.Errorf("预设 %d: document 解析失败: %w", idx, err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("预设 %d: document 不能为空", idx)
	}
	if len(nodes) > 10 {
		return fmt.Errorf("预设 %d: 顶级节点数超限（上限 10）", idx)
	}
	ids := map[string]bool{}
	for _, n := range nodes {
		if err := validatePresetNode(n, ids, 1, idx); err != nil {
			return err
		}
	}
	return nil
}

// validatePresetNode 递归校验预设节点（ID/类型/深度/无子节点规则）。
func validatePresetNode(n *core.Node, ids map[string]bool, depth, idx int) error {
	if n == nil {
		return fmt.Errorf("预设 %d: 节点为空", idx)
	}
	if depth > presetMaxDepth {
		return fmt.Errorf("预设 %d: 深度 %d 超过上限 %d", idx, depth, presetMaxDepth)
	}
	if err := core.ValidateNodeID(n.ID, n.Name, ids); err != nil {
		return fmt.Errorf("预设 %d: %w", idx, err)
	}
	// 类型名前缀白名单：内置 core.* 或插件 plugin.*（安装时不做 registry 查证——
	// 内置组件是否注册是编译期固定事实，且 plugincomp 不 blank import 组件；
	// 真正编译时 core.Lookup/PluginResolver 才验证存在性）。
	if !strings.HasPrefix(n.Type, "plugin.") && !strings.HasPrefix(n.Type, "core.") {
		return fmt.Errorf("预设 %d: 未知组件类型 %q", idx, n.Type)
	}
	for _, c := range n.Children {
		if err := validatePresetNode(c, ids, depth+1, idx); err != nil {
			return err
		}
	}
	return nil
}
