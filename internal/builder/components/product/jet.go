package product

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"math"
	"net/url"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type Image struct {
	URL string

	Alt string

	Eager bool
}

type View struct {
	HasInfo bool

	TitleTag string

	StockNote string

	Currency string

	HasMedia bool

	MediaURL string

	MediaAlt string

	HasGallery bool

	Gallery []Image

	HasTitle bool
	Title    string

	HasSubtitle bool
	Subtitle    string

	HasPrice bool
	Price    string

	HasComparePrice bool
	ComparePrice    string

	HasDiscount     bool
	DiscountPercent string

	HasDescription  bool
	DescriptionHTML string

	HasCategories bool
	Categories    []RelatedLink

	HasBrand bool
	Brand    RelatedLink

	Labels     Labels
	LiveLabels LiveLabels

	HasRatingLine bool

	RatingValue string

	RatingCount string

	HasOptions bool

	OptionGroups []OptionGroup

	VariantOptions []VariantOption
}

type RelatedLink struct {
	Slug string
	Name string

	Href string
}

type relatedPayload struct {
	Categories []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"categories"`
	Brand *struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"brand"`
}

type OptionValue struct {
	Key string

	Label string
}

type OptionGroup struct {
	Key string

	Name string

	Values []OptionValue
}

type VariantOption struct {
	ID string

	SKUCode string

	Price string

	ComparePrice string

	Labels string

	PriceCents int64

	PriceFactsURL string
	StockFactsURL string
}

type optionGroupJSON struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Values []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"values"`
}

type variantJSON struct {
	ID           string            `json:"id"`
	SKU          string            `json:"sku"`
	Price        string            `json:"price"`
	ComparePrice string            `json:"comparePrice"`
	Image        string            `json:"image"`
	Enabled      bool              `json:"enabled"`
	Options      map[string]string `json:"options"`
}

func BuildView(p *Props, content core.ContentResolver, projectID, lang string, siteLink func(string) string) (View, error) {
	source := effectiveSource(p)
	slots := p.slotFields()
	declared := 0
	for _, s := range slots {
		if s.Field != "" {
			declared++
		}
	}
	if declared == 0 {
		return View{}, fmt.Errorf("至少需要声明一个商品字段（主图/图集/标题/副标题/价格/划线价/描述/规格维度/变体组合）")
	}
	if content == nil {
		return View{}, fmt.Errorf("%w，无法解析商品字段（数据源 %s）", core.ErrContentResolverRequired, source)
	}

	view := View{TitleTag: effectiveTitleTag(p), Currency: effectiveCurrency(p), StockNote: textFallbackStockNote}

	categoryPrefix := localizePrefix(p.CategoryLinkPrefix, defaultCategoryLinkPrefix, siteLink)
	brandPrefix := localizePrefix(p.BrandLinkPrefix, defaultBrandLinkPrefix, siteLink)

	var rawOptions, rawVariants, rawMediaAlt, rawGalleryAlt string

	var rawPrice, rawComparePrice string
	for _, s := range slots {
		if s.Field == "" {
			continue
		}
		value, err := content.ResolveString(s.Field)
		if err != nil {
			return View{}, fmt.Errorf("解析商品字段 %q 失败: %w", s.Field, err)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		switch s.Slot {
		case slotMedia:
			view.HasMedia, view.MediaURL = true, strings.TrimSpace(value)
		case slotMediaAlt:
			rawMediaAlt = strings.TrimSpace(value)
		case slotGallery:
			images := parseImages(value, "")
			if len(images) > 0 {
				view.HasGallery, view.Gallery = true, images
			}
		case slotGalleryAlt:
			rawGalleryAlt = strings.TrimSpace(value)
		case slotTitle:
			view.HasTitle, view.Title = true, value
		case slotSubtitle:
			view.HasSubtitle, view.Subtitle = true, value
		case slotCategories:
			catOK, catItems := parseRelatedCategories(value)
			view.HasCategories, view.Categories = linkRelated(catOK, catItems, categoryPrefix)
		case slotBrand:
			brandOK, brandItem := parseRelatedBrand(value)
			view.HasBrand, view.Brand = linkRelatedOne(brandOK, brandItem, brandPrefix)
		case slotRating:
			view.RatingValue = strings.TrimSpace(value)
		case slotRatingCount:
			view.RatingCount = strings.TrimSpace(value)
		case slotPrice:
			rawPrice = strings.TrimSpace(value)
			view.HasPrice, view.Price = true, view.Currency+rawPrice
		case slotComparePrice:
			rawComparePrice = strings.TrimSpace(value)
			view.HasComparePrice, view.ComparePrice = true, view.Currency+rawComparePrice
		case slotDescription:
			normalized, err := core.NormalizeRichMediaURLs(value)
			if err != nil {
				return View{}, fmt.Errorf("商品描述字段 %s: %w", s.Field, err)
			}
			view.HasDescription, view.DescriptionHTML = true, core.RichTextHTML(normalized)
		case slotOptions:
			rawOptions = value
		case slotVariants:
			rawVariants = value
		}
	}
	view.HasDiscount, view.DiscountPercent = discountBadge(rawPrice, rawComparePrice)

	view.HasRatingLine = view.RatingCount != ""
	view.HasInfo = view.HasTitle || view.HasSubtitle || view.HasPrice ||
		view.HasDescription || view.HasOptions || len(view.VariantOptions) > 0

	galleryAlts := parseAltList(rawGalleryAlt)
	for i := range view.Gallery {
		if i < len(galleryAlts) && galleryAlts[i] != "" {
			view.Gallery[i].Alt = galleryAlts[i]
			continue
		}
		if view.HasTitle {
			view.Gallery[i].Alt = view.Title
		}
	}

	if view.HasMedia && rawMediaAlt != "" {
		view.MediaAlt = rawMediaAlt
	} else if view.HasMedia && view.HasTitle {
		view.MediaAlt = view.Title
	}

	view.OptionGroups = ParseOptionGroups(rawOptions)
	view.VariantOptions = ParseVariantOptions(rawVariants, view.OptionGroups, view.Currency, projectID, lang)
	view.HasOptions = len(view.OptionGroups) > 0 && len(view.VariantOptions) > 1
	return view, nil
}

func ParseOptionGroups(raw string) []OptionGroup {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var rows []optionGroupJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	out := make([]OptionGroup, 0, len(rows))
	for _, r := range rows {
		if r.Key == "" {
			continue
		}
		values := make([]OptionValue, 0, len(r.Values))
		for _, v := range r.Values {
			if v.Key == "" {
				continue
			}
			values = append(values, OptionValue{Key: v.Key, Label: v.Label})
		}
		if len(values) == 0 {
			continue
		}
		out = append(out, OptionGroup{Key: r.Key, Name: r.Name, Values: values})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func FirstEnabledVariant(raw, currency string) (VariantOption, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return VariantOption{}, false
	}
	var rows []variantJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return VariantOption{}, false
	}
	for _, r := range rows {
		if !r.Enabled || strings.TrimSpace(r.ID) == "" {
			continue
		}
		out := VariantOption{ID: r.ID, SKUCode: r.SKU}
		if price := strings.TrimSpace(r.Price); price != "" {
			out.Price, out.PriceCents = currency+price, 0
		}
		if compare := strings.TrimSpace(r.ComparePrice); compare != "" {
			out.ComparePrice = currency + compare
		}
		return out, true
	}
	return VariantOption{}, false
}

func ParseVariantOptions(raw string, groups []OptionGroup, currency, projectID, lang string) []VariantOption {
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return nil
	}
	var rows []variantJSON
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil
	}
	labels := make(map[string]map[string]string, len(groups))
	for _, g := range groups {
		m := make(map[string]string, len(g.Values))
		for _, v := range g.Values {
			m[v.Key] = v.Label
		}
		labels[g.Key] = m
	}
	out := make([]VariantOption, 0, len(rows))
	for _, r := range rows {

		if !r.Enabled {
			continue
		}
		parts := make([]string, 0, len(groups))
		complete := true
		for _, g := range groups {
			key, ok := r.Options[g.Key]
			if !ok {
				complete = false
				break
			}
			label, ok := labels[g.Key][key]
			if !ok {
				complete = false
				break
			}
			name := g.Name
			if name == "" {
				name = g.Key
			}
			parts = append(parts, name+" "+label)
		}
		if !complete {
			continue
		}
		row := VariantOption{
			ID:      r.ID,
			SKUCode: r.SKU, Labels: strings.Join(parts, " · "),
			Price: currency + r.Price,
		}

		row.StockFactsURL = variantFactsURL(r.ID, projectID, lang)

		if cents, ok := priceYuanCents(r.Price); ok {
			row.PriceCents = cents
			row.PriceFactsURL = row.StockFactsURL
		}
		if strings.TrimSpace(r.ComparePrice) != "" {
			row.ComparePrice = currency + r.ComparePrice
		}
		out = append(out, row)
	}
	return out
}

const PublicVariantsPath = "/api/site/product/variants"
const paramProjectID = "projectId"

func variantFactsURL(variantID, projectID, lang string) string {
	if strings.TrimSpace(variantID) == "" || strings.TrimSpace(projectID) == "" {
		return ""
	}
	q := url.Values{}
	q.Set(paramProjectID, strings.TrimSpace(projectID))
	q.Set("variantIds", variantID)
	if lang != "" {
		q.Set("lang", lang)
	}
	return PublicVariantsPath + "?" + q.Encode()
}

const (
	defaultCategoryLinkPrefix = "/product_category"
	defaultBrandLinkPrefix    = "/product_brand"
)

func localizePrefix(configured, fallback string, siteLink func(string) string) string {
	prefix := strings.TrimSpace(configured)
	if prefix == "" {
		prefix = fallback
	}
	if siteLink == nil {
		return prefix
	}
	return siteLink(prefix)
}

func linkRelated(ok bool, items []RelatedLink, prefix string) (bool, []RelatedLink) {
	if !ok {
		return false, nil
	}
	for i := range items {
		items[i].Href = joinArchiveHref(prefix, items[i].Slug)
	}
	return true, items
}

func linkRelatedOne(ok bool, item RelatedLink, prefix string) (bool, RelatedLink) {
	if !ok {
		return false, RelatedLink{}
	}
	item.Href = joinArchiveHref(prefix, item.Slug)
	return true, item
}

func joinArchiveHref(prefix, slug string) string {
	p, s := strings.TrimSpace(prefix), strings.TrimSpace(slug)
	if p == "" || s == "" {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p + s
}

func parseRelatedCategories(raw string) (bool, []RelatedLink) {
	p, ok := parseRelated(raw)
	if !ok {
		return false, nil
	}
	out := make([]RelatedLink, 0, len(p.Categories))
	for _, c := range p.Categories {
		if strings.TrimSpace(c.Slug) == "" || strings.TrimSpace(c.Name) == "" {
			continue
		}
		out = append(out, RelatedLink{Slug: c.Slug, Name: c.Name})
	}
	if len(out) == 0 {
		return false, nil
	}
	return true, out
}

func parseRelatedBrand(raw string) (bool, RelatedLink) {
	p, ok := parseRelated(raw)
	if !ok || p.Brand == nil {
		return false, RelatedLink{}
	}
	slug, name := strings.TrimSpace(p.Brand.Slug), strings.TrimSpace(p.Brand.Name)
	if slug == "" || name == "" {
		return false, RelatedLink{}
	}
	return true, RelatedLink{Slug: slug, Name: name}
}

func parseRelated(raw string) (relatedPayload, bool) {
	var p relatedPayload
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "{}" {
		return p, false
	}
	if err := json.Unmarshal([]byte(trimmed), &p); err != nil {
		return p, false
	}
	return p, true
}

func discountBadge(priceText, compareText string) (bool, string) {
	price, perr := strconv.ParseFloat(strings.TrimSpace(priceText), 64)
	compare, cerr := strconv.ParseFloat(strings.TrimSpace(compareText), 64)
	if perr != nil || cerr != nil || price <= 0 || compare <= price {
		return false, ""
	}
	percent := int(math.Floor((compare - price) / compare * 100))
	if percent <= 0 {
		return false, ""
	}
	return true, "-" + strconv.Itoa(percent) + "%"
}

func priceYuanCents(text string) (int64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, false
	}
	return int64(math.Round(v * 100)), true
}

func parseAltList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var alts []string
	if err := json.Unmarshal([]byte(raw), &alts); err != nil {
		return nil
	}
	out := make([]string, 0, len(alts))
	for _, a := range alts {
		out = append(out, strings.TrimSpace(a))
	}
	return out
}

func parseImages(raw, alt string) (images []Image) {
	var urls []string
	if err := json.Unmarshal([]byte(raw), &urls); err != nil {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				urls = append(urls, part)
			}
		}
	}
	for _, u := range urls {
		if u = strings.TrimSpace(u); u == "" {
			continue
		}
		images = append(images, Image{URL: u, Alt: alt, Eager: len(images) == 0})
	}
	return images
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	if !v.HasOptions || len(v.VariantOptions) == 0 {
		return nil, nil
	}

	for _, vo := range v.VariantOptions {
		if vo.PriceFactsURL != "" || vo.StockFactsURL != "" {
			return []string{"data-site-read", "data-site-product"}, nil
		}
	}
	return nil, nil
}
