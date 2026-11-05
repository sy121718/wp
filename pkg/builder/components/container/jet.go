package container

import (
	"html"
	"strings"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Tag string

	Attrs string

	ShapeTop string

	ShapeBottom string

	BgSlides []string

	FeatureAttrs []string
}

func BuildView(node *core.Node, p *Props) View {
	var attrs strings.Builder

	if p.StyleEx.GroupParent {
		attrs.WriteString(` data-sky-group="true"`)
	}

	for _, kv := range p.StyleEx.Attributes {
		attrs.WriteString(" ")
		attrs.WriteString(kv.Key)
		attrs.WriteString(`="`)
		attrs.WriteString(html.EscapeString(kv.Value))
		attrs.WriteString(`"`)
	}

	if p.Position.Type == "drawer" {
		attrs.WriteString(` id="sky-drawer-`)
		attrs.WriteString(node.ID)
		attrs.WriteString(`" data-drawer-side="`)
		attrs.WriteString(p.Position.DrawerSide)
		attrs.WriteString(`"`)
		if p.Position.DrawerOverlay {
			attrs.WriteString(` data-drawer-overlay="true"`)
		}
	}

	v := View{Tag: p.Tag, Attrs: attrs.String(), BgSlides: p.Visual.BgSlides}

	if p.StyleEx.GroupParent {
		v.FeatureAttrs = append(v.FeatureAttrs, "data-sky-group")
	}
	v.FeatureAttrs = append(v.FeatureAttrs, featureAttrKeys(p.StyleEx.Attributes)...)
	if p.Position.Type == "drawer" {
		v.FeatureAttrs = append(v.FeatureAttrs, "data-drawer-side")
		if p.Position.DrawerOverlay {
			v.FeatureAttrs = append(v.FeatureAttrs, "data-drawer-overlay")
		}
	}
	if p.StyleEx.ShapeDivider != "" {
		svg := shapeDividers[p.StyleEx.ShapeDivider]
		if p.StyleEx.ShapeDividerPosition == "top" {
			v.ShapeTop = svg
		} else {
			v.ShapeBottom = svg
		}
	}
	return v
}

func featureAttrKeys(kvs []AttributeKV) []string {
	var out []string
	for _, kv := range kvs {
		key := strings.ToLower(strings.TrimSpace(kv.Key))
		if strings.HasPrefix(key, "data-") || strings.HasPrefix(key, "hx-") {
			out = append(out, key)
		}
	}
	return out
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	return v.FeatureAttrs, nil
}
