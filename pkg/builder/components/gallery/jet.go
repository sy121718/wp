package gallery

import (
	"fmt"
	"strconv"
	"strings"

	"go_wp/pkg/builder/core"
	"go_wp/pkg/upload"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Visible bool

	IsCarousel bool

	FirstEager bool

	Items []ItemView

	CarouselAttr string

	Arrows bool

	Dots bool

	SlideLabel string

	DotItems []DotItem

	Loading string

	IsEager bool

	Skeleton bool

	Class string

	PrevLabel string
	NextLabel string

	PrevIcon string
	NextIcon string
}

const (
	TextKeyPrev = "site.component.gallery.prev"

	TextKeyNext = "site.component.gallery.next"

	TextKeySlideLabel = "site.component.gallery.slide_label"
)

type DotItem struct {
	Anchor string

	Label string
}

const (
	textFallbackPrev       = "上一张"
	textFallbackNext       = "下一张"
	textFallbackSlideLabel = "第 %s 张"
)

type ItemView struct {
	AnchorID string

	URL string

	Alt string

	Caption string

	HasFigure bool

	IsLightbox bool

	IsLink bool

	Href string

	Loading string

	FetchPriority string

	IsEager bool

	Skeleton bool

	Class string

	FetchHigh bool
	FetchLow  bool
}

func BuildView(nodeID string, p *Props, content core.ContentResolver, siteLink func(string) string) (View, error) {

	if p.Mode == "" {
		p.Mode = LayoutGrid
	}
	mode := p.Mode

	items, fromAuthor, err := resolveSourceContent(p, content)
	if err != nil {
		return View{}, err
	}
	if items == nil {
		return View{}, nil
	}

	views := make([]ItemView, 0, len(items))
	for i, r := range items {
		iv := buildItemView(p, r, siteLink, fromAuthor)
		if mode == LayoutCarousel {
			iv.AnchorID = "sky-gslide-" + nodeID + "-" + strconv.Itoa(i)
		}
		views = append(views, iv)
	}

	v := View{Visible: true, IsCarousel: mode == LayoutCarousel, Items: views, Loading: p.Loading,
		FirstEager: mode == LayoutCarousel && !p.Carousel.FirstEagerOff}

	if mode == LayoutCarousel && p.Carousel.Dots {
		v.DotItems = make([]DotItem, 0, len(views))
		for i := range views {
			v.DotItems = append(v.DotItems, DotItem{
				Anchor: views[i].AnchorID,
			})
		}
	}
	if mode == LayoutCarousel {
		c := p.Carousel
		interval := c.Interval
		if interval == 0 {
			interval = 4000
		}
		v.CarouselAttr = fmt.Sprintf(`data-carousel='{"autoplay":%t,"interval":%d,"infinite":%t,"pauseOnHover":%t,"slidesPerView":{"desktop":%s,"tablet":%s,"mobile":%s}}'`,
			c.Autoplay, interval, c.Infinite, c.PauseOnHover,
			slideNum(c.SlidesPerView.Desktop, 1), slideNum(c.SlidesPerView.Tablet, 1), slideNum(c.SlidesPerView.Mobile, 1))
		v.Arrows = c.Arrows
		v.Dots = c.Dots
	}
	if v.Arrows {
		if svg, ok := core.IconSVGClass("chevron-left", "gallery-arrow-icon"); ok {
			v.PrevIcon = svg
		}
		if svg, ok := core.IconSVGClass("chevron-right", "gallery-arrow-icon"); ok {
			v.NextIcon = svg
		}
	}
	return v, nil
}

func buildItemView(p *Props, r Item, siteLink func(string) string, linkFromAuthor bool) ItemView {
	href := r.Link
	if href != "" && linkFromAuthor {
		href = core.SiteLinkOrSame(siteLink, href)
	}
	if href == "" && strings.TrimSpace(p.DefaultLink) != "" {

		href = core.SiteLinkOrSame(siteLink, p.DefaultLink)
	}

	iv := ItemView{URL: r.URL, Alt: r.Alt, Caption: r.Caption, Loading: r.Loading, FetchPriority: r.FetchPriority}
	switch {
	case p.ClickAction == ClickLightbox && href == "":

		iv.IsLightbox = true
	case (p.ClickAction == ClickLink || r.Link != "") && href != "":
		iv.IsLink = true
		iv.Href = href
	}

	if (p.CaptionMode == CaptionBelow || p.CaptionMode == CaptionHover) && r.Caption != "" {
		iv.HasFigure = true
	}
	return iv
}

func resolveSourceContent(p *Props, content core.ContentResolver) (items []Item, fromAuthor bool, err error) {
	if p.Binding == nil || p.Binding.Field == "" {
		return p.Items, true, nil
	}
	if content == nil {
		return nil, false, core.ErrContentResolverRequired
	}
	v, err := content.ResolveString(p.Binding.Field)
	if err != nil {
		return nil, false, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
	}
	if v != "" {
		items, err = parseValues(v)
		if err != nil {
			return nil, false, err
		}
		return filterUnsafeItems(items), false, nil
	}
	if p.Binding.Placeholder != "" {
		return filterUnsafeItems([]Item{{URL: p.Binding.Placeholder}}), false, nil
	}
	return nil, false, nil
}

func filterUnsafeItems(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		it.URL = upload.StorageURL(it.URL)
		if it.URL == "" || !core.IsSafeURL(it.URL) {
			continue
		}
		if it.Link != "" && !core.IsSafeURL(it.Link) {
			it.Link = ""
		}
		out = append(out, it)
	}
	return out
}

func (v *View) ApplyImageLoading(d core.ImageDefaults) bool {

	needSkeleton := false
	allEager := true
	for i := range v.Items {
		it := &v.Items[i]
		loading := it.Loading
		if loading == "" && v.FirstEager && i == 0 {

			loading = "off"
		}
		if loading == "" {
			loading = v.Loading
		}
		attrs := core.ResolveImageLoading(loading, d)
		it.IsEager, it.Skeleton = attrs.IsEager, attrs.Skeleton
		it.Class = core.ImageSkeletonClass("", attrs.Skeleton)
		fp := core.ResolveFetchPriority(it.FetchPriority)
		if it.FetchPriority == "" && v.IsCarousel {

			if i == 0 {
				fp = core.ImageFetchPriority{High: true}
			} else {
				fp = core.ImageFetchPriority{Low: true}
			}
		}
		it.FetchHigh, it.FetchLow = fp.High, fp.Low
		if attrs.Skeleton {
			needSkeleton = true
		}
		if !attrs.IsEager {
			allEager = false
		}
	}

	v.IsEager = allEager
	v.Skeleton = needSkeleton
	v.Class = core.ImageSkeletonClass("", needSkeleton)
	return needSkeleton
}

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.PrevLabel, v.NextLabel = textFallbackPrev, textFallbackNext
		v.SlideLabel = textFallbackSlideLabel
		v.fillDotLabels()
		return
	}
	v.PrevLabel = text(TextKeyPrev, textFallbackPrev)
	v.NextLabel = text(TextKeyNext, textFallbackNext)
	v.SlideLabel = text(TextKeySlideLabel, textFallbackSlideLabel)
	v.fillDotLabels()
}

func (v *View) fillDotLabels() {
	for i := range v.DotItems {
		v.DotItems[i].Label = strings.Replace(v.SlideLabel, "%s", strconv.Itoa(i+1), 1)
	}
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	if !v.Visible {
		return nil, nil
	}
	if v.CarouselAttr != "" {
		attrs = append(attrs, "data-carousel")
	}
	for _, item := range v.Items {
		if item.IsLightbox {
			attrs = append(attrs, "data-lightbox")
			break
		}
	}
	return attrs, nil
}
