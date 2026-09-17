package workbenchhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"go_wp/internal/builder"
	workbenchenums "go_wp/internal/module/workbench/enums"

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

// inspectorRangeRow 区间列表控件的一行：上限为空表示「以上」（799+）。
type inspectorRangeRow struct {
	Min string
	Max string
}

// parseRangeRows 把 `0-199,200-399,799+` 解析成行。
//
// 认不出的片段原样放进 Min 而不是丢弃：面板不是校验入口，把作者写的原文显示出来
// 让他自己改，比在这一层静默吞掉更好（构建期仍按既有规则拒绝并给出明确报错）。
func parseRangeRows(raw string) []inspectorRangeRow {
	var rows []inspectorRangeRow
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if min, ok := strings.CutSuffix(part, "+"); ok {
			rows = append(rows, inspectorRangeRow{Min: min})
			continue
		}
		if lo, hi, ok := strings.Cut(part, "-"); ok {
			rows = append(rows, inspectorRangeRow{Min: lo, Max: hi})
			continue
		}
		rows = append(rows, inspectorRangeRow{Min: part})
	}
	return rows
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
	// Rows 区间列表控件的行（rangelist）：把 `0-199,799+` 这类值拆成可编辑的行。
	Rows []inspectorRangeRow
	// Slot 非空表示该字段由客户端增强控件渲染（取色器/联动锁/媒体选择等）：
	// 服务端只输出定位占位 div，客户端用既有控件函数填充（避免两套控件实现）。
	Slot string
	// HTML 非空表示该字段的整块结构已由服务端生成（重复项面板等）：模板原样输出，
	// 客户端只绑行为 —— 结构只有一处定义（inspector_repeater.go）。
	HTML string
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
		c.String(http.StatusInternalServerError, workbenchenums.MsgInternalError)
		return
	}
	var items []inspectorSchemaItem
	if raw, ok := schemas[node.Type]; ok {
		if err = json.Unmarshal(raw, &items); err != nil {
			c.String(http.StatusInternalServerError, workbenchenums.MsgInternalError)
			return
		}
	}
	var props map[string]any
	if len(node.Props) > 0 {
		_ = json.Unmarshal(node.Props, &props)
	}
	// tab：content / style（空 = 渲染全部，向后兼容旧调用）。
	tab := strings.TrimSpace(c.PostForm("tab"))
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	sections := buildInspectorSections(c.Request.Context(), h, items, props, tab, projectID)
	// 重复项面板（折叠项 / 页签）：结构由服务端生成，客户端只绑行为。
	sections = appendRepeaterPanel(sections, node, props, tab)
	c.HTML(http.StatusOK, "fragments/inspector_panel", gin.H{
		"NodeID": nodeID, "NodeType": node.Type,
		"Sections": sections,
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
func buildInspectorSections(ctx context.Context, h *Handle, items []inspectorSchemaItem, props map[string]any, tab, projectID string) []inspectorSection {
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
		f := inspectorFieldOf(ctx, h, ctl, props, projectID)
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
