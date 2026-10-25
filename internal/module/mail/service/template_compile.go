package mailservice

// template_compile.go — 邮件模板的可视化正文编译器（块树 → 确定性 HTML / 纯文本）。
//
// ## 为什么由 Go 编译，而不是前端拼 HTML
//
// 一份正文只能有一份生成器。前端拼的话，预览与真实发信会是两套（预览看起来对、发出去
// 不对），而那种差异没有人会在测试里发现。所以：编辑器的画布是 **iframe srcdoc**，
// 内容就是这里编译出来的 HTML —— 所见即所发。
//
// ## 确定性（AGENTS.md 不变量 5 的延伸）
//
// 同一棵树编译两次，字节必须相同。三条约束保证它：
//
//  1. **不迭代 map**：属性按固定顺序 emit（BACKGROUND → PADDING → BORDER → TEXT → WIDTH）；
//  2. 样式一律 inline，`<style>` 里只放 `@media (max-width:600px)` 的响应式覆盖；
//  3. 所有容器都是 `<table role="presentation">`（邮件客户端对 div 布局的支持不可靠）。
//
// ## 变量占位符不在这里展开
//
// `{{.name}}` 这类占位符**原样保留**在编译产物里，发送时仍走 RenderTemplate
// （missingkey=error 语义不变）。理由：编译发生在保存时（那时没有变量值），
// 而「变量拼错要在发信前报错」这条判据挂在渲染链路而不是编译器上。
//
// ## 块类型只有 12 种，故意小于上游的 24 种
//
// body / wrapper / section / column / group / text / image / button / divider / spacer /
// social / raw。父子白名单照抄 notifuse 的 types.ts（validChildren），少而封闭：
// 每多一种块，就要多一份「它在 Outlook 里长什么样」的知识。

import (
	"fmt"
	"strings"

	"go_wp/internal/templates"
)

// TemplateTree 模板正文的块树（编辑器与编译器之间的契约）。
type TemplateTree struct {
	Schema int          `json:"schema"` // 1
	Root   TemplateNode `json:"root"`
}

// TemplateNode 一个块。
type TemplateNode struct {
	// Key 稳定 id：重排不换（编辑器靠它定位选中项）。
	Key  string `json:"key"`
	Type string `json:"type"`
	// Attrs 值一律字符串：空串表示「用默认」。只用字符串是为了让 attrs 可比较、
	// 可序列化、且不出现「数字 0 与缺省」的二义性。
	Attrs    map[string]string `json:"attrs"`
	Children []TemplateNode    `json:"children"`
	// Text 仅 text / button / raw 使用。
	Text string `json:"text"`
}

// TemplateCompileError 编译失败（带节点定位）。
//
// NodeKey 是**必需**的：一张正文几十个块，只报「某个块不合法」等于没报。
type TemplateCompileError struct {
	NodeKey string
	Reason  string
}

func (e *TemplateCompileError) Error() string {
	if e == nil {
		return ""
	}
	if e.NodeKey == "" {
		return "模板编译失败: " + e.Reason
	}
	return fmt.Sprintf("模板编译失败（块 %s）: %s", e.NodeKey, e.Reason)
}

// 块类型（12 种）。
const (
	tplBody    = "body"
	tplWrapper = "wrapper"
	tplSection = "section"
	tplColumn  = "column"
	tplGroup   = "group"
	tplText    = "text"
	tplImage   = "image"
	tplButton  = "button"
	tplDivider = "divider"
	tplSpacer  = "spacer"
	tplSocial  = "social"
	tplRaw     = "raw"
)

// validChildren 父子白名单（照抄 notifuse 的 types.ts:388-435，删掉本仓不支持的块）。
//
// 写成封闭表而不是「默认允许」：允许自由嵌套的结果是用户在 column 里塞 section，
// 而那在邮件客户端里渲染成一张错位的表格 —— 预览里看不出来，收件人那边看得出来。
var validChildren = map[string][]string{
	tplBody:    {tplWrapper, tplSection, tplRaw},
	tplWrapper: {tplSection, tplRaw},
	tplSection: {tplColumn, tplGroup, tplRaw},
	tplColumn:  {tplText, tplButton, tplImage, tplDivider, tplSpacer, tplSocial, tplRaw},
	tplGroup:   {tplColumn},
	tplText:    nil,
	tplButton:  nil,
	tplImage:   nil,
	tplDivider: nil,
	tplSpacer:  nil,
	tplSocial:  nil,
	tplRaw:     nil,
}

// tplContainerWidth 内容宽度（600px 是邮件客户端的安全值）。
const tplContainerWidth = 600

// attrOrder 属性的**固定输出顺序**（不迭代 map 的第二半：顺序由这张表决定）。
//
// 分五档：背景 → 内边距 → 边框 → 文字 → 宽高。档内按字母序拼 —— 只要顺序是同一份
// 规则算出来的，两次编译就是同一串字节。
var attrOrder = []string{
	"background-color",
	"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
	"border", "border-radius", "border-top", "border-bottom",
	"color", "font-size", "font-weight", "line-height", "text-align", "text-decoration",
	"width", "height", "max-width",
}

// CompileTemplate 把块树编译成 HTML 与纯文本。
//
// 两个产物一起返回：调用方（UpsertTemplate）要在一个事务里同时写两列，
// 分两次编译会有「HTML 新、纯文本旧」的窗口。
func CompileTemplate(tree TemplateTree) (html, text string, err error) {
	if tree.Schema != 1 {
		return "", "", &TemplateCompileError{Reason: fmt.Sprintf("不支持的块树版本: %d", tree.Schema)}
	}
	if tree.Root.Type != tplBody {
		return "", "", &TemplateCompileError{NodeKey: tree.Root.Key, Reason: "根块必须是 body"}
	}
	if err := ValidateTree(tree); err != nil {
		return "", "", err
	}

	var body strings.Builder
	if err := writeNode(&body, tree.Root, 0); err != nil {
		return "", "", err
	}
	html, rerr := templates.RenderFragment("mail_template_document", map[string]any{
		"Body": body.String(),
	})
	if rerr != nil {
		return "", "", &TemplateCompileError{Reason: "渲染文档骨架失败: " + rerr.Error()}
	}
	return html, compileText(tree.Root), nil
}

// ValidateTree 校验块树的形状（父子白名单 + 必需属性），不产出任何字节。
//
// 独立导出：前端「保存前校验」要调它，而那时不该付一次完整编译的代价。
func ValidateTree(tree TemplateTree) error {
	if tree.Root.Type == "" {
		return &TemplateCompileError{Reason: "没有根块"}
	}
	return validateNode(tree.Root)
}

func validateNode(n TemplateNode) error {
	if strings.TrimSpace(n.Key) == "" {
		return &TemplateCompileError{Reason: "存在没有 key 的块（编辑器靠它定位）"}
	}
	allowed, known := validChildren[n.Type]
	if !known {
		return &TemplateCompileError{NodeKey: n.Key, Reason: "未知块类型: " + n.Type}
	}
	if n.Type == tplImage && strings.TrimSpace(n.Attrs["src"]) == "" {
		return &TemplateCompileError{NodeKey: n.Key, Reason: "图片块需要 src"}
	}
	if n.Type == tplBody && len(n.Children) == 0 {
		// 空 body 是合法的（新建模板的初始状态），不报错。
		return nil
	}
	for _, child := range n.Children {
		if !containsString(allowed, child.Type) {
			return &TemplateCompileError{
				NodeKey: child.Key,
				Reason:  fmt.Sprintf("%s 里不能放 %s", n.Type, child.Type),
			}
		}
		if err := validateNode(child); err != nil {
			return err
		}
	}
	return nil
}

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// writeNode 把一个块写成 HTML（递归）。
//
// 结构（标签 / 属性名）在 internal/templates/fragments/mail_template_block.jet 与
// mail_template_node_inner.jet 里；Go 只负责**两个决定**：
//
//  1. 走树的顺序（文档序 → 确定性）；
//  2. 按固定属性顺序拼 inline style（不迭代 map → 确定性）。
//
// 为什么标签不在 Go 里：AGENTS.md 的前后端分界是「Go 是后端，Jet 模板 + HTML 是前端」，
// Go 里出现标签字面量会被 html_boundary 门禁拦下。那个判据是对的 —— 结构一旦长在 Go 里，
// 「这段正文长什么样」就有两个可以改的地方，而只有一处会被 review。
func writeNode(b *strings.Builder, n TemplateNode, depth int) error {
	switch n.Type {
	case tplBody:
		// body 本身不产出表格（外层已经是 <body>），只往下走。
		for _, child := range n.Children {
			if err := writeNode(b, child, depth); err != nil {
				return err
			}
		}
		return nil

	case tplRaw:
		// raw 原样输出（高级用户的逃生舱）：它不做转义，由作者自己负责。
		b.WriteString(n.Text)
		return nil

	default:
		inner, err := renderInner(n, depth)
		if err != nil {
			return err
		}
		classAttr := ""
		if n.Type == tplColumn {
			// .tpl-col 是文档级 <style> 里那条响应式规则的目标（窄屏改块级）。
			classAttr = "tpl-col"
		}
		html, rerr := templates.RenderFragment("mail_template_block", map[string]any{
			"IsRaw":     false,
			"Raw":       "",
			"ClassAttr": classAttr,
			"Style":     containerStyle(n),
			"Inner":     inner,
		})
		if rerr != nil {
			return &TemplateCompileError{NodeKey: n.Key, Reason: "渲染模板失败: " + rerr.Error()}
		}
		b.WriteString(html)
		return nil
	}
}

// renderInner 渲染一个块「td 里放什么」（容器 = 子块；内容块 = 自己的值）。
func renderInner(n TemplateNode, depth int) (string, error) {
	// 所有键都给默认值：Jet 对 map 里**缺失**的键会报 "no field or method"，
	// 而模板用一串 if/else if 读它们 —— 少给一个键就等于编译整棵树失败。
	data := map[string]any{
		"IsContainer": false, "Inner": "",
		"IsText": false, "Text": "",
		"IsButton": false, "Href": "", "Label": "",
		"IsImage": false, "Src": "", "Alt": "", "Width": "100%",
		"IsDivider": false,
		"IsSpacer":  false, "Height": "24",
		"IsSocial": false,
	}
	switch n.Type {
	case tplWrapper, tplSection, tplColumn, tplGroup:
		var child strings.Builder
		for _, c := range n.Children {
			if err := writeNode(&child, c, depth+1); err != nil {
				return "", err
			}
		}
		data["IsContainer"] = true
		data["Inner"] = child.String()
	case tplText:
		data["IsText"] = true
		data["Text"] = n.Text
	case tplButton:
		href := n.Attrs["href"]
		if href == "" {
			href = "#"
		}
		data["IsButton"] = true
		data["Href"] = href // Jet 负责转义（见上面 IsImage 的说明）
		data["Label"] = n.Text
	case tplImage:
		// **不再**在 Go 侧转义：Jet 的 {{.X}} 默认就是 HTML 转义，再转一次会变成
		// &amp;quot;（双重转义），属性值里的引号反而更容易被利用。
		data["IsImage"] = true
		data["Src"] = n.Attrs["src"]
		data["Alt"] = n.Attrs["alt"]
		data["Width"] = defaultStr(n.Attrs["width"], "100%")
	case tplDivider:
		data["IsDivider"] = true
	case tplSpacer:
		data["IsSpacer"] = true
		data["Height"] = defaultStr(n.Attrs["height"], "24")
	case tplSocial:
		data["IsSocial"] = true
		data["Text"] = n.Text
	default:
		return "", &TemplateCompileError{NodeKey: n.Key, Reason: "未知块类型: " + n.Type}
	}
	html, err := templates.RenderFragment("mail_template_node_inner", data)
	if err != nil {
		return "", &TemplateCompileError{NodeKey: n.Key, Reason: "渲染内层失败: " + err.Error()}
	}
	return html, nil
}

// defaultStr 空值取默认（attrs 里空串表示「用默认」）。
func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// containerStyle 按固定顺序拼出 inline style（**不迭代 map**）。
//
// 值在这里转义（模板里用 `| unsafe` 直接输出整串 style）：如果把整串交给 Jet 转义，
// `style="a;b"` 会变成 `style="a&amp;b"` —— 样式全废。
//
// 除属性表里列出的键，其余 attrs（如 href / src / alt / height）不是样式，跳过。
// `width` 例外：容器块的 width 是样式，图片块的 width 是 HTML 属性 —— 后者不走这里。
func containerStyle(n TemplateNode) string {
	if len(n.Attrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attrOrder))
	for _, key := range attrOrder {
		value, ok := n.Attrs[key]
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		if n.Type == tplImage && (key == "width" || key == "height") {
			// 图片的宽高走 HTML 属性（上面已经写过），不再重复成 style。
			continue
		}
		parts = append(parts, key+":"+escapeAttr(strings.TrimSpace(value)))
	}
	return strings.Join(parts, ";")
}

// compileText 按文档序抽纯文本：text / button / social 的文本 + raw 的原文。
//
// 段落之间空行：纯文本客户端（以及邮件的 text/plain 部分）靠空行分段，
// 全挤在一起会变成一大坨。
func compileText(n TemplateNode) string {
	var parts []string
	collectText(n, &parts)
	return strings.Join(parts, "\n\n")
}

func collectText(n TemplateNode, out *[]string) {
	switch n.Type {
	case tplText, tplButton, tplSocial:
		if t := strings.TrimSpace(stripTags(n.Text)); t != "" {
			*out = append(*out, t)
		}
	case tplRaw:
		if t := strings.TrimSpace(stripTags(n.Text)); t != "" {
			*out = append(*out, t)
		}
	}
	for _, child := range n.Children {
		collectText(child, out)
	}
}

// stripTags 去掉文本里残留的 HTML 标签（text 块的正文允许带少量标记）。
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapeAttr 属性值转义（引号 / 尖括号 / &）。
func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
