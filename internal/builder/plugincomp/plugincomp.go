package plugincomp

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/source"
	"go_wp/internal/builder/style"
)

var propKindWhitelist = map[string]bool{
	"text": true, "textarea": true, "number": true, "select": true,
	"color": true, "media": true, "unit": true,
}

var idRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,60}$`)

var compNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,60}$`)

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[a-z0-9.]+)?$`)

var templateFileRe = regexp.MustCompile(`^[a-z0-9_-]{1,60}\.jet$`)

type Manifest struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Version    string      `json:"version"`
	Requires   *Requires   `json:"requires,omitempty"`
	Components []Component `json:"components"`

	Migrations string `json:"migrations,omitempty"`

	SchemaVersion int `json:"schemaVersion,omitempty"`

	Presets []Preset `json:"presets,omitempty"`
}

type Preset struct {
	ID string `json:"id"`

	Label string `json:"label"`

	Category string `json:"category,omitempty"`

	Thumbnail string `json:"thumbnail,omitempty"`

	Document json.RawMessage `json:"document"`
}

type Requires struct {
	Core string `json:"core,omitempty"`
}

type Component struct {
	Name string `json:"name"`

	Label string `json:"label"`

	Hint string `json:"hint,omitempty"`

	Template string `json:"template"`

	Props map[string]PropControl `json:"props,omitempty"`

	Styles *style.Schema `json:"styles,omitempty"`

	Collection *CollectionBinding `json:"collection,omitempty"`
}

type CollectionBinding struct {
	Source string `json:"source"`

	Fields []string `json:"fields"`

	Filter map[string]string `json:"filter,omitempty"`
}

type PropControl struct {
	Kind    string   `json:"kind"`
	Label   string   `json:"label,omitempty"`
	Default any      `json:"default,omitempty"`
	Options []string `json:"options,omitempty"`
	MaxLen  int      `json:"maxlen,omitempty"`
}

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

	if m.Migrations != "" && !dirNameRe.MatchString(m.Migrations) {
		return fmt.Errorf("迁移目录名 %q 非法（小写字母数字下划线连字符）", m.Migrations)
	}
	if m.SchemaVersion < 0 {
		return fmt.Errorf("schema 版本不能为负")
	}

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

var dirNameRe = regexp.MustCompile(`^[a-z0-9_-]{1,60}$`)

var presetIDRe = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

var presetCategoryRe = regexp.MustCompile(`^[A-Za-z0-9一-龥_-]{1,40}$`)

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

	return validatePresetDocument(p.Document, idx)
}

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

func defaultKindCompatible(kind string, def any) bool {
	if kind == "number" {
		_, ok := def.(float64)
		return ok
	}
	_, ok := def.(string)
	return ok
}

var collectionSourceRe = regexp.MustCompile(`^(content:[a-z]+|plugin:[a-z][a-z0-9_-]*\.[a-z][a-z0-9_]*)$`)

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

var keyCharRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,60}$`)

func BuildSpecs(m *Manifest) map[string]*core.PluginComponentSpec {
	out := make(map[string]*core.PluginComponentSpec, len(m.Components))
	for _, c := range m.Components {
		comp := c
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
			spec.CompileStyles = func(nodeID string, props map[string]any, b source.StyleSink) error {
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

func TypeOf(pluginID, name string) string {
	return "plugin." + pluginID + "." + name
}

func TemplateOf(pluginID, templateFile string) string {
	return "plugin/" + pluginID + "/" + templateFile
}

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

		sortControls(controls)
		data, err := json.Marshal(controls)
		if err != nil {
			continue
		}
		out[TypeOf(m.ID, c.Name)] = data
	}
	return out
}

func sortControls(controls []map[string]any) {
	for i := 1; i < len(controls); i++ {
		for j := i; j > 0 && controls[j]["key"].(string) < controls[j-1]["key"].(string); j-- {
			controls[j], controls[j-1] = controls[j-1], controls[j]
		}
	}
}

const presetMaxDepth = 8

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
