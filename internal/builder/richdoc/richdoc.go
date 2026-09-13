// Package richdoc 实现「富文本 ⇄ 可视化组件树」的等价转换（docs/06-B 决策 5）。
//
// 决策 5 的原话是「双视图单真源」：底层唯一真源是组件树（AST 片段森林），富文本只是
// 其中一个编辑视图。本包提供这条通路的两端，但**本轮只做转换能力，不改任何真源** ——
// 文章正文仍然是 contents.data.body 里的 HTML 字符串（改造真源是决策 5 的第 4 步，
// 要动 contents 结构与内容模板绑定，单独一轮做）。所以现在的能力定位是：
//
//	富文本 → 组件树   把文章/外部 HTML 导入画布、给内容模板起稿；
//	组件树 → 富文本   把画布内容导出成可读的 HTML（Trix 视图、跨站点搬运）。
//
// # 映射表（与文档一一对应）
//
//	块级标签 ↔ 组件（双向等价）：
//	  h1~h6      → core.heading   {tag, text}
//	  p          → core.text      {mode: richtext, text}
//	  ul / ol    → core.list      {style: dot|number, items[]}
//	  blockquote → core.quote     {text}
//	  img        → core.image     {src, alt, title}
//	  figure     → core.image     {src, alt, caption}（caption 取 figcaption）
//	  hr         → core.divider   {}
//	  table      → core.table     {caption, headers[], rows[][]}
//	  pre        → core.text      {mode: richtext, text: 保留 <pre>}
//
// 行级格式（strong / em / s / u / del / code / a / br）**不单独成组件**：
// 它们留在 core.text 的富文本字段里（决策 5 明文规定），因此 core.text 是唯一
// 「带着格式进、带着格式出」的组件。
//
// # 三条不变量
//
//  1. **降级不静默**（决策 5）：白名单外的标签不是被悄悄删掉，而是产生一条 Warning。
//     剥壳保内容（div/section 这类结构标签 → unwrap）与整块丢弃（script/style/iframe
//     → drop）分开记录 —— 前者内容还在，后者内容没了，混在一起运营会找不到丢的东西。
//
//  2. **文本一律过白名单**：写进 props 的富文本片段都要经 core.SanitizeRichHTML
//     （全站唯一白名单来源），本包不维护第二份白名单。
//
//  3. **只有可逆子集能 round-trip**：上面那张映射表内的组件双向等价；
//     其余组件（cardstack / tabs / productList 等）在导出方向只能变成占位块，
//     且在富文本里编辑后**无法还原** —— 这条边界写在 Warning 里，不靠使用者猜。
package richdoc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"go_wp/internal/builder/core"
)

// WarningAction 降级动作。三类分开记：内容还在 / 内容没了 / 组件不可逆。
type WarningAction string

const (
	// ActionUnwrap 剥壳保内容：结构标签（div/section/span 等）被去掉，子内容照常转换。
	ActionUnwrap WarningAction = "unwrap"
	// ActionDrop 整块丢弃：可执行或外部内容（script/style/iframe/form 控件等）。
	ActionDrop WarningAction = "drop"
	// ActionTrim 内容被裁剪：超长文本按 maxlen 截断，或空白节点被忽略。
	ActionTrim WarningAction = "trim"
	// ActionPlaceholder 导出方向：该组件不在可逆子集内，只能输出占位块。
	ActionPlaceholder WarningAction = "placeholder"
)

// Warning 一次不静默的降级记录。
type Warning struct {
	// Tag 触发降级的原标签名（小写，如 "div"）或组件类型（如 "core.cardstack"）。
	Tag string
	// Action 降级动作。
	Action WarningAction
	// Detail 人话说明（会直接进编辑器提示，不要写成内部术语）。
	Detail string
}

// String 便于日志与断言输出。
func (w Warning) String() string {
	return fmt.Sprintf("%s[%s]: %s", w.Tag, w.Action, w.Detail)
}

// Result 导入结果（HTML → 组件树）。
type Result struct {
	// Nodes 组件树根节点序列。
	Nodes []*core.Node
	// Warnings 降级记录（空表示这次转换没有任何信息损失）。
	Warnings []Warning
}

// ExportResult 导出结果（组件树 → HTML）。
type ExportResult struct {
	// HTML 富文本 HTML 片段。
	HTML string
	// Warnings 降级记录（含不可逆组件的占位说明）。
	Warnings []Warning
	// Lossless 是否无损：只要有一处降级就是 false。
	//
	// 调用方据此决定能不能把结果当成"原内容的另一种视图"——
	// 有损的导出拿去覆盖原文（而不是只用来展示）就会真的丢东西。
	Lossless bool
}

// 内容长度上限：与 core.text 的 ct:"richtext,maxlen=30000" 和 core.SanitizeRichHTML
// 的上限保持同一个数（三处必须一致，否则会出现「存得进去、读出来是空」的字段）。
const maxTextLen = core.MaxRichLen

// newNode 构造一个组件节点（ID 用 uuid：与 builder.CloneNodeWithNewIDs 同口径）。
func newNode(componentType string, props any) (*core.Node, error) {
	raw, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("组件 %s 的属性序列化失败: %w", componentType, err)
	}
	return &core.Node{ID: newID(), Type: componentType, Props: raw}, nil
}

// sanitizeText 富文本片段统一入口（白名单唯一来源在 core）。
func sanitizeText(src string) string {
	return core.SanitizeRichHTML(strings.TrimSpace(src))
}

// hasVisibleContent 清洗后的片段是否还有可见内容。
//
// 只判断"有没有东西"：纯图片、纯文本、纯链接都算有内容；
// 空段落、只有空格的段落、被白名单清空后的空壳都算没有。
func hasVisibleContent(htmlFragment string) bool {
	if strings.TrimSpace(core.StripRichTags(htmlFragment)) != "" {
		return true
	}
	// 剥掉标签后为空，但可能只剩图片：img 的 alt 也是可读内容，
	// 一张没有 alt 的图仍然是要保留的（它迟早要补 alt，不该在导入时消失）。
	return strings.Contains(strings.ToLower(htmlFragment), "<img")
}

// renderInner 渲染节点的子节点为 HTML 字符串（等价于 innerHTML）。
func renderInner(n *html.Node) (string, error) {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&sb, c); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// nodeText 取节点的纯文本（去标签、合并空白）。
func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			sb.WriteString(x.Data)
			return
		}
		if x.Type == html.ElementNode && x.DataAtom == atom.Br {
			sb.WriteString(" ")
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(sb.String()), " ")
}

// findElement 深度优先找第一个指定标签的元素。
func findElement(n *html.Node, name string) *html.Node {
	if n.Type == html.ElementNode && n.Data == name {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, name); found != nil {
			return found
		}
	}
	return nil
}

// childElements 取直接子元素与文本节点（忽略注释、doctype 等）。
func childElements(n *html.Node) []*html.Node {
	out := make([]*html.Node, 0, 8)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.ElementNode, html.TextNode:
			out = append(out, c)
		}
	}
	return out
}

// isBlankText 是否纯空白文本节点。
func isBlankText(n *html.Node) bool {
	return n.Type == html.TextNode && strings.TrimSpace(n.Data) == ""
}

// newID 生成节点 ID。
//
// 用 uuid 而不是前端那种 "heading-1" 的编号式 ID：编号要先知道目标文档里已有哪些 ID
// 才能保证不撞（前端有整个文档可查），而导入器只拿到一段 HTML —— 编号式 ID 一旦与
// 目标文档撞号，画布上会出现两个同 ID 节点，选中/拖拽全乱。uuid 不需要这份上下文。
func newID() string { return uuid.NewString() }

// attrOf 取元素属性值。
func attrOf(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}
