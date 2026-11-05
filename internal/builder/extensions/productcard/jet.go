package productcard

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"strings"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	TitleTag string
	Currency string

	HasImage bool
	ImageURL string

	ImageAlt string

	HasTitle bool
	Title    string

	Href string

	HasPrice bool
	Price    string

	HasComparePrice bool
	ComparePrice    string

	Tags []string
}

func BuildView(p *Props, content core.ContentResolver, siteLink func(string) string) (view View, err error) {
	if p == nil {
		return View{}, fmt.Errorf("商品卡属性为空")
	}
	slots := p.slotFields()
	declared := 0
	for _, s := range slots {
		if s.Field != "" {
			declared++
		}
	}
	if declared == 0 {
		return View{}, fmt.Errorf("至少需要声明一个商品字段（主图 / 标题 / 价格 / 划线价 / 标签 / 链接）")
	}
	if content == nil {
		return View{}, fmt.Errorf("%w，无法解析商品字段", core.ErrContentResolverRequired)
	}

	view = View{TitleTag: effectiveTitleTag(p), Currency: effectiveCurrency(p)}
	var rawImageAlt, rawTitle string
	for _, s := range slots {
		if s.Field == "" {
			continue
		}
		value, rerr := content.ResolveString(s.Field)
		if rerr != nil {
			return View{}, fmt.Errorf("解析商品字段 %q 失败: %w", s.Field, rerr)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		switch s.Slot {
		case slotImage:
			if url := FirstImageURL(value); url != "" {
				view.HasImage, view.ImageURL = true, url
			}
		case slotImageAlt:
			rawImageAlt = strings.TrimSpace(value)
		case slotTitle:
			view.HasTitle, view.Title = true, value
			rawTitle = strings.TrimSpace(value)
		case slotPrice:
			view.HasPrice, view.Price = true, view.Currency+strings.TrimSpace(value)
		case slotComparePrice:
			view.HasComparePrice, view.ComparePrice = true, view.Currency+strings.TrimSpace(value)
		case slotTags:
			view.Tags = ParseTagNames(value)
		case slotLink:

			view.Href = CardHref(LocalizePrefix(p.LinkPrefix, siteLink), value)
		}
	}

	if view.HasImage {
		view.ImageAlt = rawImageAlt
		if view.ImageAlt == "" {
			view.ImageAlt = rawTitle
		}
	}
	return view, nil
}

func FirstImageURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "[") {
		var urls []string
		if uerr := json.Unmarshal([]byte(trimmed), &urls); uerr != nil {
			return ""
		}
		for _, u := range urls {
			if s := strings.TrimSpace(u); s != "" {
				return s
			}
		}
		return ""
	}
	if strings.HasPrefix(trimmed, "{") {

		return ""
	}
	return trimmed
}

func ParseTagNames(value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var names []string
		if uerr := json.Unmarshal([]byte(trimmed), &names); uerr != nil {
			return nil
		}
		out := make([]string, 0, len(names))
		for _, n := range names {
			if s := strings.TrimSpace(n); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{trimmed}
}

func CardHref(prefix, value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "#") && !hasScheme(v) {
		p := strings.TrimSpace(prefix)
		if p == "" {

			if !core.IsSafeURL("/" + v) {
				return ""
			}
			return v
		}
		out := strings.TrimSuffix(p, "/") + "/" + strings.TrimPrefix(v, "/")
		if !core.IsSafeURL(out) {
			return ""
		}
		return out
	}
	if !core.IsSafeURL(v) {
		return ""
	}
	return v
}

func LocalizePrefix(prefix string, siteLink func(string) string) string {
	if strings.TrimSpace(prefix) == "" {
		return prefix
	}
	return core.SiteLinkOrSame(siteLink, prefix)
}

func hasScheme(s string) bool {
	idx := strings.Index(s, ":")
	if idx <= 0 {
		return false
	}
	for i := 0; i < idx; i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '-' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}
