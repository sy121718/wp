package heading

import (
	"fmt"
	"strings"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	Tag string

	Subtitle string

	Text string

	Highlight bool

	Segments []string
}

func BuildView(p *Props, content core.ContentResolver) (View, error) {
	tag := p.Tag
	if tag == "" {
		tag = "h2"
	}

	text := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if content == nil {
			return View{}, fmt.Errorf("%w，无法解析绑定 %q", core.ErrContentResolverRequired, p.Binding.Field)
		}
		v, err := content.ResolveString(p.Binding.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			text = v
		} else if p.Binding.Fallback != "" {
			text = p.Binding.Fallback
		}
	}

	view := View{Tag: tag, Subtitle: p.Subtitle, Text: text, Highlight: p.HighlightColor != ""}

	if textAnimOn(p) {
		view.Segments = splitTextSegments(text, p.TextAnim)
	}
	return view, nil
}

const maxTextSegments = 120

func splitTextSegments(text, mode string) []string {
	if text == "" {
		return nil
	}
	var segs []string
	if mode == "words" {
		for _, w := range strings.SplitAfter(text, " ") {
			if w != "" {
				segs = append(segs, w)
			}
		}
	} else {
		for _, r := range text {
			segs = append(segs, string(r))
		}
	}
	if len(segs) == 0 || len(segs) > maxTextSegments {
		return nil
	}
	return segs
}
