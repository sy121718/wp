package siteurl

import (
	"fmt"
	"strings"

	"go_wp/pkg/pathkit"
)

const (
	KindArticle  = "article"
	KindProduct  = "product"
	KindCategory = "product_category"
	KindBrand    = "product_brand"
	KindTag      = "product_tag"
)

const (
	PlaceholderSlug = "{slug}"
	PlaceholderID   = "{id}"
)

var DefaultPatterns = map[string]string{
	KindArticle:  "/blog/" + PlaceholderSlug,
	KindProduct:  "/products/" + PlaceholderSlug,
	KindCategory: "/category/" + PlaceholderSlug,
	KindBrand:    "/brand/" + PlaceholderSlug,
	KindTag:      "/tag/" + PlaceholderSlug,
}

func PatternOf(kind string, patterns map[string]string) string {
	if p := strings.TrimSpace(patterns[kind]); p != "" {
		return p
	}
	return DefaultPatterns[strings.TrimSpace(kind)]
}

func DetailPath(kind, slug, id string, patterns map[string]string) string {
	pattern := PatternOf(kind, patterns)
	if pattern == "" {
		return ""
	}
	slug = strings.TrimSpace(slug)
	id = strings.TrimSpace(id)
	if slug == "" {
		slug = id
	}
	out := strings.ReplaceAll(pattern, PlaceholderSlug, slug)
	out = strings.ReplaceAll(out, PlaceholderID, id)
	return pathkit.NormalizeSitePath(out)
}

const MaxPatternLen = 120

func ValidatePattern(pattern string) error {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return fmt.Errorf("路径模式不能为空")
	}
	if len(p) > MaxPatternLen {
		return fmt.Errorf("路径模式过长（上限 %d 字符）", MaxPatternLen)
	}
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("路径模式必须以 / 开头")
	}
	if strings.ContainsAny(p, "?#") {
		return fmt.Errorf("路径模式不能包含 ? 或 #")
	}
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '/' || r == '-' || r == '_' || r == '.':
		case r == '{' || r == '}':
		default:
			return fmt.Errorf("路径模式只能包含字母、数字与 - _ . / { }（含空格与中文都不行）")
		}
	}
	if !strings.Contains(p, PlaceholderSlug) && !strings.Contains(p, PlaceholderID) {
		return fmt.Errorf("路径模式必须包含 %s 或 %s，否则所有内容会指向同一个路径", PlaceholderSlug, PlaceholderID)
	}
	return nil
}

var KnownKinds = []struct {
	Kind  string
	Label string
}{
	{KindArticle, "文章详情页"},
	{KindProduct, "商品详情页"},
	{KindCategory, "商品分类页"},
	{KindBrand, "品牌页"},
	{KindTag, "标签页"},
}
