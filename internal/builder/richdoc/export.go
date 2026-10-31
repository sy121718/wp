package richdoc

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	stdhtml "html"
	"strings"

	accordionpkg "go_wp/internal/builder/components/accordion"
	dividerpkg "go_wp/internal/builder/components/divider"
	headingpkg "go_wp/internal/builder/components/heading"
	imagepkg "go_wp/internal/builder/components/image"
	listpkg "go_wp/internal/builder/components/list"
	quotepkg "go_wp/internal/builder/components/quote"
	tablepkg "go_wp/internal/builder/components/table"
	textpkg "go_wp/internal/builder/components/text"
	"go_wp/internal/builder/core"
)

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
	case accordionpkg.Type:
		return exp.writeAccordion(sb, n)
	}
	return exp.writePlaceholder(sb, n)
}

func (exp *exporter) writeAccordion(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[accordionpkg.Props](n)
	if err != nil {
		return err
	}
	if len(props.Items) == 0 || len(props.Items) != len(n.Children) {
		exp.warn(accordionpkg.Type, ActionPlaceholder, fmt.Sprintf(
			"折叠项标题数（%d）与内容数（%d）不一致，无法一一对应，已导出为占位文字；请在画布上补齐折叠项后再导出",
			len(props.Items), len(n.Children)))
		sb.WriteString(placeholderHTML(accordionpkg.Type))
		return nil
	}
	for i, item := range props.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {

			exp.warn(accordionpkg.Type, ActionTrim, fmt.Sprintf(
				"第 %d 个折叠项没有标题，`<summary>` 为空，导回画布时这一项会被丢弃", i+1))
		}
		sb.WriteString("<details><summary>" + stdhtml.EscapeString(title) + "</summary>")
		var inner strings.Builder
		if err := exp.writeNodes(&inner, n.Children[i:i+1]); err != nil {
			return err
		}
		sb.WriteString(inner.String())
		sb.WriteString("</details>")
	}

	if props.OneOpen {
		exp.warn(accordionpkg.Type, ActionTrim,
			"「同时只开一个」是组件行为，富文本里没有对应属性，导回画布后每个折叠块各自独立")
	}
	for _, item := range props.Items {
		if item.Open {
			exp.warn(accordionpkg.Type, ActionTrim,
				"「默认展开」是组件状态，富文本不保留 details 的 open 属性（白名单刻意剥掉），导回画布后全部收起")
			break
		}
	}
	return nil
}

func (exp *exporter) writeHeading(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[headingpkg.Props](n)
	if err != nil {
		return err
	}
	tag := strings.ToLower(strings.TrimSpace(props.Tag))
	if !isHeadingTag(tag) {

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

func (exp *exporter) writeText(sb *strings.Builder, n *core.Node) error {
	props, err := decodeProps[textpkg.Props](n)
	if err != nil {
		return err
	}
	if strings.TrimSpace(props.Mode) == textpkg.ModePlainText {
		sb.WriteString("<p>" + stdhtml.EscapeString(props.Text) + "</p>")
		return nil
	}

	sb.WriteString(sanitizeText(props.Text))
	return nil
}

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

func (exp *exporter) writePlaceholder(sb *strings.Builder, n *core.Node) error {
	name := strings.TrimSpace(n.Type)
	if name == "" {
		name = "未知组件"
	}
	exp.warn(name, ActionPlaceholder,
		"该组件在富文本里没有等价物，已导出为占位文字；在富文本里编辑后无法还原为组件，要改它请回画布")
	sb.WriteString(placeholderHTML(name))
	return nil
}

func placeholderHTML(name string) string {
	if strings.TrimSpace(name) == "" {
		name = "未知组件"
	}
	return "<p>［组件：" + stdhtml.EscapeString(name) + "｜该组件不能在富文本中编辑］</p>"
}

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

func isHeadingTag(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}

func fallbackTag(tag string) string {
	if strings.TrimSpace(tag) == "" {
		return "（默认 h2）"
	}
	return tag
}
