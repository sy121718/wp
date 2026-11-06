package editor

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"html"
	"sort"
	"strconv"
	"strings"

	"go_wp/pkg/builder/core"
)

// DocNode 是编辑器文档树的最小节点形状（只读文档事实，不含业务字段）。
type DocNode struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props"`
	Children []DocNode       `json:"children"`
}

// FindDocNode 在文档 root 中按 ID 深度优先查找节点。
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

// OutlineNode 是大纲树输入：文档节点的展示事实。
type OutlineNode struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Name     string        `json:"name"`
	Hidden   bool          `json:"hidden"`
	Locked   bool          `json:"locked"`
	Children []OutlineNode `json:"children"`
}

// OutlineLabels 是大纲树用到的宿主文案，由应用按请求语言提供。
type OutlineLabels struct {
	Toggle      string
	HiddenHint  string
	HiddenBadge string
	LockedHint  string
	LockedBadge string
	OpUp        string
	OpDown      string
	OpDup       string
	OpDel       string
}

// RenderOutlineHTML 渲染大纲树。结构、属性与转义属通用编辑器机制；
// 文案由调用方显式传入，不读模块词库。
func RenderOutlineHTML(nodes []OutlineNode, selectedID, filter string, labels OutlineLabels) string {
	var sb strings.Builder
	writeOutlineNodes(labels, &sb, nodes, selectedID, filter)
	return sb.String()
}

func writeOutlineNodes(labels OutlineLabels, sb *strings.Builder, nodes []OutlineNode, selectedID, filter string) {
	sb.WriteString("<ul>")
	for i := range nodes {
		n := &nodes[i]
		if filter != "" && !outlineSubtreeHit(n, filter) {
			continue
		}
		label := outlineLabel(n)
		cls := "wb-node"
		if n.ID == selectedID {
			cls += " is-selected"
		}
		sb.WriteString("<li>")
		sb.WriteString(`<div class="` + cls + `" role="treeitem" tabindex="0" data-id="` + html.EscapeString(n.ID) +
			`" data-type="` + html.EscapeString(n.Type) + `" draggable="true">`)
		caret := ""
		if len(n.Children) > 0 {
			caret = "▾"
		}
		sb.WriteString(`<button class="wb-caret" title="` + html.EscapeString(labels.Toggle) + `">` + caret + `</button>`)
		sb.WriteString(`<span class="wb-node-name" data-named="` + boolFlag(n.Name != "") + `">` + html.EscapeString(label) + `</span>`)
		if n.Hidden {
			sb.WriteString(`<span class="wb-node-flag" title="` + html.EscapeString(labels.HiddenHint) + `">` +
				html.EscapeString(labels.HiddenBadge) + `</span>`)
		}
		if n.Locked {
			sb.WriteString(`<span class="wb-node-flag" title="` + html.EscapeString(labels.LockedHint) + `">` +
				html.EscapeString(labels.LockedBadge) + `</span>`)
		}
		sb.WriteString(`<span class="wb-node-actions">`)
		for _, op := range []struct{ text, title, op string }{
			{"↑", labels.OpUp, "up"}, {"↓", labels.OpDown, "down"},
			{"⧉", labels.OpDup, "dup"}, {"✕", labels.OpDel, "del"},
		} {
			sb.WriteString(`<button type="button" class="wb-node-action" data-wb-op="` + op.op +
				`" title="` + html.EscapeString(op.title) + `">` + op.text + `</button>`)
		}
		sb.WriteString(`</span></div>`)
		if len(n.Children) > 0 {
			writeOutlineNodes(labels, sb, n.Children, selectedID, filter)
		}
		sb.WriteString("</li>")
	}
	sb.WriteString("</ul>")
}

func outlineLabel(n *OutlineNode) string {
	if strings.TrimSpace(n.Name) != "" {
		return n.Name
	}
	if t := strings.TrimPrefix(n.Type, "core."); t != "" {
		return t
	}
	return n.ID
}

func outlineSubtreeHit(n *OutlineNode, filter string) bool {
	if strings.Contains(strings.ToLower(outlineLabel(n)), filter) {
		return true
	}
	for i := range n.Children {
		if outlineSubtreeHit(&n.Children[i], filter) {
			return true
		}
	}
	return false
}

func boolFlag(v bool) string {
	if v {
		return "1"
	}
	return ""
}

// InspectorSchemaItem 是组件 Inspector schema 的最小字段声明。
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

// InspectorOption 是一个可选项事实（值 + 展示标签 + 选中态）。
type InspectorOption struct {
	Value    string
	Label    string
	Selected bool
}

// InspectorSubInput 是多输入控件的子输入。
type InspectorSubInput struct {
	Path        string
	Label       string
	Value       string
	Placeholder string
}

// InspectorRangeRow 是区间列表的一行。
type InspectorRangeRow struct {
	Min string
	Max string
}

// InspectorField 是渲染后的一个字段。
type InspectorField struct {
	Key   string
	Label string

	UI          string
	Value       string
	Bool        bool
	Options     []InspectorOption
	Min         int
	Max         int
	Step        int
	Placeholder string
	Inputs      []InspectorSubInput

	Rows []InspectorRangeRow

	Slot string

	HTML string

	NavNewRef string

	KindOptions []InspectorOption

	// Extra 表示该字段还带有宿主专属控件（如导航新建），模板只输出占位，
	// 由应用宿主脚本填充，不在工具模板里写业务操作。
	Extra bool
}

// InspectorSection 是一个折叠分组。
type InspectorSection struct {
	Key    string
	Title  string
	Open   bool
	Used   int
	Fields []InspectorField
}

// SectionKeys 是检查器分组的固定顺序。
var SectionKeys = []string{"content", "style", "layout", "background", "border", "transform", "motion", "hover", "responsive", "advanced"}

// Labels 是检查器渲染所需的宿主文案。文案按请求语言由应用解析，
// 工具不读模块词库、不拼成品句。
type Labels struct {
	Sections map[string]string

	Corners     string
	TopLeft     string
	TopRight    string
	BottomRight string
	BottomLeft  string

	BpDesktop string
	BpTablet  string
	BpMobile  string

	DirTop    string
	DirRight  string
	DirBottom string
	DirLeft   string

	PhClasses   string
	PhCSSDecls  string
	PhDimension string
	NavAny      string
	MoveUp      string
	MoveDown    string
	Remove      string
	Matched     string
	Mismatch    string
}

func (l Labels) sectionTitle(key string) string {
	if t, ok := l.Sections[key]; ok {
		return t
	}
	return key
}

// Ports 是检查器需要的应用窄端口。工具不内置实体查询或业务写入。
type Ports struct {
	// EntityRefOptions 返回某个引用种类（category/brand/tag 等）的候选实体。
	// refKind 来自组件 schema 的 options[0].value。
	EntityRefOptions func(ctx context.Context, projectID, refKind, selected string) []InspectorOption
	// NavigationKinds 返回导航位置选项（仅当处于导航新建上下文时使用）。
	NavigationKinds func() []InspectorOption
}

// InspectorInput 是一次检查器构建请求。
type InspectorInput struct {
	Node      *DocNode
	Schemas   map[string]json.RawMessage
	Tab       string
	ProjectID string
	Labels    Labels
	Ports     Ports
}

// BuildInspectorSections 把组件 schema + 节点 props 转成分组字段。
func BuildInspectorSections(ctx context.Context, in InspectorInput) ([]InspectorSection, error) {
	var items []InspectorSchemaItem
	if in.Node != nil {
		if raw, ok := in.Schemas[in.Node.Type]; ok {
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, err
			}
		}
	}
	var props map[string]any
	if in.Node != nil && len(in.Node.Props) > 0 {
		_ = json.Unmarshal(in.Node.Props, &props)
	}
	sections := buildSections(ctx, in, items, props)
	return AppendRepeaterPanel(sections, in.Node, props, in.Tab, in.Labels), nil
}

func buildSections(ctx context.Context, in InspectorInput, items []InspectorSchemaItem, props map[string]any) []InspectorSection {
	buckets := map[string][]InspectorField{}
	used := map[string]int{}
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
			buckets[sec] = append(buckets[sec], cornersField(ctl, props, in.Labels))
			continue
		}
		if isCornerTailKey(ctl.Key) {
			continue
		}
		f := inspectorFieldOf(ctx, ctl, props, in)
		if f.Key == "" {
			continue
		}
		if f.Value != "" || f.Bool || hasSubValue(f.Inputs) {
			used[sec]++
		}
		buckets[sec] = append(buckets[sec], f)
	}
	out := make([]InspectorSection, 0, len(SectionKeys))
	seen := map[string]bool{}
	for _, key := range SectionKeys {
		if !sectionInTab(key, in.Tab) {
			continue
		}
		fields := buckets[key]
		if len(fields) == 0 {
			continue
		}
		seen[key] = true
		out = append(out, InspectorSection{
			Key: key, Title: in.Labels.sectionTitle(key), Fields: fields, Used: used[key],
			Open: used[key] > 0 || key == "content" || len(out) == 0,
		})
	}
	rest := make([]string, 0, 4)
	for k := range buckets {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if !sectionInTab(k, in.Tab) {
			continue
		}
		out = append(out, InspectorSection{Key: k, Title: in.Labels.sectionTitle(k), Fields: buckets[k], Used: used[k], Open: used[k] > 0 || len(out) == 0})
	}
	return out
}

func sectionInTab(section, tab string) bool {
	switch tab {
	case "content":
		return section == "content"
	case "style":
		return section != "content"
	}
	return true
}

func inspectorFieldOf(ctx context.Context, ctl InspectorSchemaItem, props map[string]any, in InspectorInput) InspectorField {
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
		f.Options = entityRefOptions(ctx, in, refKind, value)
		if refKind == "navigation" && in.ProjectID != "" && in.Ports.NavigationKinds != nil {
			f.NavNewRef = refKind
			f.KindOptions = in.Ports.NavigationKinds()
			f.Extra = true
		}
	case "multientityref":
		f.UI = "multientityref"
		f.Value = value
		selected := map[string]bool{}
		for _, id := range strings.Split(value, ",") {
			if id = strings.TrimSpace(id); id != "" {
				selected[id] = true
			}
		}
		for _, kindOpt := range ctl.Options {
			for _, o := range entityRefOptions(ctx, in, kindOpt.Value, "") {
				if o.Value == "" {
					continue
				}
				f.Options = append(f.Options, InspectorOption{Value: o.Value, Label: o.Label, Selected: selected[o.Value]})
			}
		}
	case "rangelist":
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
		f.UI = "textarea"
		f.Value = value
	case "richtext":
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
		f.Inputs = spacingInputs(props, ctl.Key, in.Labels)
	case "boxspacing":
		f.UI = "boxspacing"
		f.Slot = "boxspacing"
	case "rtext":
		f.UI = "rtext"
		f.Slot = "rtext"
		f.Inputs = responsiveTextInputs(props, ctl.Key, in.Labels)
	case "classes":
		f.UI = "classes"
		f.Value = value
		f.Placeholder = in.Labels.PhClasses
	case "cssdecls":
		f.UI = "cssdecls"
		f.Value = value
		f.Placeholder = in.Labels.PhCSSDecls
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
		f.Placeholder = in.Labels.PhDimension
	case "collectionfield", "bindingfield":
		f.UI = ctl.Kind
		f.Slot = ctl.Kind
		f.Value = value
	default:
		f.UI = "text"
		f.Value = value
	}
	return f
}

func entityRefOptions(ctx context.Context, in InspectorInput, refKind, selected string) []InspectorOption {
	out := []InspectorOption{{Value: "", Label: in.Labels.NavAny, Selected: selected == ""}}
	if in.Ports.EntityRefOptions == nil || refKind == "navigation" {
		return out
	}
	if in.ProjectID == "" {
		return out
	}
	out = append(out, in.Ports.EntityRefOptions(ctx, in.ProjectID, refKind, selected)...)
	return out
}

func cornersField(ctl InspectorSchemaItem, props map[string]any, labels Labels) InspectorField {
	f := InspectorField{Key: ctl.Key, Label: ctl.Label, UI: "corners", Slot: "corners"}
	if f.Label == "" {
		f.Label = labels.Corners
	}
	if strings.HasSuffix(ctl.Key, "radiusTL") {
		base := strings.TrimSuffix(ctl.Key, "TL")
		for _, pair := range []struct{ suffix, label string }{
			{"TL", labels.TopLeft}, {"TR", labels.TopRight},
			{"BR", labels.BottomRight}, {"BL", labels.BottomLeft},
		} {
			f.Inputs = append(f.Inputs, InspectorSubInput{Path: base + pair.suffix, Label: pair.label, Value: propString(props, base+pair.suffix)})
		}
		return f
	}
	base := strings.TrimSuffix(ctl.Key, "topLeft")
	for _, pair := range []struct{ suffix, label string }{
		{"topLeft", labels.TopLeft}, {"topRight", labels.TopRight},
		{"bottomRight", labels.BottomRight}, {"bottomLeft", labels.BottomLeft},
	} {
		f.Inputs = append(f.Inputs, InspectorSubInput{Path: base + pair.suffix, Label: pair.label, Value: propString(props, base+pair.suffix)})
	}
	return f
}

func spacingInputs(props map[string]any, key string, labels Labels) []InspectorSubInput {
	out := make([]InspectorSubInput, 0, 12)
	for _, bp := range []struct{ key, label string }{
		{"desktop", labels.BpDesktop}, {"tablet", labels.BpTablet}, {"mobile", labels.BpMobile},
	} {
		for _, dir := range []struct{ key, label string }{
			{"top", labels.DirTop}, {"right", labels.DirRight}, {"bottom", labels.DirBottom}, {"left", labels.DirLeft},
		} {
			path := fmt.Sprintf("%s.%s.%s", key, bp.key, dir.key)
			out = append(out, InspectorSubInput{Path: path, Label: bp.label + dir.label, Value: propString(props, path), Placeholder: "0px"})
		}
	}
	return out
}

func responsiveTextInputs(props map[string]any, key string, labels Labels) []InspectorSubInput {
	out := make([]InspectorSubInput, 0, 3)
	for _, bp := range []struct{ key, label string }{
		{"desktop", labels.BpDesktop}, {"tablet", labels.BpTablet}, {"mobile", labels.BpMobile},
	} {
		path := key + "." + bp.key
		out = append(out, InspectorSubInput{Path: path, Label: bp.label, Value: propString(props, path)})
	}
	return out
}

func hasSubValue(inputs []InspectorSubInput) bool {
	for _, in := range inputs {
		if in.Value != "" {
			return true
		}
	}
	return false
}

func isCornerKey(key string) bool {
	return strings.HasSuffix(key, "radiusTL") || strings.HasSuffix(key, "radius.topLeft")
}

func isCornerTailKey(key string) bool {
	return strings.HasSuffix(key, "radiusTR") || strings.HasSuffix(key, "radiusBR") || strings.HasSuffix(key, "radiusBL") ||
		strings.HasSuffix(key, "radius.topRight") || strings.HasSuffix(key, "radius.bottomRight") || strings.HasSuffix(key, "radius.bottomLeft")
}

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

type repeaterRow struct {
	Value  string
	Extras map[string]bool
}

func repeaterRowsOf(props map[string]any, spec *core.AlignedRepeaterSpec) []repeaterRow {
	raw, _ := props[spec.AlignKey].([]any)
	rows := make([]repeaterRow, 0, len(raw))
	for _, it := range raw {
		entry, _ := it.(map[string]any)
		row := repeaterRow{Extras: map[string]bool{}}
		if v, ok := entry[spec.Field].(string); ok {
			row.Value = v
		}
		for _, ex := range spec.Extra {
			if b, ok := entry[ex.Key].(bool); ok {
				row.Extras[ex.Key] = b
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func renderRepeaterHTML(spec *core.AlignedRepeaterSpec, rows []repeaterRow, panelCount int, labels Labels) string {
	var b strings.Builder
	b.WriteString(`<div class="wb-repeater" data-wb-rep="` + html.EscapeString(spec.Type) +
		`" data-wb-rep-field="` + html.EscapeString(spec.Field) + `">`)
	for i, row := range rows {
		b.WriteString(`<div class="wb-repeater-row" data-wb-rep-index="` + fmt.Sprint(i) + `">`)
		b.WriteString(`<div class="wb-repeater-mid">`)
		placeholder := spec.Noun + fmt.Sprint(i+1) + " " + spec.Label
		b.WriteString(`<input type="text" data-wb-rep-input="` + fmt.Sprint(i) +
			`" value="` + html.EscapeString(row.Value) + `" placeholder="` + html.EscapeString(placeholder) + `">`)
		for _, ex := range spec.Extra {
			checked := ""
			if row.Extras[ex.Key] {
				checked = " checked"
			}
			b.WriteString(`<label class="wb-check-field"><input type="checkbox" data-wb-rep-extra="` +
				html.EscapeString(ex.Key) + `" data-wb-rep-index="` + fmt.Sprint(i) + `"` + checked + `> ` + html.EscapeString(ex.Label) + `</label>`)
		}
		b.WriteString(`</div><div class="wb-repeater-acts">`)
		if i > 0 {
			b.WriteString(repeaterButton("↑", labels.MoveUp, "move", i, "-1", "wb-btn wb-btn-sm wb-btn-ghost"))
		}
		if i < len(rows)-1 {
			b.WriteString(repeaterButton("↓", labels.MoveDown, "move", i, "1", "wb-btn wb-btn-sm wb-btn-ghost"))
		}
		b.WriteString(repeaterButton("✕",
			strings.ReplaceAll(labels.Remove, "{noun}", spec.Noun),
			"remove", i, "", "wb-icon-btn"))
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`<button type="button" class="wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add" data-wb-rep-op="add">` +
		html.EscapeString(spec.AddText) + `</button>`)
	b.WriteString(`<p class="wb-empty"`)
	if len(rows) != panelCount {
		b.WriteString(` style="color: var(--c-danger, #d93425)"`)
	}
	b.WriteString(`>`)
	if len(rows) == panelCount {
		b.WriteString(html.EscapeString(fillRepeaterText(labels.Matched, map[string]string{
			"{noun}": spec.Noun, "{count}": fmt.Sprint(len(rows)),
		})))
	} else {
		b.WriteString(html.EscapeString(fillRepeaterText(labels.Mismatch, map[string]string{
			"{noun}": spec.Noun, "{rows}": fmt.Sprint(len(rows)), "{panels}": fmt.Sprint(panelCount),
		})))
	}
	b.WriteString(`</p></div>`)
	return b.String()
}

func fillRepeaterText(text string, values map[string]string) string {
	for k, v := range values {
		text = strings.ReplaceAll(text, k, v)
	}
	return text
}

func repeaterButton(text, title, op string, index int, to, cls string) string {
	s := `<button type="button" class="` + cls + `" data-wb-rep-op="` + op +
		`" data-wb-rep-index="` + fmt.Sprint(index) + `"`
	if to != "" {
		s += ` data-wb-rep-to="` + to + `"`
	}
	return s + ` title="` + html.EscapeString(title) + `">` + html.EscapeString(text) + `</button>`
}

// AppendRepeaterPanel 把结构型组件的重复项面板并入内容分组。
func AppendRepeaterPanel(sections []InspectorSection, node *DocNode, props map[string]any, tab string, labels Labels) []InspectorSection {
	if tab == "style" || tab == "motion" || node == nil {
		return sections
	}
	spec := core.AlignedRepeaterFor(node.Type)
	if spec == nil {
		return sections
	}
	field := InspectorField{
		Key:  "__repeater",
		HTML: renderRepeaterHTML(spec, repeaterRowsOf(props, spec), len(node.Children), labels),
	}
	for i := range sections {
		if sections[i].Key == "content" {
			sections[i].Fields = append(sections[i].Fields, field)
			return sections
		}
	}
	return append([]InspectorSection{{Key: "content", Title: labels.sectionTitle("content"), Open: true, Fields: []InspectorField{field}}}, sections...)
}
