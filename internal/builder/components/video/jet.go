package video

import (
	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	IsEmbed bool

	EmbedSrc string

	Poster string

	Autoplay bool
	Loop     bool
	Muted    bool
	Controls bool

	Preload string

	CaptionsSrc   string
	CaptionsLabel string
	CaptionsLang  string

	Src string

	Title string
}

const TextKeyTitle = "site.component.video.title"

const textFallbackTitle = "视频"

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		v.Title = textFallbackTitle
		return
	}
	v.Title = text(TextKeyTitle, textFallbackTitle)
}

func BuildView(p *Props) View {
	v := View{
		Poster:        p.Poster,
		Autoplay:      p.Autoplay,
		Loop:          p.Loop,
		Muted:         p.Muted || p.Autoplay,
		Controls:      p.Controls,
		CaptionsSrc:   p.CaptionsSrc,
		CaptionsLabel: p.CaptionsLabel,
		CaptionsLang:  p.CaptionsLang,
	}

	videoURL := p.URL
	if embedSrc, ok := embedURL(videoURL); ok {
		v.IsEmbed = true
		v.EmbedSrc = embedSrc
		return v
	}

	preload := p.Preload
	if preload == "" {
		preload = "metadata"
	}
	v.Preload = preload
	v.Src = videoURL
	return v
}
