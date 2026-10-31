package text

import (
	"fmt"
	"html"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	IsPlain bool

	PlainTag string

	Text string

	SanitizedContent string
}

func BuildView(p *Props, content core.ContentResolver) (View, error) {
	mode := p.Mode
	if mode == "" {
		mode = ModeRichText
	}
	plainTag := p.PlainTag
	if mode == ModePlainText && plainTag == "" {
		plainTag = "p"
	}

	c := p.Text
	if p.Binding != nil && p.Binding.Field != "" {
		if content == nil {
			return View{}, fmt.Errorf("%w，无法解析绑定 %q", core.ErrContentResolverRequired, p.Binding.Field)
		}
		v, err := content.ResolveString(p.Binding.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			c = v
		} else if p.Binding.Fallback != "" {
			c = p.Binding.Fallback
		}
	}

	if mode == ModePlainText {
		return View{IsPlain: true, PlainTag: plainTag, Text: c}, nil
	}

	c, err := core.NormalizeRichMediaURLs(c)
	if err != nil {
		return View{}, fmt.Errorf("正文内容: %w", err)
	}
	rich := core.AbsolutizeInternalLinks(sanitizeRichHTML(c))
	if p.Excerpt > 0 {
		rich = html.EscapeString(truncateRunes(stripRichTags(c), p.Excerpt))
	}
	return View{IsPlain: false, SanitizedContent: rich}, nil
}
