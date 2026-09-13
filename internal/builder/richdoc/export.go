package richdoc

// export.go — 组件树 → 富文本 HTML（决策 5 的导出方向）。
//
// 与 import.go 对称：只处理**可逆子集**（heading / text / list / quote / image /
// divider / table），其余组件输出占位文字并记 Lossless=false —— 富文本视图装不下
// 组件树里的全部信息，这一点必须让调用方知道（有损的导出拿去覆盖原文就会真的丢东西）。

import (
	"encoding/json"
	stdhtml "html"
	"strings"

	dividerpkg "go_wp/internal/builder/components/divider"
	headingpkg "go_wp/internal/builder/components/heading"
	imagepkg "go_wp/internal/builder/components/image"
	listpkg "go_wp/internal/builder/components/list"
	quotepkg "go_wp/internal/builder/components/quote"
	tablepkg "go_wp/internal/builder/components/table"
	textpkg "go_wp/internal/builder/components/text"
	"go_wp/internal/builder/core"
)

// NodesToHTML 把组件树导出成富文本 HTML。
//
// Lossless=false 表示这次导出有信息损失（不可逆组件、语义标签无等价物、列表图标样式等），
// 每一处损失都在 Warnings 里有对应的条目 —— 调用方据此决定"只是展示"还是"可以回写原文"。
func NodesToHTML(nodes []*core.Node) (*ExportResult, error) {
	exp := &exporter{lossless: true}
	var sb strings.Builder
	if err := exp.writeNodes(&sb, nodes); err != nil {
		return nil, err
	}
	return &ExportResult{
		HTML:     strings.TrimSpace(sb.String()),
		Warnings: exp.warnings,
		Lossless: exp.lossless,
	}, nil
}

// exporter 一次导出的上下文。
type exporter struct {
	lossless bool
	warnings []Warning
}

func (exp *exporter) warn(tag string, action WarningAction, detail string) {
	exp.warnings = append(exp.warnings, Warning{Tag: tag, Action: action, Detail: detail})
	if action == ActionPlaceholder || action == ActionDrop || action == ActionTrim || action == ActionUnwrap {
		exp.lossless = false
	}
}

// writeNodes 写一组节点。
func (exp *exporter) writeNodes(sb *strings.Builder, nodes []*core.Node) error {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if err := exp.writeNode(sb, n); err != nil {
			return err
		}
	}
	return nil
}

// writeNode 写单个节点（按组件类型分派，与 import.go 的 switch 一一对应）。
func (exp *exporter) writeNode(sb *strings.Builder, n *core.Node) error {
	switch n.Type {
	case headingpkg.Type:
		return exp.writeHeading(sb, n)
	case textpkg.Type:
		return exp.writeText(sb, n)
	case listpkg.Type:
		return exp.writeList(sb, n)
	case quotepkg.Type:
		return exp.writeQuote(sb, n)
	case imagepkg.Type:
		return exp.writeImage(sb, n)
	case dividerpkg.Type:
		sb.WriteString("<hr>")
		return nil
	case tablepkg.Type:
		return exp.writeTable(sb, n)
	}
	return exp.writePlaceholder(sb, n)
}

// writeHeading 标题：h1~h6 原样，其余语义标签（div/span）在富文本里没有等价物。
func (exp *exporter) writeHeading(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[headingpkg.Props](n)
	if err != nil {
		return err
	}
	tag := strings.ToLower(strings.TrimSpace(props.Tag))
	if !isHeadingTag(tag) {
		// 标题组件用 div/span 当语义标签时，富文本里没有对应形态：
		// 按段落导出（导回来会变成一个 text 组件），并说明这次降级。
		exp.warn(headingpkg.Type, ActionTrim,
			"标题组件的语义标签是 "+fallbackTag(tag)+"，富文本里没有等价物，已按普通段落导出（导回画布会变成正文组件）")
		tag = "p"
	}
	if tag == "p" {
		sb.WriteString("<p>" + stdhtml.EscapeString(props.Text) + "</p>")
		return nil
	}
	sb.WriteString("<" + tag + ">" + stdhtml.EscapeString(props.Text) + "</" + tag + ">")
	return nil
}

// writeText 正文：richtext 直出（内容已过白名单），plaintext 包一层 <p>。
func (exp *exporter) writeText(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[textpkg.Props](n)
	if err != nil {
		return err
	}
	if strings.TrimSpace(props.Mode) == textpkg.ModePlainText {
		sb.WriteString("<p>" + stdhtml.EscapeString(props.Text) + "</p>")
		return nil
	}
	// 再过一次白名单：组件树里的 richtext 可能来自旧数据或直接构造（不走导入器），
	// 导出成 HTML 前必须保证它能安全地交给 Trix。
	sb.WriteString(sanitizeText(props.Text))
	return nil
}

// writeList 列表：序号样式 → ol，其余 → ul。
func (exp *exporter) writeList(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[listpkg.Props](n)
	if err != nil {
		return err
	}
	tag := "ul"
	switch props.Style {
	case listpkg.StyleNumber:
		tag = "ol"
	case listpkg.StyleIcon:
		exp.warn(listpkg.Type, ActionTrim,
			"列表是图标样式，富文本里只有圆点与序号两种形态，导回后会是圆点列表")
	}
	sb.WriteString("<" + tag + ">")
	for _, item := range props.Items {
		sb.WriteString("<li>")
		if strings.TrimSpace(item.Link) != "" {
			sb.WriteString(`<a href="` + stdhtml.EscapeString(item.Link) + `">`)
		}
		sb.WriteString(stdhtml.EscapeString(item.Text))
		if strings.TrimSpace(item.Link) != "" {
			sb.WriteString("</a>")
		}
		sb.WriteString("</li>")
	}
	sb.WriteString("</" + tag + ">")
	return nil
}

// writeQuote 引用：作者写进 <cite>（导入方向会把直接子级 cite 取回 Author 字段，闭环）。
func (exp *exporter) writeQuote(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[quotepkg.Props](n)
	if err != nil {
		return err
	}
	sb.WriteString("<blockquote>")
	sb.WriteString(sanitizeText(props.Text))
	if author := strings.TrimSpace(props.Author); author != "" {
		sb.WriteString("<cite>" + stdhtml.EscapeString(author) + "</cite>")
	}
	sb.WriteString("</blockquote>")
	return nil
}

// writeImage 图片：有图注 → figure + figcaption，否则裸 <img>。
func (exp *exporter) writeImage(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[imagepkg.Props](n)
	if err != nil {
		return err
	}
	src := strings.TrimSpace(props.Src)
	if src == "" {
		exp.warn(imagepkg.Type, ActionDrop, "图片没有地址（src 为空），已跳过该图")
		return nil
	}
	img := `<img src="` + stdhtml.EscapeString(src) + `"`
	if alt := strings.TrimSpace(props.Alt); alt != "" {
		img += ` alt="` + stdhtml.EscapeString(alt) + `"`
	}
	if title := strings.TrimSpace(props.Title); title != "" {
		img += ` title="` + stdhtml.EscapeString(title) + `"`
	}
	img += ">"
	if caption := strings.TrimSpace(props.Caption); caption != "" {
		sb.WriteString("<figure>" + img + "<figcaption>" + stdhtml.EscapeString(caption) + "</figcaption></figure>")
		return nil
	}
	sb.WriteString(img)
	return nil
}

// writeTable 表格：表头进 thead，数据行进 tbody。
func (exp *exporter) writeTable(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[tablepkg.Props](n)
	if err != nil {
		return err
	}
	sb.WriteString("<table>")
	if strings.TrimSpace(props.Caption) != "" {
		sb.WriteString("<caption>" + stdhtml.EscapeString(props.Caption) + "</caption>")
	}
	if len(props.Headers) > 0 {
		sb.WriteString("<thead><tr>")
		for _, h := range props.Headers {
			sb.WriteString("<th>" + stdhtml.EscapeString(h) + "</th>")
		}
		sb.WriteString("</tr></thead>")
	}
	if len(props.Rows) > 0 {
		sb.WriteString("<tbody>")
		for _, row := range props.Rows {
			sb.WriteString("<tr>")
			for _, cell := range row {
				sb.WriteString("<td>" + stdhtml.EscapeString(cell) + "</td>")
			}
			sb.WriteString("</tr>")
		}
		sb.WriteString("</tbody>")
	}
	sb.WriteString("</table>")
	return nil
}

// writePlaceholder 不可逆组件的占位。
//
// 占位是**给人在富文本里看的**，不是可还原标记：Trix 只保留自己认识的标签与属性，
// 任何 data-* 还原标记在编辑器里过一手就没了。所以这里不假装无损，
// 而是明确写出"这个位置原本是什么"，并让导出结果带 Lossless=false。
func (exp *exporter) writePlaceholder(sb *strings.Builder, n *core.Node) error {
	name := strings.TrimSpace(n.Type)
	if name == "" {
		name = "未知组件"
	}
	exp.warn(name, ActionPlaceholder,
		"该组件在富文本里没有等价物，已导出为占位文字；在富文本里编辑后无法还原为组件，要改它请回画布")
	sb.WriteString("<p>［组件：" + stdhtml.EscapeString(name) + "｜该组件不能在富文本中编辑］</p>")
	return nil
}

// decodeProps 解出组件属性（props 为空时给零值：组件按缺省值渲染）。
func decodeProps[T any](n *core.Node) (T, error) {
	var props T
	if n == nil || len(n.Props) == 0 {
		return props, nil
	}
	if err := json.Unmarshal(n.Props, &props); err != nil {
		return props, err
	}
	return props, nil
}

// isHeadingTag 是否 h1~h6。
func isHeadingTag(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}

// fallbackTag 空标签在提示里显示成"（默认）"，免得写出「语义标签是 ，富文本里…」这种句子。
func fallbackTag(tag string) string {
	if strings.TrimSpace(tag) == "" {
		return "（默认 h2）"
	}
	return tag
}
