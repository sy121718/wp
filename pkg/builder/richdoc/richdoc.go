package richdoc

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/richtext"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"go_wp/pkg/builder/core"
)

type WarningAction string

const (
	ActionUnwrap WarningAction = "unwrap"

	ActionDrop WarningAction = "drop"

	ActionTrim WarningAction = "trim"

	ActionPlaceholder WarningAction = "placeholder"
)

type Warning struct {
	Tag string

	Action WarningAction

	Detail string
}

func (w Warning) String() string {
	return fmt.Sprintf("%s[%s]: %s", w.Tag, w.Action, w.Detail)
}

type Result struct {
	Nodes []*core.Node

	Warnings []Warning
}

type ExportResult struct {
	HTML string

	Warnings []Warning

	Lossless bool
}

const maxTextLen = richtext.MaxRichLen

func newNode(componentType string, props any) (*core.Node, error) {
	raw, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("组件 %s 的属性序列化失败: %w", componentType, err)
	}
	return &core.Node{ID: newID(), Type: componentType, Props: raw}, nil
}

func sanitizeText(src string) string {
	return strings.TrimSpace(richtext.SanitizeRichHTML(strings.TrimSpace(src)))
}

func hasVisibleContent(htmlFragment string) bool {
	if strings.TrimSpace(richtext.StripRichTags(htmlFragment)) != "" {
		return true
	}

	return strings.Contains(strings.ToLower(htmlFragment), "<img")
}

func renderInner(n *html.Node) (string, error) {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&sb, c); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

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

func isBlankText(n *html.Node) bool {
	return n.Type == html.TextNode && strings.TrimSpace(n.Data) == ""
}

func newID() string { return uuid.NewString() }

func attrOf(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}
