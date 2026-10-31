package mailservice

import (
	"fmt"
	"strings"

	"go_wp/internal/templates"
)

type TemplateTree struct {
	Schema int          `json:"schema"`
	Root   TemplateNode `json:"root"`
}

type TemplateNode struct {
	Key  string `json:"key"`
	Type string `json:"type"`

	Attrs    map[string]string `json:"attrs"`
	Children []TemplateNode    `json:"children"`

	Text string `json:"text"`
}

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

const tplContainerWidth = 600

var attrOrder = []string{
	"background-color",
	"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
	"border", "border-radius", "border-top", "border-bottom",
	"color", "font-size", "font-weight", "line-height", "text-align", "text-decoration",
	"width", "height", "max-width",
}

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

func writeNode(b *strings.Builder, n TemplateNode, depth int) error {
	switch n.Type {
	case tplBody:

		for _, child := range n.Children {
			if err := writeNode(b, child, depth); err != nil {
				return err
			}
		}
		return nil

	case tplRaw:

		b.WriteString(n.Text)
		return nil

	default:
		inner, err := renderInner(n, depth)
		if err != nil {
			return err
		}
		classAttr := ""
		if n.Type == tplColumn {

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

func renderInner(n TemplateNode, depth int) (string, error) {

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
		data["Href"] = href
		data["Label"] = n.Text
	case tplImage:

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

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

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

			continue
		}
		parts = append(parts, key+":"+escapeAttr(strings.TrimSpace(value)))
	}
	return strings.Join(parts, ";")
}

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

func escapeAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
