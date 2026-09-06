// pluginview.go — 插件组件的编译视图装配（docs/06-plugin-system.md §5/§7）。
//
// plugin.{pluginID}.{name} 类型节点经 RenderContext.Plugin 解析规格：
//   - props 按 spec 白名单解码校验（未知键拒绝、文本长度限幅、枚举比对）；
//   - 样式经 spec.CompileStyles 闭包（style 引擎）编译进 CSSBuckets；
//   - 视图走 nodeView 通用字段：Template 指向插件命名空间模板，V = props map
//     （模板经 {{.V.field}} 访问，Jet 默认转义）。
package builder

import (
	"encoding/json"
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// pluginTypePrefix 插件组件类型前缀（与内置 core.* 命名空间隔离）。
const pluginTypePrefix = "plugin."

// pluginViewOf 插件节点 → 视图（含 props 白名单校验与样式编译）。
func pluginViewOf(node *core.Node, topLevel bool, ctx *core.RenderContext) (*nodeView, error) {
	if ctx.Plugin == nil {
		return nil, fmt.Errorf("节点 %s: 编译上下文缺少插件解析器（页面使用了插件组件 %q）", node.ID, node.Type)
	}
	spec, ok := ctx.Plugin.LookupPluginComponent(node.Type)
	if !ok {
		return nil, fmt.Errorf("节点 %s: 插件组件 %q 不可用（未安装或未启用）", node.ID, node.Type)
	}
	props, err := decodePluginProps(node, spec)
	if err != nil {
		return nil, fmt.Errorf("节点 %s: %w", node.ID, err)
	}

	var classes []string
	classes = append(classes, core.NodeClass(node.ID))

	// 样式声明编译（style 引擎闭包，值白名单在闭包内二次校验）。
	if spec.CompileStyles != nil {
		if serr := spec.CompileStyles(node.ID, props, ctx.CSS); serr != nil {
			return nil, fmt.Errorf("节点 %s: 样式编译失败: %w", node.ID, serr)
		}
	}
	if node.Hidden {
		// 编辑期隐藏仅编辑器语义，编译产物不含（与内置组件一致：Hidden 不进产物）。
		_ = node.Hidden
	}

	// 视图数据 V：默认 = props map（{{.V.field}}）；集合组件追加 .V.items 列表。
	viewData := props
	if spec.Collection != nil {
		if ctx.Collection == nil {
			return nil, fmt.Errorf("节点 %s: 编译上下文缺少集合解析器（组件 %q 声明了集合绑定）", node.ID, node.Type)
		}
		items, cerr := ctx.Collection.ResolveCollection(spec.Collection.Source, spec.Collection.Filter)
		if cerr != nil {
			return nil, fmt.Errorf("节点 %s: 集合 %q 解析失败: %w", node.ID, spec.Collection.Source, cerr)
		}
		// 字段白名单裁剪（不变量 4：模板只能渲染声明字段）。
		viewData = map[string]any{
			"items": cropFields(items, spec.Collection.Fields),
			"props": props,
		}
	}

	return &nodeView{
		Type:     spec.Type,
		Template: spec.Template,
		NodeID:   node.ID,
		Classes:  strings.Join(classes, " "),
		TopLevel: topLevel,
		Props:    props,
		V:        viewData, // 模板经 {{.V.field}}（非集合）或 {{range .V.items}}（集合）
	}, nil
}

// cropFields 按字段白名单裁剪列表项（不变量 4：拒绝声明外字段）。
func cropFields(items []map[string]any, fields []string) []map[string]any {
	allow := make(map[string]bool, len(fields))
	for _, f := range fields {
		allow[f] = true
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := make(map[string]any, len(fields))
		for _, f := range fields {
			if v, ok := item[f]; ok {
				row[f] = v
			}
		}
		out = append(out, row)
	}
	return out
}

// decodePluginProps 按 spec 白名单解码节点 props：未知键拒绝（防夹带），
// 值类型与长度校验，select 枚举比对；缺省键回填 Default。
func decodePluginProps(node *core.Node, spec *core.PluginComponentSpec) (map[string]any, error) {
	raw := map[string]any{}
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &raw); err != nil {
			return nil, fmt.Errorf("props 反序列化失败: %w", err)
		}
	}
	out := make(map[string]any, len(spec.Props))
	for key, ctl := range spec.Props {
		v, exists := raw[key]
		if !exists || v == nil {
			if ctl.Default != nil {
				out[key] = ctl.Default
			}
			continue
		}
		val, err := validatePluginPropValue(key, v, ctl)
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	// 未知键拒绝：props 只允许 schema 声明过的字段（防夹带未校验数据）。
	for key := range raw {
		if _, declared := spec.Props[key]; !declared {
			return nil, fmt.Errorf("props 含未声明的键 %q（白名单拒绝）", key)
		}
	}
	return out, nil
}

// validatePluginPropValue 单个插件 props 值校验（按控件类型）。
func validatePluginPropValue(key string, v any, ctl core.PluginPropControl) (any, error) {
	maxLen := ctl.MaxLen
	if maxLen <= 0 {
		maxLen = 200
	}
	switch ctl.Kind {
	case "text", "textarea", "unit", "color", "media":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("props %q 期望字符串值", key)
		}
		if len(s) > maxLen {
			return nil, fmt.Errorf("props %q 超长（上限 %d）", key, maxLen)
		}
		if ctl.Kind == "color" && s != "" && !isPluginColorValue(s) {
			return nil, fmt.Errorf("props %q 非法颜色值", key)
		}
		if ctl.Kind == "unit" && s != "" && !isPluginUnitValue(s) {
			return nil, fmt.Errorf("props %q 非法尺寸值", key)
		}
		return s, nil
	case "number":
		switch n := v.(type) {
		case float64:
			return n, nil
		case int:
			return float64(n), nil
		default:
			return nil, fmt.Errorf("props %q 期望数值", key)
		}
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("props %q 期望布尔值", key)
		}
		return b, nil
	case "select":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("props %q 期望字符串枚举值", key)
		}
		for _, opt := range ctl.Options {
			if s == opt {
				return s, nil
			}
		}
		return nil, fmt.Errorf("props %q 的值 %q 不在枚举内", key, s)
	default:
		return nil, fmt.Errorf("props %q 的控件类型 %q 不在白名单", key, ctl.Kind)
	}
}

// isPluginColorValue 颜色值白名单：#hex / rgb()/rgba()/hsl() 函数值 / var(--token)。
func isPluginColorValue(s string) bool {
	if strings.HasPrefix(s, "#") {
		return len(s) >= 4 && len(s) <= 9
	}
	for _, fn := range []string{"rgb(", "rgba(", "hsl(", "hsla("} {
		if strings.HasPrefix(s, fn) {
			return true
		}
	}
	return strings.HasPrefix(s, "var(--") && strings.HasSuffix(s, ")")
}

// isPluginUnitValue 尺寸值白名单：数字+单位（px/%/em/rem/vw/vh，允许负数）或 auto。
func isPluginUnitValue(s string) bool {
	if s == "auto" {
		return true
	}
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if digits == 0 || i == len(s) {
		return false
	}
	switch s[i:] {
	case "px", "%", "em", "rem", "vw", "vh":
		return true
	}
	return false
}
