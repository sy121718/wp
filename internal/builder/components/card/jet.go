package card

import (
	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	HasImage bool

	ImageSrc string

	TitleTag string

	Title string

	Text string

	HasButton bool

	Label string

	ButtonLink string

	Loading string

	IsEager bool

	Skeleton bool

	Class string

	FetchPriority string

	FetchHigh bool
	FetchLow  bool
}

func BuildView(p *Props, siteLink func(string) string) View {

	titleTag := p.TitleTag
	switch titleTag {
	case "h2", "h3", "h4", "h5":
	default:
		titleTag = "h3"
	}
	return View{
		TitleTag:  titleTag,
		HasImage:  p.ImageSrc != "",
		ImageSrc:  p.ImageSrc,
		Title:     p.Title,
		Text:      core.RichTextHTML(p.Text),
		HasButton: p.Label != "" && p.ButtonLink != "",
		Label:     p.Label,

		ButtonLink:    core.SiteLinkOrSame(siteLink, p.ButtonLink),
		Loading:       p.Loading,
		FetchPriority: p.FetchPriority,
	}
}

func (v *View) ApplyImageLoading(d core.ImageDefaults) bool {
	attrs := core.ResolveImageLoading(v.Loading, d)
	v.IsEager, v.Skeleton = attrs.IsEager, attrs.Skeleton
	v.Class = core.ImageSkeletonClass(v.Class, attrs.Skeleton)
	fp := core.ResolveFetchPriority(v.FetchPriority)
	v.FetchHigh, v.FetchLow = fp.High, fp.Low
	return attrs.Skeleton
}
