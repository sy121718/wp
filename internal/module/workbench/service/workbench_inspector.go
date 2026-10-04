package service

// workbench_inspector.go — 检查器面板的数据装配（schema 控件 → 模板可渲染的分组）。
//
// 合并自原 inbound/http 的 inspector_handle.go / inspector_field_handle.go /
// inspector_repeater.go / inspector_entity_ref.go / inspector_navigation.go：
// 这些文件里与 HTTP 无关的部分（分组、字段构造、下拉取数、重复项骨架）整体下沉，
// handler 只留「解析请求 → 调本方法 → 渲染片段」。
//
// 取词：面板 HTML 由 Go 拼串产出，模板只做插槽，所以分组标题、字段标签、占位符、
// 按钮提示都必须在这里取词 —— 全部经 tr（Translate）。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	navigationdto "go_wp/internal/module/navigation/dto"
	productcontract "go_wp/internal/module/product/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

// InspectorSchemaItem 与 core.SchemaJSON 的输出对齐（后端单源，前端不再各自解析）。
type InspectorSchemaItem struct {
	Key     string `json:"key"`
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Section string `json:"section"`
	Default string `json:"default"`
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Step    int    `json:"step"`
	MaxLen  int    `json:"maxLen"`
	Unit    string `json:"unit"`
	Options []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"options"`
	Hidden bool `json:"hidden"`
}

// InspectorOption 下拉/分段选项（模板渲染用）。
type InspectorOption struct {
	Value    string
	Label    string
	Selected bool
}

// InspectorSubInput 多输入控件（spacing/corners/rtext）的单个子输入。
type InspectorSubInput struct {
	// Path 相对 props 的完整路径（如 advanced.margin.desktop.top）。
	Path        string
	Label       string
	Value       string
	Placeholder string
}

// InspectorRangeRow 区间列表控件的一行：上限为空表示「以上」（799+）。
type InspectorRangeRow struct {
	Min string
	Max string
}

// InspectorField 单个字段（模板渲染用）。
type InspectorField struct {
	Key   string
	Label string
	// UI 渲染形态：text/textarea/number/bool/select/color/spacing/corners/rtext/classes/cssdecls/media/mediaList/dimension
	UI          string
	Value       string
	Bool        bool
	Options     []InspectorOption
	Min         int
	Max         int
	Step        int
	Placeholder string
	Inputs      []InspectorSubInput
	// Rows 区间列表控件的行（rangelist）：把 `0-199,799+` 这类值拆成可编辑的行。
	Rows []InspectorRangeRow
	// Slot 非空表示该字段由客户端增强控件渲染（取色器/联动锁/媒体选择等）：
	// 服务端只输出定位占位 div，客户端用既有控件函数填充（避免两套控件实现）。
	Slot string
	// HTML 非空表示该字段的整块结构已由服务端生成（重复项面板等）：模板原样输出，
	// 客户端只绑行为 —— 结构只有一处定义（见 workbench_repeater.go）。
	HTML string
	// NavNewRef 非空表示该 entityref 字段支持「就地新建」（当前只有 navigation）：
	// 模板据此渲染一个折叠的新建表单，客户端提交后把新项写回本字段。
	// 端口未注入 / 无工程上下文时不置位 —— 入口整体不渲染，不留一个点了没反应的表单。
	NavNewRef string
	// KindOptions 新建菜单项时的位置选项（只随 NavNewRef 一起用）。
	KindOptions []InspectorOption
}

// InspectorSection 面板分组（WP 式折叠分组）。
// Key 为分组标识（content/style/layout/…），客户端据此把增强面板插入对应折叠组。
type InspectorSection struct {
	Key    string
	Title  string
	Open   bool
	Used   int
	Fields []InspectorField
}

// DocNode 页面文档节点（仅面板定位所需字段）。
type DocNode struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []DocNode       `json:"children"`
}

// 分组顺序与中文标题 key（与前端旧面板保持一致）。标题按请求语言取词：
// 拼进面板 HTML 的是译文，key 与中文兜底登记在 workbenchenums（workbench.inspector.section.*）。
var inspectorSectionOrder = []struct{ Key, TitleKey string }{
	{"content", workbenchenums.InspectorSectionContent},
	{"style", workbenchenums.InspectorSectionStyle},
	{"layout", workbenchenums.InspectorSectionLayout},
	{"background", workbenchenums.InspectorSectionBackground},
	{"border", workbenchenums.InspectorSectionBorder},
	{"transform", workbenchenums.InspectorSectionTransform},
	{"motion", workbenchenums.InspectorSectionMotion},
	{"hover", workbenchenums.InspectorSectionHover},
	{"responsive", workbenchenums.InspectorSectionResponsive},
	{"advanced", workbenchenums.InspectorSectionAdvanced},
}

// InspectorSections 把节点文档装配成模板可渲染的分组数据。
//
// 装配链：组件 schema（构建内核，按节点类型取）→ 单字段构造 → 分组桶 →
// 重复项面板骨架。任一步取数失败都返回 error，由调用方决定出口（HTTP 侧是 500）。
func (s *Service) InspectorSections(ctx context.Context, node *DocNode, tab, projectID string, tr Translate) ([]InspectorSection, error) {
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		return nil, err
	}
	var items []InspectorSchemaItem
	if node != nil {
		if raw, ok := schemas[node.Type]; ok {
			if err = json.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
		}
	}
	var props map[string]any
	if node != nil && len(node.Props) > 0 {
		_ = json.Unmarshal(node.Props, &props)
	}
	sections := s.buildInspectorSections(ctx, items, props, tab, projectID, tr)
	// 重复项面板（折叠项 / 页签）：结构由服务端生成，客户端只绑行为。
	return AppendRepeaterPanel(sections, node, props, tab, tr), nil
}

// FindDocNode 在页面文档里按 ID 查找节点（深度优先）。
func FindDocNode(doc json.RawMessage, nodeID string) (*DocNode, error) {
	if len(doc) == 0 || nodeID == "" {
		return nil, nil
	}
	var page struct {
		Root []DocNode `json:"root"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		return nil, err
	}
	var walk func(nodes []DocNode) *DocNode
	walk = func(nodes []DocNode) *DocNode {
		for i := range nodes {
			if nodes[i].ID == nodeID {
				return &nodes[i]
			}
			if found := walk(nodes[i].Children); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(page.Root), nil
}

// buildInspectorSections 把 schema 控件按分组转成模板数据（跳过 hidden 与不满足条件的字段）。
//
// tr 是「key → 当前语言文案」的取词函数：分组标题与各字段的 Label / Placeholder
// 都在本函数的下游产出，模板只负责把它们铺出来。
func (s *Service) buildInspectorSections(ctx context.Context, items []InspectorSchemaItem, props map[string]any, tab, projectID string, tr Translate) []InspectorSection {
	buckets := map[string][]InspectorField{}
	used := map[string]int{}
	// corners 合并：radiusTL/TR/BR/BL 与 advanced.radius.topLeft/… 各只渲染一次。
	cornersDone := false
	for _, ctl := range items {
		if ctl.Hidden || !inspectorFieldVisible(ctl.Key, props) {
			continue
		}
		sec := ctl.Section
		if sec == "" {
			sec = "content"
		}
		if isCornerKey(ctl.Key) {
			if cornersDone {
				continue
			}
			cornersDone = true
			buckets[sec] = append(buckets[sec], cornersField(ctl, props, tr))
			continue
		}
		if isCornerTailKey(ctl.Key) {
			continue
		}
		f := s.inspectorFieldOf(ctx, ctl, props, projectID, tr)
		if f.Key == "" {
			continue
		}
		if f.Value != "" || f.Bool || hasSubValue(f.Inputs) {
			used[sec]++
		}
		buckets[sec] = append(buckets[sec], f)
	}
	out := make([]InspectorSection, 0, len(inspectorSectionOrder))
	seen := map[string]bool{}
	for _, sec := range inspectorSectionOrder {
		if !sectionInTab(sec.Key, tab) {
			continue
		}
		fields := buckets[sec.Key]
		if len(fields) == 0 {
			continue
		}
		seen[sec.Key] = true
		out = append(out, InspectorSection{
			Key: sec.Key, Title: tr(sec.TitleKey), Fields: fields, Used: used[sec.Key],
			// 展开规则：有值的分组展开、内容分组默认展开、首个分组兜底展开
			// （空面板全收起时用户看不到任何控件，必须至少露一组）。
			Open: used[sec.Key] > 0 || sec.Key == "content" || len(out) == 0,
		})
	}
	// 未登记分组兜底（字典序，保证确定性）。
	rest := make([]string, 0, 4)
	for k := range buckets {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		// 兜底分组同样受页签过滤约束（否则被过滤掉的已登记分组会被当成「未登记」加回来）。
		if !sectionInTab(k, tab) {
			continue
		}
		out = append(out, InspectorSection{Key: k, Title: k, Fields: buckets[k], Used: used[k], Open: used[k] > 0 || len(out) == 0})
	}
	return out
}

// sectionInTab 分组归属页签：content = 内容页签；其余（基础/布局/背景/边框/变换/动效/
// 响应式/高级）= 样式页签。tab 为空表示不过滤（渲染全部）。
func sectionInTab(section, tab string) bool {
	switch tab {
	case "content":
		return section == "content"
	case "style":
		return section != "content"
	}
	return true
}

// inspectorFieldOf 单个 schema 控件 → 模板字段。
//
// tr 是「key → 当前语言文案」的取词函数：本函数与它的下游（圆角 / 间距 / 响应式控件、
// 导航下拉）产出的 Label 与 Placeholder 会直接进面板 HTML，模板层不参与这些句子，
// 所以取词必须在这里完成。
func (s *Service) inspectorFieldOf(ctx context.Context, ctl InspectorSchemaItem, props map[string]any, projectID string, tr Translate) InspectorField {
	f := InspectorField{Key: ctl.Key, Label: ctl.Label, Min: ctl.Min, Max: ctl.Max, Step: ctl.Step}
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
		f.Options = s.entityRefInspectorOptions(ctx, projectID, refKind, value, tr)
		// 导航菜单项：检查器里可以就地新建（写回走 navigation 契约的 Create）。
		// 三个条件缺一不可 —— refKind 是 navigation、端口已注入、有工程上下文；
		// 少任何一个都会渲染出一个「提交必然失败」的入口，比不显示更糟。
		if refKind == "navigation" && s.navigations != nil && projectID != "" {
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
			for _, o := range s.entityRefInspectorOptions(ctx, projectID, kindOpt.Value, "", tr) {
				if o.Value == "" {
					continue // 「（不限）」在单选的语义里有用，在多选里是噪声
				}
				f.Options = append(f.Options, InspectorOption{
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
			f.Options = append(f.Options, InspectorOption{Value: o.Value, Label: o.Label, Selected: o.Value == value})
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
func cornersField(ctl InspectorSchemaItem, props map[string]any, tr Translate) InspectorField {
	f := InspectorField{Key: ctl.Key, Label: ctl.Label, UI: "corners", Slot: "corners"}
	if f.Label == "" {
		f.Label = tr(workbenchenums.InspectorCorners)
	}
	if strings.HasSuffix(ctl.Key, "radiusTL") {
		base := strings.TrimSuffix(ctl.Key, "TL")
		for _, pair := range []struct{ suffix, labelKey string }{
			{"TL", workbenchenums.InspectorCornerTopLeft}, {"TR", workbenchenums.InspectorCornerTopRight},
			{"BR", workbenchenums.InspectorCornerBottomRight}, {"BL", workbenchenums.InspectorCornerBottomLeft},
		} {
			f.Inputs = append(f.Inputs, InspectorSubInput{
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
		f.Inputs = append(f.Inputs, InspectorSubInput{
			Path: base + pair.suffix, Label: tr(pair.labelKey), Value: propString(props, base+pair.suffix),
		})
	}
	return f
}

// spacingInputs 三端 × 四向边距子输入。
func spacingInputs(props map[string]any, key string, tr Translate) []InspectorSubInput {
	out := make([]InspectorSubInput, 0, 12)
	for _, bp := range []struct{ key, labelKey string }{
		{"desktop", workbenchenums.InspectorBpDesktop}, {"tablet", workbenchenums.InspectorBpTablet},
		{"mobile", workbenchenums.InspectorBpMobile},
	} {
		for _, dir := range []struct{ key, labelKey string }{
			{"top", workbenchenums.InspectorDirTop}, {"right", workbenchenums.InspectorDirRight},
			{"bottom", workbenchenums.InspectorDirBottom}, {"left", workbenchenums.InspectorDirLeft},
		} {
			path := fmt.Sprintf("%s.%s.%s", key, bp.key, dir.key)
			out = append(out, InspectorSubInput{
				Path: path, Label: tr(bp.labelKey) + tr(dir.labelKey), Value: propString(props, path), Placeholder: "0px",
			})
		}
	}
	return out
}

// responsiveTextInputs 三端文本子输入（字号/行高等）。
func responsiveTextInputs(props map[string]any, key string, tr Translate) []InspectorSubInput {
	out := make([]InspectorSubInput, 0, 3)
	for _, bp := range []struct{ key, labelKey string }{
		{"desktop", workbenchenums.InspectorBpDesktop}, {"tablet", workbenchenums.InspectorBpTablet},
		{"mobile", workbenchenums.InspectorBpMobile},
	} {
		path := key + "." + bp.key
		out = append(out, InspectorSubInput{Path: path, Label: tr(bp.labelKey), Value: propString(props, path)})
	}
	return out
}

// hasSubValue 多输入控件是否有已填值（分组「已用项数」统计）。
func hasSubValue(inputs []InspectorSubInput) bool {
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

// parseRangeRows 把 `0-199,200-399,799+` 解析成行。
//
// 认不出的片段原样放进 Min 而不是丢弃：面板不是校验入口，把作者写的原文显示出来
// 让他自己改，比在这一层静默吞掉更好（构建期仍按既有规则拒绝并给出明确报错）。
func parseRangeRows(raw string) []InspectorRangeRow {
	var rows []InspectorRangeRow
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if min, ok := strings.CutSuffix(part, "+"); ok {
			rows = append(rows, InspectorRangeRow{Min: min})
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			rows = append(rows, InspectorRangeRow{Min: lo, Max: hi})
			continue
		}
		rows = append(rows, InspectorRangeRow{Min: part})
	}
	return rows
}

// entityRefInspectorOptions 把集合源可选筛选项转成检查器下拉（EDT-005）。
//
// tr 是「key → 当前语言文案」的取词函数：空选项与导航位置的标签都会直接进面板 HTML，
// 模板层不参与，所以取词在这里完成。
func (s *Service) entityRefInspectorOptions(ctx context.Context, projectID, refKind, selected string, tr Translate) []InspectorOption {
	out := []InspectorOption{{Value: "", Label: tr(workbenchenums.InspectorNavAny), Selected: selected == ""}}
	// 导航菜单项不是集合筛选项（不在商品数据源里），单独走导航模块的列表端口。
	if refKind == "navigation" {
		return s.navigationInspectorOptions(ctx, projectID, selected, tr)
	}
	if s == nil || s.products == nil || projectID == "" {
		return out
	}
	provider, ok := s.products.(core.CollectionFilterOptionsProvider)
	if !ok {
		return out
	}
	// 检查器持有的就是商品数据源：源标识用 contract 常量，不写第二份字面量。
	opts, err := provider.CollectionFilterOptions(ctx, productcontract.CollectionSourceProduct, projectID)
	if err != nil {
		return out
	}
	var choices []core.CollectionFilterChoice
	switch refKind {
	case "category":
		choices = opts.Categories
	case "brand":
		choices = opts.Brands
	case "tag":
		choices = opts.Tags
	default:
		return out
	}
	for _, ch := range choices {
		out = append(out, InspectorOption{
			Value: ch.ID, Label: ch.Name, Selected: ch.ID == selected,
		})
	}
	return out
}

// navigationInspectorOptions 列出本工程全部菜单项（按位置分组排序）。
//
// 标签带位置前缀：同一个工程里「产品」这类标题在页眉与移动端各有一条，
// 只显示标题会让检查器里出现两个一模一样的选项，选错就静默绑到另一端的菜单上。
func (s *Service) navigationInspectorOptions(ctx context.Context, projectID, selected string, tr Translate) []InspectorOption {
	out := []InspectorOption{{Value: "", Label: tr(workbenchenums.InspectorNavAny), Selected: selected == ""}}
	if s == nil || s.navigations == nil || projectID == "" {
		return out
	}
	rows, err := s.navigations.List(ctx, &navigationdto.ListReq{ProjectID: projectID})
	if err != nil {
		return out
	}
	for _, row := range rows {
		if row == nil || row.ID == "" {
			continue
		}
		// 只列根项：按项引用时渲染的是「该项及其子树」，挂到子项上也合法，
		// 但下拉里给全部项会让列表过长且层级难辨；子项可另用「按位置」模式取整棵树。
		if row.ParentID != nil && *row.ParentID != "" {
			continue
		}
		out = append(out, InspectorOption{
			Value:    row.ID,
			Label:    NavigationKindLabel(row.Kind, tr) + " · " + row.Title,
			Selected: row.ID == selected,
		})
	}
	return out
}

// navigationKindOptions 新建菜单项时的位置选项（与导航管理页的四个位置一致）。
func navigationKindOptions(tr Translate) []InspectorOption {
	return []InspectorOption{
		{Value: "header", Label: NavigationKindLabel("header", tr), Selected: true},
		{Value: "header_mobile", Label: NavigationKindLabel("header_mobile", tr)},
		{Value: "footer", Label: NavigationKindLabel("footer", tr)},
		{Value: "footer_mobile", Label: NavigationKindLabel("footer_mobile", tr)},
	}
}

// NavigationKindLabel 位置名（与 admin 导航页的选项文案同义，按请求语言取词）。
func NavigationKindLabel(kind string, tr Translate) string {
	switch kind {
	case "header":
		return tr(workbenchenums.InspectorNavKindHeader)
	case "header_mobile":
		return tr(workbenchenums.InspectorNavKindHeaderMobile)
	case "footer":
		return tr(workbenchenums.InspectorNavKindFooter)
	case "footer_mobile":
		return tr(workbenchenums.InspectorNavKindFooterMobile)
	}
	return kind
}
