package richdoc

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	dividerpkg "go_wp/pkg/builder/components/divider"
	headingpkg "go_wp/pkg/builder/components/heading"
	imagepkg "go_wp/pkg/builder/components/image"
	listpkg "go_wp/pkg/builder/components/list"
	quotepkg "go_wp/pkg/builder/components/quote"
	tablepkg "go_wp/pkg/builder/components/table"
	textpkg "go_wp/pkg/builder/components/text"
	"go_wp/pkg/builder/core"
)

const maxHeadingLen = 500

const maxQuoteLen = 1000

func HTMLToNodes(src string) (*Result, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("HTML 解析失败: %w", err)
	}
	imp := &importer{}
	root := findElement(doc, "body")
	if root == nil {
		root = doc
	}
	nodes, err := imp.blocks(childElements(root))
	if err != nil {
		return nil, err
	}
	return &Result{Nodes: nodes, Warnings: imp.warnings}, nil
}

type importer struct {
	warnings []Warning
}

func (im *importer) warn(tag string, action WarningAction, detail string) {
	im.warnings = append(im.warnings, Warning{Tag: tag, Action: action, Detail: detail})
}

func (im *importer) blocks(children []*html.Node) (nodes []*core.Node, err error) {
	nodes = make([]*core.Node, 0, len(children))
	inline := make([]*html.Node, 0, 4)

	flush := func() error {
		if len(inline) == 0 {
			return nil
		}
		n, ferr := im.inlineTextNode(inline)
		inline = inline[:0]
		if ferr != nil {
			return ferr
		}
		if n != nil {
			nodes = append(nodes, n)
		}
		return nil
	}

	for _, c := range children {
		if isBlankText(c) {
			continue
		}
		if isInline(c) {
			inline = append(inline, c)
			continue
		}
		if err = flush(); err != nil {
			return nil, err
		}
		produced, berr := im.block(c)
		if berr != nil {
			return nil, berr
		}
		nodes = append(nodes, produced...)
	}
	if err = flush(); err != nil {
		return nil, err
	}
	return nodes, nil
}

func (im *importer) block(n *html.Node) ([]*core.Node, error) {
	if n.Type == html.TextNode {
		return im.blocks([]*html.Node{n})
	}
	if n.Type != html.ElementNode {
		return nil, nil
	}

	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return im.heading(n)
	case atom.P:

		return im.blocks(childElements(n))
	case atom.Ul, atom.Ol:
		return im.list(n)
	case atom.Blockquote:
		return im.blockquote(n)
	case atom.Pre:
		return im.pre(n)
	case atom.Img:
		return im.image(n, "")
	case atom.Figure:
		return im.figure(n)
	case atom.Hr:
		return im.divider()
	case atom.Table:
		return im.table(n)
	case atom.Details:
		return im.details(n)
	}

	if structuralTags[n.DataAtom] {
		im.warn(n.Data, ActionUnwrap,
			"结构标签 <"+n.Data+"> 没有对应组件，已去掉这层壳并保留里面的内容")
		return im.blocks(childElements(n))
	}

	if dropTags[n.DataAtom] {
		im.warn(n.Data, ActionDrop,
			"已丢弃 <"+n.Data+">：脚本、表单控件与外部嵌入不能进静态产物，留着也只是无效标签")
		return nil, nil
	}

	im.warn(n.Data, ActionUnwrap,
		"不认识的标签 <"+n.Data+"> 已去壳保留文字，请检查这一段格式是否符合预期")
	return im.blocks(childElements(n))
}

func (im *importer) heading(n *html.Node) ([]*core.Node, error) {
	textValue := nodeText(n)
	if textValue == "" {
		im.warn(n.Data, ActionTrim, "空标题 <"+n.Data+"> 没有内容，已忽略")
		return nil, nil
	}
	if runes := []rune(textValue); len(runes) > maxHeadingLen {
		textValue = string(runes[:maxHeadingLen])
		im.warn(n.Data, ActionTrim,
			fmt.Sprintf("标题超过 %d 字已截断（标题字段是纯文本，没有富文本那种结构可保留）", maxHeadingLen))
	}
	node, err := newNode(headingpkg.Type, headingpkg.Props{Text: textValue, Tag: n.Data})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) list(n *html.Node) ([]*core.Node, error) {
	items := make([]listpkg.Item, 0, 4)
	for _, li := range childElements(n) {
		if li.Type != html.ElementNode || li.DataAtom != atom.Li {
			continue
		}
		item := listpkg.Item{Text: nodeText(li)}
		if a := findElement(li, "a"); a != nil {
			item.Link = attrOf(a, "href")
		}
		if item.Text == "" && item.Link == "" {
			continue
		}

		if hasInlineMarkup(li) {
			im.warn("li", ActionTrim, "列表项里的加粗、斜体等格式无法保留（列表项只有纯文本与链接两栏）")
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		im.warn(n.Data, ActionTrim, "空列表 <"+n.Data+"> 已忽略")
		return nil, nil
	}
	style := listpkg.StyleDot
	if n.DataAtom == atom.Ol {
		style = listpkg.StyleNumber
	}
	node, err := newNode(listpkg.Type, listpkg.Props{Items: items, Style: style})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) blockquote(n *html.Node) ([]*core.Node, error) {
	cite := directChild(n, atom.Cite)
	inner, err := renderInnerExcept(n, cite)
	if err != nil {
		return nil, err
	}
	textValue := sanitizeText(inner)
	author := ""
	if cite != nil {
		author = nodeText(cite)
	}
	if !hasVisibleContent(textValue) {
		if author == "" {
			im.warn("blockquote", ActionTrim, "空引用已忽略")
			return nil, nil
		}

		im.warn("blockquote", ActionTrim, "引用没有正文，只有出处，已忽略")
		return nil, nil
	}
	if len(textValue) > maxQuoteLen {
		im.warn("blockquote", ActionTrim,
			fmt.Sprintf("引用超过 %d 字符，请自行精简（引用组件的建议上限；不自动裁剪是因为截断会切断标签）", maxQuoteLen))
	}
	node, err := newNode(quotepkg.Type, quotepkg.Props{Text: textValue, Author: author})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) pre(n *html.Node) ([]*core.Node, error) {
	var raw strings.Builder
	if err := html.Render(&raw, n); err != nil {
		return nil, err
	}
	textValue := sanitizeText(raw.String())
	if !hasVisibleContent(textValue) {
		im.warn("pre", ActionTrim, "空代码块已忽略")
		return nil, nil
	}
	node, err := newNode(textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: textValue})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) details(n *html.Node) ([]*core.Node, error) {
	inner, err := renderInner(n)
	if err != nil {
		return nil, err
	}
	textValue := sanitizeText("<details>" + inner + "</details>")
	if !hasVisibleContent(textValue) {
		im.warn("details", ActionTrim, "空折叠块已忽略")
		return nil, nil
	}
	node, err := newNode(textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: textValue})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) image(n *html.Node, caption string) ([]*core.Node, error) {
	src := attrOf(n, "src")
	if src == "" {
		im.warn("img", ActionDrop, "图片没有 src（外站防盗链或粘贴残留常见），已丢弃该图")
		return nil, nil
	}
	node, err := newNode(imagepkg.Type, imagepkg.Props{
		Src:     src,
		Alt:     attrOf(n, "alt"),
		Title:   attrOf(n, "title"),
		Caption: caption,
	})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) figure(n *html.Node) ([]*core.Node, error) {
	img := findElement(n, "img")
	if img == nil {
		im.warn("figure", ActionUnwrap, "<figure> 里没有图片，已去壳保留内容")
		return im.blocks(childElements(n))
	}
	caption := ""
	if fc := findElement(n, "figcaption"); fc != nil {
		caption = nodeText(fc)
	}
	return im.image(img, caption)
}

func (im *importer) divider() ([]*core.Node, error) {
	node, err := newNode(dividerpkg.Type, dividerpkg.Props{})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) table(n *html.Node) ([]*core.Node, error) {
	props := tablepkg.Props{}
	if captionNode := findElement(n, "caption"); captionNode != nil {
		props.Caption = nodeText(captionNode)
	}
	headers := make([]string, 0, 4)
	rows := make([][]string, 0, 4)
	for _, tr := range findAll(n, atom.Tr) {
		cells := make([]string, 0, 4)
		thCount, tdCount := 0, 0
		for _, cell := range childElements(tr) {
			if cell.Type != html.ElementNode {
				continue
			}
			switch cell.DataAtom {
			case atom.Th:
				thCount++
				cells = append(cells, nodeText(cell))
			case atom.Td:
				tdCount++
				cells = append(cells, nodeText(cell))
			}
		}
		if len(cells) == 0 {
			continue
		}

		if thCount > 0 && tdCount == 0 && len(headers) == 0 {
			headers = append(headers, cells...)
			continue
		}
		rows = append(rows, cells)
	}
	if len(headers) == 0 && len(rows) == 0 {
		im.warn("table", ActionTrim, "空表格已忽略")
		return nil, nil
	}
	props.Headers = headers
	props.Rows = rows
	node, err := newNode(tablepkg.Type, props)
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

func (im *importer) inlineTextNode(nodes []*html.Node) (*core.Node, error) {
	var sb strings.Builder
	for _, n := range nodes {
		if err := html.Render(&sb, n); err != nil {
			return nil, err
		}
	}
	textValue := sanitizeText(sb.String())
	if !hasVisibleContent(textValue) {
		return nil, nil
	}

	if !fragmentHasBlockLevel(textValue) {
		textValue = "<p>" + textValue + "</p>"
	}

	if len(textValue) > maxTextLen {
		textValue = truncateToBytes(textValue, maxTextLen)
		textValue = sanitizeText(textValue)
		im.warn("text", ActionTrim,
			fmt.Sprintf("段落超过 %d 字节已截断（超限的富文本在构建期会被整段判空，截断至少保住前半段）", maxTextLen))
	}
	node, err := newNode(textpkg.Type, textpkg.Props{Mode: textpkg.ModeRichText, Text: textValue})
	if err != nil {
		return nil, err
	}
	return node, nil
}

func fragmentHasBlockLevel(fragment string) bool {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {

		return true
	}
	body := findElement(doc, "body")
	if body == nil {
		return true
	}
	for _, c := range childElements(body) {
		if containsBlockLevel(c) {
			return true
		}
	}
	return false
}

func containsBlockLevel(n *html.Node) bool {
	if n.Type == html.ElementNode && !inlineTags[n.DataAtom] {
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if containsBlockLevel(c) {
			return true
		}
	}
	return false
}

func truncateToBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func isInline(n *html.Node) bool {
	if n.Type == html.TextNode {
		return true
	}
	if n.Type != html.ElementNode {
		return false
	}
	return inlineTags[n.DataAtom]
}

func hasInlineMarkup(n *html.Node) bool {
	found := false
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if found {
			return
		}
		if x.Type == html.ElementNode && markupTags[x.DataAtom] {
			found = true
			return
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return found
}

func directChild(n *html.Node, a atom.Atom) *html.Node {
	for _, c := range childElements(n) {
		if c.Type == html.ElementNode && c.DataAtom == a {
			return c
		}
	}
	return nil
}

func renderInnerExcept(n *html.Node, skip *html.Node) (string, error) {
	if skip == nil {
		return renderInner(n)
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c == skip {
			continue
		}
		if err := html.Render(&sb, c); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

func findAll(n *html.Node, a atom.Atom) []*html.Node {
	out := make([]*html.Node, 0, 4)
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.ElementNode && x.DataAtom == a {
			out = append(out, x)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

var inlineTags = map[atom.Atom]bool{
	atom.A: true, atom.Strong: true, atom.B: true, atom.Em: true, atom.I: true,
	atom.S: true, atom.U: true, atom.Del: true, atom.Code: true, atom.Br: true,
	atom.Span: true, atom.Mark: true, atom.Sub: true, atom.Sup: true, atom.Small: true,
}

var markupTags = map[atom.Atom]bool{
	atom.Strong: true, atom.B: true, atom.Em: true, atom.I: true,
	atom.S: true, atom.U: true, atom.Del: true, atom.Code: true, atom.Mark: true,
}

var structuralTags = map[atom.Atom]bool{
	atom.Div: true, atom.Section: true, atom.Article: true, atom.Aside: true,
	atom.Header: true, atom.Footer: true, atom.Main: true, atom.Nav: true,
	atom.Address: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
	atom.Center: true, atom.Font: true, atom.Summary: true,
}

var dropTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Iframe: true, atom.Object: true,
	atom.Embed: true, atom.Form: true, atom.Input: true, atom.Textarea: true,
	atom.Select: true, atom.Option: true, atom.Button: true, atom.Label: true,
	atom.Meta: true, atom.Link: true, atom.Base: true, atom.Template: true,
	atom.Canvas: true, atom.Video: true, atom.Audio: true, atom.Source: true,
	atom.Track: true, atom.Map: true, atom.Area: true, atom.Svg: true,
	atom.Math: true, atom.Noscript: true,
}
