package workbenchhttp

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	workbenchenums "go_wp/internal/module/workbench/enums"
)

// inspector_field_handle.go - 检查器单字段构造（角/间距/响应式控件、可见性与取值读取）。

// inspectorFieldOf 单个 schema 控件 → 模板字段。
//
// tr 是「key → 当前语言文案」的取词函数（workbenchTrFunc）：本函数与它的下游
// （圆角 / 间距 / 响应式控件、导航下拉）产出的 Label 与 Placeholder 会直接进面板 HTML，
// 模板层不参与这些句子，所以取词必须在这里完成。
func inspectorFieldOf(ctx context.Context, h *Handle, ctl inspectorSchemaItem, props map[string]any, projectID string, tr func(key string) string) inspectorField {
	f := inspectorField{Key: ctl.Key, Label: ctl.Label, Min: ctl.Min, Max: ctl.Max, Step: ctl.Step}
	if f.Label == "" {
		f.Label = ctl.Key
	}
	value := propString(props, ctl.Key)
	switch ctl.Kind {
	case "entityref":
		f.UI = "select"
		f.Value = value
		refKind := ""
		if len(ctl.Options) > 0 {
			refKind = ctl.Options[0].Value
		}
		f.Options = entityRefInspectorOptions(ctx, h, projectID, refKind, value, tr)
		// 导航菜单项：检查器里可以就地新建（写回走 navigation 契约的 Create）。
		// 三个条件缺一不可 —— refKind 是 navigation、端口已注入、有工程上下文；
		// 少任何一个都会渲染出一个「提交必然失败」的入口，比不显示更糟。
		if refKind == "navigation" && h != nil && h.navigations != nil && projectID != "" {
			f.NavNewRef = refKind
			f.KindOptions = navigationKindOptions(tr)
		}
	case "multientityref":
		// 多选实体（标签 id 列表等，审计 EDT-007）：值仍是逗号分隔串（读写兼容），
		// 但勾选状态由当前值直接渲染 —— 打开面板就知道已经选了哪几个，
		// 不用去数一串 id。选项按 ct tag 声明的实体类型逐个取。
		f.UI = "multientityref"
		f.Value = value
		selected := map[string]bool{}
		for _, id := range strings.Split(value, ",") {
			if id = strings.TrimSpace(id); id != "" {
				selected[id] = true
			}
		}
		for _, kindOpt := range ctl.Options {
			for _, o := range entityRefInspectorOptions(ctx, h, projectID, kindOpt.Value, "", tr) {
				if o.Value == "" {
					continue // 「（不限）」在单选的语义里有用，在多选里是噪声
				}
				f.Options = append(f.Options, inspectorOption{
					Value: o.Value, Label: o.Label, Selected: selected[o.Value],
				})
			}
		}
	case "rangelist":
		// 区间列表（预设价格档位等，审计 EDT-007）：既有格式 `0-199,799+` 保持不变，
		// 只是把「手写整串」换成逐行编辑。
		f.UI = "rangelist"
		f.Value = value
		f.Rows = parseRangeRows(value)
	case "bool":
		f.UI = "bool"
		f.Bool = value == "true"
	case "select":
		f.UI = "select"
		f.Value = value
		for _, o := range ctl.Options {
			f.Options = append(f.Options, inspectorOption{Value: o.Value, Label: o.Label, Selected: o.Value == value})
		}
	case "text", "textarea":
		// text / textarea：多行纯文本输入。
		f.UI = "textarea"
		f.Value = value
	case "richtext":
		// 富文本内容字段（core.text 正文 / card 正文 / quote 引用 / infobox 描述 / faq 答案）：
		// 编辑器为 Trix，服务端只输出 slot 占位，客户端 fillInspectorSlots 用 richTextField 填充
		// （core.text 的 mode=plaintext 时前端回退多行输入）。
		f.UI = "richtext"
		f.Slot = "richtext"
		f.Value = value
	case "int", "slider", "number":
		f.UI = "number"
		f.Value = value
		if f.Step == 0 {
			f.Step = 1
		}
	case "color":
		f.UI = "color"
		f.Slot = "color"
		f.Value = value
	case "spacing", "margin":
		f.UI = "spacing"
		f.Slot = "spacing"
		f.Inputs = spacingInputs(props, ctl.Key, tr)
	case "boxspacing":
		// container 的 box.padding/margin：三端 CSS 简写，客户端按「一行四向 + 联动」编辑。
		f.UI = "boxspacing"
		f.Slot = "boxspacing"
	case "rtext":
		f.UI = "rtext"
		f.Slot = "rtext"
		f.Inputs = responsiveTextInputs(props, ctl.Key, tr)
	case "classes":
		f.UI = "classes"
		f.Value = value
		f.Placeholder = tr(workbenchenums.InspectorPhClasses)
	case "cssdecls":
		f.UI = "cssdecls"
		f.Value = value
		f.Placeholder = tr(workbenchenums.InspectorPhCSSDecls)
	case "media":
		f.UI = "media"
		f.Slot = "media"
		f.Value = value
	case "mediaList":
		f.UI = "mediaList"
		f.Slot = "mediaList"
		f.Value = propListString(props, ctl.Key)
	case "dimension":
		f.UI = "dimension"
		f.Slot = "dimension"
		f.Value = value
		f.Placeholder = tr(workbenchenums.InspectorPhDimension)
	case "collectionfield", "bindingfield":
		// 集合字段映射（core.cardstack 的 5 个字段）与内容字段绑定（item.<字段>）：
		// 选项来自后端字段白名单（按当前节点的「内容集合」过滤），手填字段名会绕过白名单，
		// 所以服务端只输出 slot，客户端用 collectionFieldControl / bindingFieldControl
		// 渲染成下拉 —— 不走 default 的文本框（退化成手填等于把白名单丢了）。
		f.UI = ctl.Kind
		f.Slot = ctl.Kind
		f.Value = value
	default:
		f.UI = "text"
		f.Value = value
	}
	return f
}

// cornersField 把四角圆角（组件级 radiusTL 或通用层 radius.topLeft）合并为一个字段。
func cornersField(ctl inspectorSchemaItem, props map[string]any, tr func(key string) string) inspectorField {
	f := inspectorField{Key: ctl.Key, Label: ctl.Label, UI: "corners", Slot: "corners"}
	if f.Label == "" {
		f.Label = tr(workbenchenums.InspectorCorners)
	}
	if strings.HasSuffix(ctl.Key, "radiusTL") {
		base := strings.TrimSuffix(ctl.Key, "TL")
		for _, pair := range []struct{ suffix, labelKey string }{
			{"TL", workbenchenums.InspectorCornerTopLeft}, {"TR", workbenchenums.InspectorCornerTopRight},
			{"BR", workbenchenums.InspectorCornerBottomRight}, {"BL", workbenchenums.InspectorCornerBottomLeft},
		} {
			f.Inputs = append(f.Inputs, inspectorSubInput{
				Path: base + pair.suffix, Label: tr(pair.labelKey), Value: propString(props, base+pair.suffix),
			})
		}
		return f
	}
	base := strings.TrimSuffix(ctl.Key, "topLeft")
	for _, pair := range []struct{ suffix, labelKey string }{
		{"topLeft", workbenchenums.InspectorCornerTopLeft}, {"topRight", workbenchenums.InspectorCornerTopRight},
		{"bottomRight", workbenchenums.InspectorCornerBottomRight}, {"bottomLeft", workbenchenums.InspectorCornerBottomLeft},
	} {
		f.Inputs = append(f.Inputs, inspectorSubInput{
			Path: base + pair.suffix, Label: tr(pair.labelKey), Value: propString(props, base+pair.suffix),
		})
	}
	return f
}

// spacingInputs 三端 × 四向边距子输入。
func spacingInputs(props map[string]any, key string, tr func(key string) string) []inspectorSubInput {
	out := make([]inspectorSubInput, 0, 12)
	for _, bp := range []struct{ key, labelKey string }{
		{"desktop", workbenchenums.InspectorBpDesktop}, {"tablet", workbenchenums.InspectorBpTablet},
		{"mobile", workbenchenums.InspectorBpMobile},
	} {
		for _, dir := range []struct{ key, labelKey string }{
			{"top", workbenchenums.InspectorDirTop}, {"right", workbenchenums.InspectorDirRight},
			{"bottom", workbenchenums.InspectorDirBottom}, {"left", workbenchenums.InspectorDirLeft},
		} {
			path := fmt.Sprintf("%s.%s.%s", key, bp.key, dir.key)
			out = append(out, inspectorSubInput{
				Path: path, Label: tr(bp.labelKey) + tr(dir.labelKey), Value: propString(props, path), Placeholder: "0px",
			})
		}
	}
	return out
}

// responsiveTextInputs 三端文本子输入（字号/行高等）。
func responsiveTextInputs(props map[string]any, key string, tr func(key string) string) []inspectorSubInput {
	out := make([]inspectorSubInput, 0, 3)
	for _, bp := range []struct{ key, labelKey string }{
		{"desktop", workbenchenums.InspectorBpDesktop}, {"tablet", workbenchenums.InspectorBpTablet},
		{"mobile", workbenchenums.InspectorBpMobile},
	} {
		path := key + "." + bp.key
		out = append(out, inspectorSubInput{Path: path, Label: tr(bp.labelKey), Value: propString(props, path)})
	}
	return out
}

// hasSubValue 多输入控件是否有已填值（分组「已用项数」统计）。
func hasSubValue(inputs []inspectorSubInput) bool {
	for _, in := range inputs {
		if in.Value != "" {
			return true
		}
	}
	return false
}

// isCornerKey 是否四角圆角的首个字段。
func isCornerKey(key string) bool {
	return strings.HasSuffix(key, "radiusTL") || strings.HasSuffix(key, "radius.topLeft")
}

// isCornerTailKey 四角圆角其余字段（已合并，跳过）。
func isCornerTailKey(key string) bool {
	return strings.HasSuffix(key, "radiusTR") || strings.HasSuffix(key, "radiusBR") || strings.HasSuffix(key, "radiusBL") ||
		strings.HasSuffix(key, "radius.topRight") || strings.HasSuffix(key, "radius.bottomRight") || strings.HasSuffix(key, "radius.bottomLeft")
}

// inspectorFieldVisible 条件字段显隐（与前端旧面板同一套规则）。
func inspectorFieldVisible(key string, props map[string]any) bool {
	switch {
	case key == "advanced.widthValue":
		return propString(props, "advanced.widthMode") == "fixed"
	case key == "visual.bgPositionXY":
		return propString(props, "visual.bgPosition") == "custom"
	case key == "visual.bgSizeValue":
		return propString(props, "visual.bgSize") == "custom"
	case strings.HasPrefix(key, "position.top"), strings.HasPrefix(key, "position.right"),
		strings.HasPrefix(key, "position.bottom"), strings.HasPrefix(key, "position.left"):
		pt := propString(props, "position.type")
		return pt != "" && pt != "static"
	case strings.HasPrefix(key, "position.drawer"):
		return propString(props, "position.type") == "drawer"
	}
	return true
}

// propString 按点路径取 props 值并转字符串（数字去尾零）。
func propString(props map[string]any, path string) string {
	var cur any = props
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[seg]
		if !ok {
			return ""
		}
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

// propListString 取字符串数组值（媒体列表）并转为换行文本。
func propListString(props map[string]any, path string) string {
	var cur any = props
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[seg]
		if !ok {
			return ""
		}
	}
	list, ok := cur.([]any)
	if !ok {
		return ""
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		switch v := item.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			for _, key := range []string{"url", "src", "value"} {
				if s, ok := v[key].(string); ok && s != "" {
					out = append(out, s)
					break
				}
			}
		}
	}
	return strings.Join(out, "\n")
}
