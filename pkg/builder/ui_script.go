package builder

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

type uiBlock struct {
	file  string
	attrs []string

	prefixes []string

	noStyle bool
}

var uiBaseClasses = []string{

	"btn", "btn-primary", "btn-secondary", "btn-ghost", "btn-danger", "btn-sm", "btn-icon",

	"wb-btn", "wb-btn-primary", "wb-btn-secondary", "wb-btn-ghost", "wb-icon-btn",

	"form-input", "form-select", "form-textarea", "form-label", "form-hint", "form-error",
	"form-group", "checkbox",

	"wbs",

	"form-inline", "form-row",

	"card", "card-header", "card-title", "card-body",

	"table-wrap", "data-table", "data-table-wide",

	"badge", "badge-success", "badge-warning", "badge-danger", "badge-info", "badge-mute",
	"dot",
}

var uiBlocks = []uiBlock{
	{file: "htmx.min.js", attrs: []string{"data-write"}, prefixes: []string{"hx-"}, noStyle: true},
	{file: "site-preview.js", attrs: []string{"data-site-read", "data-write", "data-component-instance"}, prefixes: []string{"hx-"}, noStyle: true},
	{file: "site-actions.js", attrs: []string{"data-component-action"}, noStyle: true},
	{file: "site-data.js", attrs: []string{"data-site-read"}, noStyle: true},
	{file: "site-catalog.js", attrs: []string{"data-site-catalog", "data-site-search"}, noStyle: true},
	{file: "site-product.js", attrs: []string{"data-site-product"}, noStyle: true},
	{file: "write-base.js", attrs: []string{"data-write"}, prefixes: []string{"hx-"}, noStyle: true},
	{file: "htmx-bridge.js", attrs: []string{"data-write", "data-drawer", "data-modal"}, prefixes: []string{"hx-"}, noStyle: true},
	{file: "requires_js.js", attrs: []string{"data-requires-js"}, noStyle: true},
	{file: "select.js", attrs: []string{"data-ui-select"}},
	{file: "modal.js", attrs: []string{"data-modal", "data-modal-open", "data-modal-close"}},
	{file: "drawer.js", attrs: []string{"data-drawer-open", "data-drawer-close", "data-drawer", "data-drawer-mask"}},
	{file: "confirm.js", attrs: []string{"data-confirm"}},
	{file: "colorfield.js", attrs: []string{"data-color-field"}},
	{file: "iconfield.js", attrs: []string{"data-icon-field", "data-icon-name"}},
	{file: "themetoggle.js", attrs: []string{"data-theme-toggle"}},
}

func UIAssetFiles() []string {
	files := make([]string, 0, len(uiBlocks)+2)
	files = append(files, "_util.js")
	for _, block := range uiBlocks {
		files = append(files, block.file)
	}
	return append(files, "index.js")
}

type htmlFeatures map[string]struct{}

type htmlScan struct {
	attrs   htmlFeatures
	classes htmlFeatures

	frags htmlFeatures
}

func (c *CompiledPage) featureScan() htmlScan {
	if c.Features != nil {
		attrs := htmlFeatures(c.Features.Attrs())
		scan := htmlScan{
			attrs:   attrs,
			classes: htmlFeatures(c.Features.Classes()),
		}

		if hasHXAttr(attrs) || strings.Contains(c.HTML, fragmentPathPrefix) {
			scan.frags = fragmentCapsFromHTML(c.HTML)
		}
		return scan
	}
	return collectHTMLScan(c.HTML)
}

func collectHTMLFeatures(content string) htmlFeatures {
	return collectHTMLScan(content).attrs
}

func collectHTMLScan(content string) htmlScan {
	out := htmlScan{attrs: make(htmlFeatures), classes: make(htmlFeatures), frags: make(htmlFeatures)}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		_, more := z.TagName()
		for more {
			var key, val []byte
			key, val, more = z.TagAttr()
			name := strings.ToLower(string(key))
			if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "hx-") {
				out.attrs[name] = struct{}{}
			}

			if strings.HasPrefix(name, "hx-") {
				for _, cap := range fragmentCapsIn(string(val)) {
					out.frags[cap] = struct{}{}
				}
			}
			if name == "class" {
				for _, token := range strings.Fields(string(val)) {
					if c := strings.ToLower(strings.TrimSpace(token)); c != "" {
						out.classes[c] = struct{}{}
					}
				}
			}
		}
	}
	return out
}

func relevantAttrs(attrs htmlFeatures) htmlFeatures {
	out := make(htmlFeatures)
	for name := range attrs {
		single := htmlFeatures{name: struct{}{}}
		if relevantAttr(single) {
			out[name] = struct{}{}
		}
	}
	return out
}

func relevantAttr(single htmlFeatures) bool {
	for _, block := range uiBlocks {
		if block.hit(single) {
			return true
		}
	}
	for _, block := range allEnhanceBlocks() {
		for _, feat := range block.feats {
			if _, ok := single[feat]; ok {
				return true
			}
		}
	}
	return false
}

func hasUIBaseClass(classes htmlFeatures) bool {
	for _, c := range uiBaseClasses {
		if _, ok := classes[c]; ok {
			return true
		}
	}
	return false
}

func (b uiBlock) hit(attrs htmlFeatures) bool {
	for _, attr := range b.attrs {
		if _, ok := attrs[attr]; ok {
			return true
		}
	}
	for _, prefix := range b.prefixes {
		for name := range attrs {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

func usedUIFiles(attrs htmlFeatures) (files []string, needBase bool) {
	for _, block := range uiBlocks {
		if !block.hit(attrs) {
			continue
		}
		files = append(files, block.file)
		if !block.noStyle {
			needBase = true
		}
	}
	return files, needBase
}

func uiAssetsFor(attrs htmlFeatures, css string, sources map[string]string) (string, string, error) {
	return uiAssetsForScan(htmlScan{attrs: attrs}, css, sources)
}

func uiAssetsForScan(scan htmlScan, css string, sources map[string]string) (string, string, error) {
	files, needStyleFromAttrs := usedUIFiles(scan.attrs)
	needStyle := needStyleFromAttrs || hasUIBaseClass(scan.classes)

	fragCSS := fragmentBaseCSSFor(scan.frags)
	if _, ok := scan.attrs["data-site-cart"]; ok {
		fragCSS = joinCSS(fragCSS, cartFragmentBaseCSS())
	}
	if len(files) == 0 && !needStyle && fragCSS == "" {
		return "", "", nil
	}

	baseCSS, err := uiCSSFor(pickCSS(needStyle, css), scan)
	if err != nil {
		return "", "", fmt.Errorf("切分控件基座样式失败: %w", err)
	}
	if sources == nil {
		return joinCSS(baseCSS, fragCSS), "", nil
	}
	if len(files) > 0 {
		if needStyleFromAttrs {

			files = append(append([]string{"_util.js"}, files...), "index.js")
		}
	}
	parts := make([]string, 0, len(files))
	for _, file := range files {
		src := sources[file]
		if strings.TrimSpace(src) == "" {
			return "", "", fmt.Errorf("控件资源缺失: %s", file)
		}
		parts = append(parts, src)
	}
	if needStyle && strings.TrimSpace(css) == "" {
		return "", "", fmt.Errorf("控件资源缺失: ui.css")
	}
	return joinCSS(baseCSS, fragCSS), strings.Join(parts, "\n"), nil
}

func pickCSS(need bool, css string) string {
	if need {
		return css
	}
	return ""
}

func joinCSS(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	}
	return first + "\n\n" + second
}
