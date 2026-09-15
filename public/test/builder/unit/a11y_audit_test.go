package unit

// a11y_audit_test.go — 全组件产物的无障碍审计。
//
// 与 schema_audit_test.go 同源思路：那份保证「每个控件 kind 都有渲染分支」，这份保证
// 「每个组件的产物满足最基本的可访问性约束」。组件新增或改动若引入下列问题会直接报出来，
// 而不是等人工评审 —— cardstack 的 <label> 包 <a>、tabs 的 role=tab 挂在不可聚焦的 label 上、
// form 的 label 不与控件关联，都是这一族问题。
//
// 规则（来自 WCAG / HTML 规范的硬约束）：
//  1. <img> 必须有 alt 属性（空值合法 = 装饰性图片，缺属性不行）；
//  2. <button> 必须有可访问名（文本内容或 aria-label）；
//  3. <label> 内不得出现 <a> / <button>（规范禁止 label 含交互式内容）；
//  4. 文本类表单控件（input 非 checkbox/radio/hidden/submit、textarea、select）
//     必须有关联 label（被 label 包裹，或存在 label[for=它的 id]）；
//  5. role="tab" 必须挂在可聚焦元素上（button / input / 带 tabindex）。

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"

	xhtml "golang.org/x/net/html"
)

// a11yChild 生成第 i 个通用子节点（叶子组件）：容器类组件用它渲染出内容。
// ID 必须逐个不同 —— 重复 ID 会被组件校验直接拒绝，测试就白白跳过这些组件。
func a11yChild(i int) string {
	return fmt.Sprintf(`{"id":"c%d","type":"core.heading","props":{"text":"小节 %d","tag":"h3"}}`, i, i)
}

// a11yProps 空 props 渲染不出内容（或直接被校验拒绝）的组件所需的最小可用参数。
var a11yProps = map[string]string{
	"core.accordion": `{"items":[{"title":"问题一"}]}`,
	"core.badge":     `{"text":"新"}`,
	"core.button":    `{"action":"external","text":"按钮","value":"https://example.com/x"}`,
	"core.card":      `{"title":"标题","text":"正文"}`,
	"core.container": `{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}}`,
	"core.countdown": `{"targetDate":"2030-01-01 00:00:00"}`,
	"core.faq":       `{"items":[{"question":"问题一","answer":"答案一"}]}`,
	"core.form":      `{"submitLabel":"提交","fields":[{"type":"text","label":"姓名","name":"name"},{"type":"email","label":"邮箱","name":"email"},{"type":"textarea","label":"留言","name":"msg"},{"type":"select","label":"城市","name":"city","options":["北京"]},{"type":"checkbox","label":"同意","name":"agree"}]}`,
	"core.gallery":   `{"items":[{"url":"/a.jpg","alt":"示例图"}]}`,
	"core.heading":   `{"text":"标题","tag":"h2"}`,
	"core.image":     `{"src":"/a.jpg","alt":"示例图"}`,
	"core.list":      `{"items":[{"text":"条目一"}]}`,
	"core.nav":       `{"items":[{"label":"首页","url":"/"},{"label":"关于","url":"/about"}]}`,
	"core.progress":  `{"value":60}`,
	"core.quote":     `{"text":"引用内容","author":"作者"}`,
	"core.rating":    `{"value":4}`,
	"core.tabs":      `{"tabs":[{"label":"一"},{"label":"二"}]}`,
	"core.text":      `{"text":"正文"}`,
	"core.video":     `{"url":"/a.mp4"}`,
}

// a11yChildCount 需要子节点才通过校验的组件。
var a11yChildCount = map[string]int{
	"core.accordion": 1, "core.container": 1, "core.marquee": 1, "core.slider": 1, "core.tabs": 2,
}

// TestComponentA11yAudit 逐个组件编译产物并做无障碍规则检查。
func TestComponentA11yAudit(t *testing.T) {
	types := core.Types()
	if len(types) == 0 {
		t.Fatal("组件类型列表为空")
	}
	violations := map[string][]string{}
	var audited, skipped []string

	for _, typeName := range types {
		props := a11yProps[typeName]
		if props == "" {
			props = "{}"
		}
		var children []string
		for i := 0; i < a11yChildCount[typeName]; i++ {
			children = append(children, a11yChild(i))
		}
		doc := fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":%q,"props":%s,"children":[%s]}]}`,
			typeName, props, strings.Join(children, ","))
		page, err := builder.ParsePage([]byte(doc))
		if err != nil {
			t.Errorf("%s 文档解析失败: %v", typeName, err)
			continue
		}
		// 带集合解析器编译：cardstack / gallery 的「内容集合」模式没它渲染不出卡片，
		// 而未渲染的组件恰恰最容易漏掉审计。
		compiled, err := compile(t, page, builder.WithCollectionResolver(galleryCollection{}))
		if err != nil {
			// 参数不足（如 globalref 需要真实 block）导致的跳过必须打印，否则
			// 「没审计到」会看起来像「没问题」。
			skipped = append(skipped, typeName+" → "+err.Error())
			continue
		}
		audited = append(audited, typeName)
		for _, v := range auditHTML(compiled.HTML) {
			violations[typeName] = append(violations[typeName], v)
		}
	}

	keys := make([]string, 0, len(violations))
	for k := range violations {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Errorf("%s：%s", k, strings.Join(violations[k], "；"))
	}
	if len(skipped) > 0 {
		sort.Strings(skipped)
		t.Logf("跳过的组件（参数不足或需特殊装配，需人工确认 a11y）：%s", strings.Join(skipped, " | "))
	}
	t.Logf("已审计组件 %d / %d", len(audited), len(types))
}

// auditHTML 解析产物并对每个元素跑一遍规则。
func auditHTML(htmlText string) []string {
	root, err := xhtml.Parse(strings.NewReader(htmlText))
	if err != nil {
		return []string{"HTML 解析失败: " + err.Error()}
	}
	// 先收集 label[for] 的目标 id（控件关联检查）与 id 使用情况（重复 id 检查）。
	labeled := map[string]bool{}
	seenID := map[string]bool{}
	var problems []string
	var collect func(*xhtml.Node)
	collect = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			if n.Data == "label" {
				if id := attr(n, "for"); id != "" {
					labeled[id] = true
				}
			}
			if id := attr(n, "id"); id != "" {
				if seenID[id] {
					problems = append(problems, "重复 id="+id)
				}
				seenID[id] = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(root)

	var check func(*xhtml.Node)
	check = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "img":
				if !hasAttr(n, "alt") {
					problems = append(problems, "<img> 缺少 alt 属性")
				}
			case "button":
				if !hasAttr(n, "aria-label") && !hasAttr(n, "aria-labelledby") && strings.TrimSpace(textOf(n)) == "" {
					problems = append(problems, "<button> 没有可访问名")
				}
			case "label":
				if bad := innerInteractive(n); bad != "" {
					problems = append(problems, "<label> 内出现交互式内容 <"+bad+">")
				}
				// label[for] 是关联控件的**可访问名来源**（tabs 的 sr-only radio 就靠它取名）：
				// 带文本的 label 加 aria-hidden=true 会让读屏拿不到控件名，只剩「单选按钮」
				// 而没有标签文本（UI-009 的成因）。防重复朗读的正确做法是不加 aria-hidden
				// —— 关联 label 的文本本就作为控件名被引用，不会被读两遍。
				// 空 label（如 cardstack 的 zoom-layer 覆盖层）不在此列：它没有文本，
				// 名字由控件的 aria-label 提供，隐藏它反而避免了空名节点。
				if attr(n, "for") != "" && attr(n, "aria-hidden") == "true" && strings.TrimSpace(textOf(n)) != "" {
					problems = append(problems, "label[for] 带 aria-hidden=true（隐藏了控件的可访问名来源）")
				}
			case "input":
				switch strings.ToLower(attr(n, "type")) {
				case "checkbox", "radio", "hidden", "submit", "button", "file", "range", "color":
				default:
					if !inLabel(n) && !labeled[attr(n, "id")] {
						problems = append(problems, "<input> 没有关联 label")
					}
				}
			case "textarea", "select":
				if !inLabel(n) && !labeled[attr(n, "id")] {
					problems = append(problems, "<"+n.Data+"> 没有关联 label")
				}
			case "ul", "ol":
				// 列表的直接子元素只能是 <li>：中间夹一层 div 会被读屏当成
				// 「列表里没有项目」，项目数播报直接失效。
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == xhtml.ElementNode && c.Data != "li" && c.Data != "template" && c.Data != "script" {
						problems = append(problems, "<"+n.Data+"> 的直接子元素出现 <"+c.Data+">（只能是 li）")
					}
				}
			case "a":
				// 没有 href 的 <a> 既不可聚焦也不是链接（除非显式给了 role 当自定义控件用）。
				if !hasAttr(n, "href") && !hasAttr(n, "role") {
					problems = append(problems, "<a> 没有 href（不可聚焦、语义不明）")
				}
			}
			// aria-hidden 子树里放可聚焦元素是最常见的 a11y 反模式：读屏跳过它，键盘却能进去。
			if attr(n, "aria-hidden") == "true" && hasFocusable(n) {
				problems = append(problems, "aria-hidden=true 的 <"+n.Data+"> 子树里有可聚焦元素")
			}
			// 正 tabindex 会打乱全局 Tab 顺序（WAI-ARIA 明确不推荐）。
			if ti := attr(n, "tabindex"); ti != "" && ti != "0" && ti != "-1" {
				problems = append(problems, "tabindex="+ti+" 为正值（打乱 Tab 顺序）")
			}
			if attr(n, "role") == "tab" && n.Data != "button" && n.Data != "input" && !hasAttr(n, "tabindex") {
				problems = append(problems, "role=tab 挂在不可聚焦的 <"+n.Data+"> 上")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			check(c)
		}
	}
	check(root)
	return problems
}

func hasAttr(n *xhtml.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func attr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// innerInteractive 返回 label 内第一个交互元素名（无则空串）。
func innerInteractive(n *xhtml.Node) string {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			switch c.Data {
			case "a", "button":
				return c.Data
			}
			if bad := innerInteractive(c); bad != "" {
				return bad
			}
		}
	}
	return ""
}

// hasFocusable 判断子树里是否存在可聚焦元素（链接、表单控件、显式 tabindex）。
func hasFocusable(n *xhtml.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			switch c.Data {
			case "a":
				if hasAttr(c, "href") {
					return true
				}
			case "button", "input", "select", "textarea":
				return true
			}
			if ti := attr(c, "tabindex"); ti != "" && ti != "-1" {
				return true
			}
			if hasFocusable(c) {
				return true
			}
		}
	}
	return false
}

// inLabel 判断节点是否被 label 包裹（隐式关联）。
func inLabel(n *xhtml.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == xhtml.ElementNode && p.Data == "label" {
			return true
		}
	}
	return false
}

func textOf(n *xhtml.Node) string {
	var sb strings.Builder
	var walk func(*xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.TextNode {
			sb.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}
