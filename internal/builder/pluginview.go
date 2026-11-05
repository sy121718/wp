package builder

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/pkg/builder/core"
)

const pluginTypePrefix = "plugin."

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

	if spec.CompileStyles != nil {
		if serr := spec.CompileStyles(node.ID, props, ctx.CSS); serr != nil {
			return nil, fmt.Errorf("节点 %s: 样式编译失败: %w", node.ID, serr)
		}
	}

	viewData := props
	if spec.Collection != nil {
		if ctx.Collection == nil {
			return nil, fmt.Errorf("节点 %s: 编译上下文缺少集合解析器（组件 %q 声明了集合绑定）", node.ID, node.Type)
		}
		items, cerr := ctx.Collection.ResolveCollection(ctx.Context, spec.Collection.Source, spec.Collection.Filter)
		if cerr != nil {
			return nil, fmt.Errorf("节点 %s: 集合 %q 解析失败: %w", node.ID, spec.Collection.Source, cerr)
		}

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
		V:        viewData,
	}, nil
}

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

	for key := range raw {
		if _, declared := spec.Props[key]; !declared {
			return nil, fmt.Errorf("props 含未声明的键 %q（白名单拒绝）", key)
		}
	}
	return out, nil
}

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
