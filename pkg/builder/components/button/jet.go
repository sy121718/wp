package button

import (
	"fmt"
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

	Text string

	IconPrefix string
	IconSuffix string

	Loading string

	IsEager bool

	Skeleton bool

	Class string

	FetchPriority string

	FetchHigh bool
	FetchLow  bool

	FeatureAttrs []string
}

func BuildView(p *Props, content core.ContentResolver, siteLink func(string) string) (View, error) {
	fragment, err := buildIconFragment(p)
	if err != nil {
		return View{}, err
	}
	tag, attrs, err := buildAttrs(p, content, siteLink)
	if err != nil {
		return View{}, err
	}
	v := View{Tag: tag, Attrs: attrs, Text: p.Text, Loading: p.Loading, FetchPriority: p.FetchPriority}

	if p.Action == ActionModal {
		v.FeatureAttrs = append(v.FeatureAttrs, "data-modal-open")
	}
	if p.Icon != nil {

		switch p.Icon.Position {
		case "suffix", "bottom":
			v.IconSuffix = fragment
		default:
			v.IconPrefix = fragment
		}
	}
	return v, nil
}

func (v *View) ApplyImageLoading(d core.ImageDefaults) bool {
	attrs := core.ResolveImageLoading(v.Loading, d)
	v.IsEager, v.Skeleton = attrs.IsEager, attrs.Skeleton

	v.Class = core.ImageSkeletonClass("", attrs.Skeleton)
	fp := core.ResolveFetchPriority(v.FetchPriority)
	v.FetchHigh, v.FetchLow = fp.High, fp.Low
	return attrs.Skeleton
}

func buildIconFragment(p *Props) (string, error) {
	if p.Icon == nil {
		return "", nil
	}
	if p.Icon.Source == "builtin" {
		inner, ok := core.IconInner(p.Icon.Name)
		if !ok {
			return "", fmt.Errorf("无效的内置图标: %q", p.Icon.Name)
		}
		return inner, nil
	}

	return html.EscapeString(p.Icon.URL), nil
}

func buildAttrs(p *Props, content core.ContentResolver, siteLink func(string) string) (tag, attrs string, err error) {
	if siteLink == nil {
		siteLink = func(s string) string { return s }
	}
	tag = "a"
	switch p.Action {
	case ActionModal:
		tag = "button"
		attrs = ` type="button" data-modal-open="` + html.EscapeString(p.Value) + `"`
	case ActionLink:
		if content == nil {
			return "", "", fmt.Errorf("%w，无法解析动态链接", core.ErrContentResolverRequired)
		}

		if p.Binding == nil || p.Binding.Field == "" {
			return "", "", fmt.Errorf("动态链接动作缺少绑定字段（binding.field），请配置绑定或改用其他动作")
		}
		v, e := content.ResolveString(p.Binding.Field)
		if e != nil {
			return "", "", fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, e)
		}
		if v == "" {
			return "", "", fmt.Errorf("绑定字段 %q 为空，无 fallback 兜底", p.Binding.Field)
		}

		if !core.IsSafeURL(v) {
			return tag, "", nil
		}
		attrs = ` href="` + html.EscapeString(v) + `"`
	case ActionAnchor:
		attrs = ` href="#` + html.EscapeString(p.Value) + `"`
	case ActionNative, ActionInternal:
		attrs = ` href="` + html.EscapeString(siteLink(p.Value)) + `"`
	default:
		attrs = ` href="` + html.EscapeString(p.Value) + `"`
		relParts := []string{}
		if p.Target == "blank" {
			attrs += ` target="_blank"`
			relParts = append(relParts, "noopener", "noreferrer")
		}
		if p.Rel == "nofollow" || p.Rel == "sponsored" {
			relParts = append(relParts, p.Rel)
		}
		if len(relParts) > 0 {
			attrs += ` rel="` + strings.Join(relParts, " ") + `"`
		}
	}
	return tag, attrs, nil
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	return v.FeatureAttrs, nil
}
