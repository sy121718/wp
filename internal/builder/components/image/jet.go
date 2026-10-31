package image

import (
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
	mediacontract "go_wp/internal/module/media/contract"
	"go_wp/pkg/upload"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Src string

	Alt string

	Title string

	Class string

	IsEager bool

	Skeleton bool

	Srcset string

	Sizes string

	FetchHigh bool
	FetchLow  bool

	IsLightbox bool

	CloseIcon string

	IsLink bool

	Link string

	TargetBlank bool

	RelNofollow bool

	LinkID string

	ImgID string

	NodeID string

	Caption string
}

func BuildView(node *core.Node, p *Props, class, customID string, content core.ContentResolver, defaults core.ImageDefaults, probe func(string) []mediacontract.VariantRef, siteLink func(string) string) (View, error) {

	src := upload.StorageURL(p.Src)
	if p.Binding != nil && p.Binding.Field != "" {
		if content == nil {
			return View{}, fmt.Errorf("%w，无法解析绑定 %q", core.ErrContentResolverRequired, p.Binding.Field)
		}
		v, err := content.ResolveString(p.Binding.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			src = v
		} else if p.Binding.Fallback != "" {
			src = p.Binding.Fallback
		}
	}
	if src == "" {
		return View{}, fmt.Errorf("图片地址为空")
	}

	src = upload.StorageURL(src)

	if !core.IsSafeURL(src) {
		src = ""
	}

	attrs := core.ResolveImageLoading(p.Loading, defaults)
	fp := core.ResolveFetchPriority(p.FetchPriority)

	v := View{
		Src:       src,
		Alt:       resolveImageAlt(p, content),
		Title:     p.Title,
		Class:     class,
		IsEager:   attrs.IsEager,
		Skeleton:  attrs.Skeleton,
		FetchHigh: fp.High,
		FetchLow:  fp.Low,
		NodeID:    node.ID,
		Caption:   p.Caption,
	}

	if probe != nil {
		if refs := probe(src); len(refs) > 0 {
			parts := make([]string, 0, len(refs))
			for _, ref := range refs {
				if ref.URL == "" || ref.Width <= 0 {
					continue
				}
				parts = append(parts, fmt.Sprintf("%s %dw", ref.URL, ref.Width))
			}
			if len(parts) > 0 {
				v.Srcset = strings.Join(parts, ", ")
				v.Sizes = "(max-width: 640px) 100vw, 50vw"
			}
		}
	}

	switch {
	case p.ClickAction == "lightbox":
		v.IsLightbox = true
	case p.Link != "" || p.ClickAction == "link":
		v.IsLink = true

		v.Link = core.SiteLinkOrSame(siteLink, p.Link)
		v.TargetBlank = p.LinkTarget == "blank"
		v.RelNofollow = p.LinkRel == "nofollow"
		v.LinkID = customID
	default:

		v.ImgID = customID
	}

	if v.IsLightbox {
		if svg, ok := core.IconSVGClass("close", "sky-lightbox-close-icon"); ok {
			v.CloseIcon = svg
		}
	}
	return v, nil
}

func resolveImageAlt(p *Props, content core.ContentResolver) string {
	if p == nil {
		return ""
	}
	if alt := strings.TrimSpace(p.Alt); alt != "" {
		return alt
	}
	if p.AltBinding == nil || strings.TrimSpace(p.AltBinding.Field) == "" || content == nil {
		return ""
	}
	v, err := content.ResolveString(p.AltBinding.Field)
	if err != nil {
		return ""
	}
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return strings.TrimSpace(p.AltBinding.Fallback)
}
