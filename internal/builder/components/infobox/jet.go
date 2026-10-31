package infobox

import (
	"strings"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	HasLink bool

	HasBtn bool

	Link string

	Label string

	HasMedia bool

	MediaSrc string

	MediaAlt string

	HasIcon bool

	IconSVG string

	Subtitle string

	Title string

	TitleTag string

	Text string

	Loading string

	IsEager bool

	Skeleton bool

	Class string

	FetchPriority string

	FetchHigh bool
	FetchLow  bool
}

func BuildView(p *Props, siteLink func(string) string) View {
	v := View{}

	if p.MediaImage != "" {
		v.HasMedia = true
		v.MediaSrc = p.MediaImage
		v.MediaAlt = p.MediaAlt
	} else if p.Icon != "" {
		v.HasIcon = true
		if svg, ok := core.IconSVG(p.Icon); ok {
			v.IconSVG = svg
		}
	}
	v.Subtitle = p.Subtitle
	v.Title = p.Title
	v.TitleTag = p.TitleTag
	if v.TitleTag == "" {
		v.TitleTag = "h3"
	}
	v.Text = core.RichTextHTML(p.Text)
	v.Loading = p.Loading
	v.FetchPriority = p.FetchPriority

	if strings.TrimSpace(p.Link) != "" {
		v.HasLink = true
		v.Link = core.SiteLinkOrSame(siteLink, p.Link)
		if strings.TrimSpace(p.Label) != "" {
			v.HasBtn = true
			v.Label = p.Label
		}
	}
	return v
}

func (v *View) ApplyImageLoading(d core.ImageDefaults) bool {
	attrs := core.ResolveImageLoading(v.Loading, d)
	v.IsEager, v.Skeleton = attrs.IsEager, attrs.Skeleton
	v.Class = core.ImageSkeletonClass(v.Class, attrs.Skeleton)
	fp := core.ResolveFetchPriority(v.FetchPriority)
	v.FetchHigh, v.FetchLow = fp.High, fp.Low
	return attrs.Skeleton
}
