package dashboardhttp

// inspector_handle.go — 检查器面板的服务端渲染（HTMX 化打样，docs/09 §3）。
//
// 背景：workbench.js 用数百行 DOM 代码把组件 schema 变成表单（含分组、条件字段、
// 复杂控件）。本文件把「schema → 表单 HTML」搬到服务端（Jet 片段），客户端只保留
// 「值变更 → 回写 AST → 刷新画布」这一层。
//
// 端点：POST /workbench/inspector（HTMX 片段），参数 document（草稿 JSON）+ nodeId。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder"
	dashboardenums "go_wp/internal/module/dashboard/enums"

	"github.com/gin-gonic/gin"
)

// inspectorSchemaItem 与 core.SchemaJSON 的输出对齐（后端单源，前端不再各自解析）。
type inspectorSchemaItem struct {
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

// inspectorOption 下拉/分段选项（模板渲染用）。
type inspectorOption struct {
	Value    string
	Label    string
	Selected bool
}

// inspectorSubInput 多输入控件（spacing/corners/rtext）的单个子输入。
type inspectorSubInput struct {
	// Path 相对 props 的完整路径（如 advanced.margin.desktop.top）。
	Path        string
	Label       string
	Value       string
	Placeholder string
}

// inspectorField 单个字段（模板渲染用）。
type inspectorField struct {
	Key   string
	Label string
	// UI 渲染形态：text/textarea/number/bool/select/color/spacing/corners/rtext/classes/cssdecls/media/mediaList/dimension
	UI          string
	Value       string
	Bool        bool
	Options     []inspectorOption
	Min         int
	Max         int
	Step        int
	Placeholder string
	Inputs      []inspectorSubInput
	// Slot 非空表示该字段由客户端增强控件渲染（取色器/联动锁/媒体选择等）：
	// 服务端只输出定位占位 div，客户端用既有控件函数填充（避免两套控件实现）。
	Slot string
}

// inspectorSection 面板分组（WP 式折叠分组）。
// Key 为分组标识（content/style/layout/…），客户端据此把增强面板插入对应折叠组。
type inspectorSection struct {
	Key    string
	Title  string
	Open   bool
	Used   int
	Fields []inspectorField
}

// 分组顺序与中文标题（与前端旧面板保持一致）。
var inspectorSectionOrder = []struct{ Key, Title string }{
	{"content", "内容"},
	{"style", "基础"},
	{"layout", "布局"},
	{"background", "背景"},
	{"border", "边框"},
	{"transform", "变换"},
	{"motion", "动效"},
	{"hover", "悬停"},
	{"responsive", "响应式"},
	{"advanced", "高级"},
}

// docNode 页面文档节点（仅面板定位所需字段）。
type docNode struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []docNode       `json:"children"`
}

// InspectorPanel 渲染选中节点的检查器面板片段。
func (h *Handle) InspectorPanel(c *gin.Context) {
	nodeID := strings.TrimSpace(c.PostForm("nodeId"))
	node, err := findDocNode(json.RawMessage(c.PostForm("document")), nodeID)
	if err != nil || node == nil {
		c.HTML(http.StatusOK, "fragments/inspector_panel", gin.H{"NodeID": ""})
		return
	}
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
		return
	}
	var items []inspectorSchemaItem
	if raw, ok := schemas[node.Type]; ok {
		if err = json.Unmarshal(raw, &items); err != nil {
			c.String(http.StatusInternalServerError, dashboardenums.MsgInternalError)
			return
		}
	}
	var props map[string]any
	if len(node.Props) > 0 {
		_ = json.Unmarshal(node.Props, &props)
	}
	// tab：content / style（空 = 渲染全部，向后兼容旧调用）。
	tab := strings.TrimSpace(c.PostForm("tab"))
	c.HTML(http.StatusOK, "fragments/inspector_panel", gin.H{
		"NodeID": nodeID, "NodeType": node.Type,
		"Sections": buildInspectorSections(items, props, tab),
	})
}

// findDocNode 在页面文档里按 ID 查找节点（深度优先）。
func findDocNode(doc json.RawMessage, nodeID string) (*docNode, error) {
	if len(doc) == 0 || nodeID == "" {
		return nil, nil
	}
	var page struct {
		Root []docNode `json:"root"`
	}
	if err := json.Unmarshal(doc, &page); err != nil {
		return nil, err
	}
	var walk func(nodes []docNode) *docNode
	walk = func(nodes []docNode) *docNode {
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
func buildInspectorSections(items []inspectorSchemaItem, props map[string]any, tab string) []inspectorSection {
	buckets := map[string][]inspectorField{}
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
			buckets[sec] = append(buckets[sec], cornersField(ctl, props))
			continue
		}
		if isCornerTailKey(ctl.Key) {
			continue
		}
		f := inspectorFieldOf(ctl, props)
		if f.Key == "" {
			continue
		}
		if f.Value != "" || f.Bool || hasSubValue(f.Inputs) {
			used[sec]++
		}
		buckets[sec] = append(buckets[sec], f)
	}
	out := make([]inspectorSection, 0, len(inspectorSectionOrder))
	seen := map[string]bool{}
	for _, s := range inspectorSectionOrder {
		if !sectionInTab(s.Key, tab) {
			continue
		}
		fields := buckets[s.Key]
		if len(fields) == 0 {
			continue
		}
		seen[s.Key] = true
		out = append(out, inspectorSection{
			Key: s.Key, Title: s.Title, Fields: fields, Used: used[s.Key],
			// 展开规则：有值的分组展开、内容分组默认展开、首个分组兜底展开
			// （空面板全收起时用户看不到任何控件，必须至少露一组）。
			Open: used[s.Key] > 0 || s.Key == "content" || len(out) == 0,
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
		out = append(out, inspectorSection{Key: k, Title: k, Fields: buckets[k], Used: used[k], Open: used[k] > 0 || len(out) == 0})
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
func inspectorFieldOf(ctl inspectorSchemaItem, props map[string]any) inspectorField {
	f := inspectorField{Key: ctl.Key, Label: ctl.Label, Min: ctl.Min, Max: ctl.Max, Step: ctl.Step}
	if f.Label == "" {
		f.Label = ctl.Key
	}
	value := propString(props, ctl.Key)
	switch ctl.Kind {
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
		f.Inputs = spacingInputs(props, ctl.Key)
	case "boxspacing":
		// container 的 box.padding/margin：三端 CSS 简写，客户端按「一行四向 + 联动」编辑。
		f.UI = "boxspacing"
		f.Slot = "boxspacing"
	case "rtext":
		f.UI = "rtext"
		f.Slot = "rtext"
		f.Inputs = responsiveTextInputs(props, ctl.Key)
	case "classes":
		f.UI = "classes"
		f.Value = value
		f.Placeholder = "逗号或空格分隔，禁 wp- 前缀"
	case "cssdecls":
		f.UI = "cssdecls"
		f.Value = value
		f.Placeholder = "只写样式/布局/动画属性，分号分隔，如 font-size:16px; padding:12px"
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
		f.Placeholder = "如 16px / 1.5rem"
	default:
		f.UI = "text"
		f.Value = value
	}
	return f
}

// cornersField 把四角圆角（组件级 radiusTL 或通用层 radius.topLeft）合并为一个字段。
func cornersField(ctl inspectorSchemaItem, props map[string]any) inspectorField {
	f := inspectorField{Key: ctl.Key, Label: ctl.Label, UI: "corners", Slot: "corners"}
	if f.Label == "" {
		f.Label = "圆角"
	}
	if strings.HasSuffix(ctl.Key, "radiusTL") {
		base := strings.TrimSuffix(ctl.Key, "TL")
		for _, pair := range []struct{ suffix, label string }{
			{"TL", "左上"}, {"TR", "右上"}, {"BR", "右下"}, {"BL", "左下"},
		} {
			f.Inputs = append(f.Inputs, inspectorSubInput{
				Path: base + pair.suffix, Label: pair.label, Value: propString(props, base+pair.suffix),
			})
		}
		return f
	}
	base := strings.TrimSuffix(ctl.Key, "topLeft")
	for _, pair := range []struct{ suffix, label string }{
		{"topLeft", "左上"}, {"topRight", "右上"}, {"bottomRight", "右下"}, {"bottomLeft", "左下"},
	} {
		f.Inputs = append(f.Inputs, inspectorSubInput{
			Path: base + pair.suffix, Label: pair.label, Value: propString(props, base+pair.suffix),
		})
	}
	return f
}

// spacingInputs 三端 × 四向边距子输入。
func spacingInputs(props map[string]any, key string) []inspectorSubInput {
	out := make([]inspectorSubInput, 0, 12)
	for _, bp := range []struct{ key, label string }{
		{"desktop", "桌面"}, {"tablet", "平板"}, {"mobile", "手机"},
	} {
		for _, dir := range []struct{ key, label string }{
			{"top", "上"}, {"right", "右"}, {"bottom", "下"}, {"left", "左"},
		} {
			path := fmt.Sprintf("%s.%s.%s", key, bp.key, dir.key)
			out = append(out, inspectorSubInput{
				Path: path, Label: bp.label + dir.label, Value: propString(props, path), Placeholder: "0px",
			})
		}
	}
	return out
}

// responsiveTextInputs 三端文本子输入（字号/行高等）。
func responsiveTextInputs(props map[string]any, key string) []inspectorSubInput {
	out := make([]inspectorSubInput, 0, 3)
	for _, bp := range []struct{ key, label string }{
		{"desktop", "桌面"}, {"tablet", "平板"}, {"mobile", "手机"},
	} {
		path := key + "." + bp.key
		out = append(out, inspectorSubInput{Path: path, Label: bp.label, Value: propString(props, path)})
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
