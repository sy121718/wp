package breadcrumb

import (
	json "go_wp/pkg/jsonx"
	"net/url"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/seo"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type ItemView struct {
	Label string

	URL string

	Current bool

	Separator string
}

type View struct {
	Items []ItemView

	Separator string

	Visible bool

	Label string

	JSONLD string

	homeIndex int

	homeFromDefault bool

	jsonLDEnabled bool
}

const (
	TextKeyLabel = "site.component.breadcrumb.label"

	TextKeyHome = "site.breadcrumb.home"
)

const (
	textFallbackLabel = "面包屑导航"
	textFallbackHome  = "首页"
)

func BuildView(p *Props, ctx *core.RenderContext) View {
	sep := strings.TrimSpace(p.Separator)
	if sep == "" {
		sep = defaultSeparator
	}
	v := View{
		Separator:       sep,
		Label:           textFallbackLabel,
		homeIndex:       -1,
		homeFromDefault: strings.TrimSpace(p.HomeLabel) == "",
		jsonLDEnabled:   p.JSONLD,
	}
	items := p.Items
	derived := len(items) == 0
	if derived {
		path := ""
		if ctx != nil {
			path = ctx.CurrentPath
		}
		items = deriveItems(path, homeOf(p))
		if len(items) > 0 {
			v.homeIndex = 0
		}
	}
	if p.HideHome && len(items) > 0 {
		items = items[1:]

		v.homeIndex = -1
	}

	if derived && !p.HideHome && len(items) < 2 {
		items = nil
		v.homeIndex = -1
	}

	siteLink := (func(string) string)(nil)
	if !derived {
		siteLink = ctx.ResolveSiteLink
	}
	v.Items = itemViews(items, sep, siteLink)
	v.Visible = len(v.Items) > 0
	return v
}

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.Label = textFallbackLabel
	} else {
		v.Label = text(TextKeyLabel, textFallbackLabel)
	}
	if v.homeFromDefault && v.homeIndex >= 0 && v.homeIndex < len(v.Items) {
		home := textFallbackHome
		if text != nil {
			if t := text(TextKeyHome, textFallbackHome); t != "" {
				home = t
			}
		}
		v.Items[v.homeIndex].Label = home
	}
	v.JSONLD = ""
	if v.jsonLDEnabled {
		v.JSONLD = jsonLDScript(v.Items)
	}
}

func homeOf(p *Props) string {
	if h := strings.TrimSpace(p.HomeLabel); h != "" {
		return h
	}
	return textFallbackHome
}

func deriveItems(rawPath, homeLabel string) []Item {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return nil
	}
	u, err := url.Parse(rawPath)
	if err != nil || u.Opaque != "" {
		return nil
	}
	items := []Item{{Label: homeLabel, URL: "/"}}
	cur := ""
	for _, seg := range strings.Split(strings.TrimPrefix(u.EscapedPath(), "/"), "/") {
		if seg == "" {
			continue
		}
		cur += "/" + seg
		name, uerr := url.PathUnescape(seg)
		if uerr != nil {
			name = seg
		}
		items = append(items, Item{Label: name, URL: cur})
	}
	return items
}

func itemViews(items []Item, sep string, siteLink func(string) string) []ItemView {
	out := make([]ItemView, 0, len(items))
	for i, it := range items {
		raw := strings.TrimSpace(it.URL)
		if raw != "" && siteLink != nil {
			raw = siteLink(raw)
		} else if raw != "" {

			raw = seo.AbsoluteSiteURL(raw)
		}
		iv := ItemView{
			Label:   strings.TrimSpace(it.Label),
			URL:     raw,
			Current: i == len(items)-1,
		}
		if i < len(items)-1 {
			iv.Separator = sep
		}
		out = append(out, iv)
	}
	return out
}

type ldListItem struct {
	Type     string `json:"@type"`
	Position int    `json:"position"`
	Name     string `json:"name"`
	Item     string `json:"item,omitempty"`
}

type ldDoc struct {
	Context         string       `json:"@context"`
	Type            string       `json:"@type"`
	ItemListElement []ldListItem `json:"itemListElement"`
}

func jsonLDScript(items []ItemView) string {
	if len(items) == 0 {
		return ""
	}
	doc := ldDoc{Context: "https://schema.org", Type: "BreadcrumbList"}
	for i, it := range items {
		doc.ItemListElement = append(doc.ItemListElement, ldListItem{
			Type:     "ListItem",
			Position: i + 1,
			Name:     it.Label,
			Item:     it.URL,
		})
	}
	b, err := json.Marshal(doc)
	if err != nil {

		return ""
	}
	return "<script type=\"application/ld+json\">" + string(b) + "</script>"
}
