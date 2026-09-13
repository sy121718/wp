package richdoc

// import.go — 富文本 HTML → 组件树（决策 5 的导入方向）。
//
// 纯函数：输入一段 HTML，输出 []*core.Node + 降级记录。不读写数据库、不落盘、
// 不依赖任何配置表 —— 映射规则就是这个文件里的 switch 分支（与 docs/06-B 决策 5
// 的映射表一一对应，改映射改这里，没有第二处需要同步）。

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	dividerpkg "go_wp/internal/builder/components/divider"
	headingpkg "go_wp/internal/builder/components/heading"
	imagepkg "go_wp/internal/builder/components/image"
	listpkg "go_wp/internal/builder/components/list"
	quotepkg "go_wp/internal/builder/components/quote"
	tablepkg "go_wp/internal/builder/components/table"
	textpkg "go_wp/internal/builder/components/text"
	"go_wp/internal/builder/core"
)

// maxHeadingLen core.heading 的 Text 上限（ct:"text,maxlen=500"）。
// 标题是纯文本字段，超长直接裁字不会破坏结构（与富文本字段的处理不同）。
const maxHeadingLen = 500

// maxQuoteLen core.quote 的 Text 建议上限（ct:"richtext,maxlen=1000"）。
//
// 只用于提醒，不用于裁剪：富文本裁剪会切断标签（<stro…），而构建期真正的硬约束是
// core.MaxRichLen（超了整段判空）。让作者自己删一段，比程序切一半更可控。
const maxQuoteLen = 1000

// HTMLToNodes 把富文本 HTML 转成组件树。
//
// 这是「富文本 → 可视化」的唯一入口：文章导入画布、内容模板起稿、外部 HTML 搬运都走它。
// 返回的 Warnings 必须展示给使用者（决策 5：降级不静默）—— 转换成功但有损时吞掉警告，
// 作者会以为"都搬过来了"。
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

// importer 一次转换的上下文（只有警告累积，转换本身无副作用）。
type importer struct {
	warnings []Warning
}

func (im *importer) warn(tag string, action WarningAction, detail string) {
	im.warnings = append(im.warnings, Warning{Tag: tag, Action: action, Detail: detail})
}

// blocks 把一串兄弟节点转成组件序列。
//
// 分组规则：连续的行内内容合并成一个 core.text（行级格式因此天然留在 core.text 内，
// 与决策 5 的规定一致），块级元素各自成节点。
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

// block 转换单个块级节点（0 个 = 丢弃，1 个 = 常态，多个 = 一个标签拆成多段）。
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
		// <p> 不整体变成一个 core.text：Trix 允许图片在段落里，而图片在我们的树里
		// 是一等组件。按块边界切开 → 文字归 core.text，图片归 core.image。
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
	}

	// 结构标签：剥壳保内容。内容还在，只是少了这一层 ——
	// 与「整块丢弃」必须分开记，否则运营分不清"东西丢了"和"只是少了一层壳"。
	if structuralTags[n.DataAtom] {
		im.warn(n.Data, ActionUnwrap,
			"结构标签 <"+n.Data+"> 没有对应组件，已去掉这层壳并保留里面的内容")
		return im.blocks(childElements(n))
	}
	// 可执行 / 外部内容：整块丢弃。
	if dropTags[n.DataAtom] {
		im.warn(n.Data, ActionDrop,
			"已丢弃 <"+n.Data+">：脚本、表单控件与外部嵌入不能进静态产物，留着也只是无效标签")
		return nil, nil
	}
	// 其余不认识的标签：降级不静默（决策 5）。
	im.warn(n.Data, ActionUnwrap,
		"不认识的标签 <"+n.Data+"> 已去壳保留文字，请检查这一段格式是否符合预期")
	return im.blocks(childElements(n))
}

// heading h1~h6 → core.heading（层级原样带进 tag，语义不丢）。
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

// list ul/ol → core.list（ul 圆点、ol 序号，导回时可逆）。
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
		// 列表项的 Text 是纯文本字段：项内的加粗/斜体/行内代码拿不回来，要说一声。
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

// blockquote → core.quote。
//
// 直接子级里的 <cite> 单独取成 Author 字段，并从引用正文里排除 ——
// 否则"作者名"会在正文和作者栏各出现一次。
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
		// 只有出处没有正文：当成一句空的引用没意义，但作者信息不该静默消失。
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

// pre → core.text（保留 <pre> 本身：白名单里有它，代码块是正文的一部分而不是独立组件）。
func (im *importer) pre(n *html.Node) ([]*core.Node, error) {
	inner, err := renderInner(n)
	if err != nil {
		return nil, err
	}
	textValue := sanitizeText("<pre>" + inner + "</pre>")
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

// image img → core.image。
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

// figure → core.image（figcaption 进 caption：图片组件的图注就是这个字段）。
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

// divider hr → core.divider。
func (im *importer) divider() ([]*core.Node, error) {
	node, err := newNode(dividerpkg.Type, dividerpkg.Props{})
	if err != nil {
		return nil, err
	}
	return []*core.Node{node}, nil
}

// table → core.table（caption 作标题；单行全 th 视为表头，其余行作数据）。
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
		// 全是 th 的单行就是表头 —— HTML 里两种写法都常见（thead>tr>th 与首行 th）。
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

// inlineTextNode 把一串行内节点合并成一个 core.text 节点（行级格式原样保留在 text 里）。
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
	// 文字段一律带段落容器（<p>）：否则 round-trip 会退化 —— <p>a</p><p>b</p> 导入两次
	// 时第二次拿不到段落容器，两段会并成一段。core.text 的 richtext 本来就是
	// "块级容器序列"形态（Trix 提交的也是 div/p 序列），这里只是把容器补齐。
	if !fragmentHasBlockLevel(textValue) {
		textValue = "<p>" + textValue + "</p>"
	}
	// 按**字节**判长与裁切：core.MaxRichLen 与 core.SanitizeRichHTML 都按字节判，
	// 按 rune 裁会出现「裁到 3 万 rune 仍有 9 万字节 → 构建期整段判空」的静默丢失。
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

// fragmentHasBlockLevel 片段里是否已有块级内容（**递归**：嵌在行内标签里也算）。
//
// 判据是「非行内即块级」，不是一份块级标签清单 —— 清单式判据被 fuzz 连抓两次：
//   - `<em>0<p></em>`：p 嵌在行内里，只查顶层会漏，补出 <p> 套 <p>，清洗器遇到嵌套 p
//     会剥掉内层并错配结束标签，导出交叉嵌套的 `<p><em>0</p></em>`；
//   - `<a><li>0`：li 没进那份清单，同样补错，导出非法的 <a><li>…</li></a>。
//
// 清单永远会漏（HTML 有上百个标签，畸形输入里任何一个都可能出现在行内位置），
// 而「行内白名单」是本包已经维护的封闭集合，取反就是完整答案。
func fragmentHasBlockLevel(fragment string) bool {
	doc, err := html.Parse(strings.NewReader(fragment))
	if err != nil {
		// 解析失败时保守当作「有块级」：不补包裹最多少一层容器，
		// 补错了却会产出非法结构，且非法结构会一路漂进产物。
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

// containsBlockLevel 子树里是否出现行内白名单之外的标签。
//
// 从 body 的直接子节点开始调用：html / head / body 本身不是行内标签，
// 若从文档根开始遍历会立刻命中 html 从而永远判真（那会让补包裹功能静默失效）。
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

// truncateToBytes 按字节上限裁切，并回退到完整 UTF-8 边界（不切碎多字节字符）。
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

// isInline 是否行内内容（会被合并进 core.text）。
//
// img 刻意**不算**行内：一张图一个组件，混进 core.text 的富文本里会让"换图"变成"改 HTML"。
func isInline(n *html.Node) bool {
	if n.Type == html.TextNode {
		return true
	}
	if n.Type != html.ElementNode {
		return false
	}
	return inlineTags[n.DataAtom]
}

// hasInlineMarkup 子树里是否有链接以外的行内格式（列表项只有纯文本字段）。
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

// findDirectChild 找直接子元素（不递归）。
func directChild(n *html.Node, a atom.Atom) *html.Node {
	for _, c := range childElements(n) {
		if c.Type == html.ElementNode && c.DataAtom == a {
			return c
		}
	}
	return nil
}

// renderInnerExcept 渲染子节点为 HTML，跳过 skip 及其子树。
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

// findAll 收集指定标签的所有后代（文档顺序）。
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

// inlineTags 行内标签（合并进 core.text 的富文本字段）。
var inlineTags = map[atom.Atom]bool{
	atom.A: true, atom.Strong: true, atom.B: true, atom.Em: true, atom.I: true,
	atom.S: true, atom.U: true, atom.Del: true, atom.Code: true, atom.Br: true,
	atom.Span: true, atom.Mark: true, atom.Sub: true, atom.Sup: true, atom.Small: true,
}

// markupTags 会在导入时丢失（列表项内）的行级格式标签。
var markupTags = map[atom.Atom]bool{
	atom.Strong: true, atom.B: true, atom.Em: true, atom.I: true,
	atom.S: true, atom.U: true, atom.Del: true, atom.Code: true, atom.Mark: true,
}

// structuralTags 只是分组语义、没有对应组件的结构标签：剥壳保内容。
var structuralTags = map[atom.Atom]bool{
	atom.Div: true, atom.Section: true, atom.Article: true, atom.Aside: true,
	atom.Header: true, atom.Footer: true, atom.Main: true, atom.Nav: true,
	atom.Address: true, atom.Dl: true, atom.Dt: true, atom.Dd: true,
	atom.Center: true, atom.Font: true, atom.Details: true, atom.Summary: true,
}

// dropTags 不能进静态产物的标签：整块丢弃（内容也一起消失，必须警告）。
var dropTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Iframe: true, atom.Object: true,
	atom.Embed: true, atom.Form: true, atom.Input: true, atom.Textarea: true,
	atom.Select: true, atom.Option: true, atom.Button: true, atom.Label: true,
	atom.Meta: true, atom.Link: true, atom.Base: true, atom.Template: true,
	atom.Canvas: true, atom.Video: true, atom.Audio: true, atom.Source: true,
	atom.Track: true, atom.Map: true, atom.Area: true, atom.Svg: true,
	atom.Math: true, atom.Noscript: true,
}
