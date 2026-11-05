package product

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.product"

const (
	defaultSource   = "product"
	defaultCurrency = "¥"
)

var fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)

type Props struct {
	Source string `json:"source,omitempty" ct:"select,product=商品,default=product,sec=content,label=数据源"`

	MediaField string `json:"mediaField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=主图字段"`

	GalleryField string `json:"galleryField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=图集字段"`

	TitleField string `json:"titleField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=标题字段"`

	SubtitleField string `json:"subtitleField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=副标题字段"`

	PriceField string `json:"priceField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=价格字段"`

	ComparePriceField string `json:"comparePriceField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=划线价字段"`

	DescriptionField string `json:"descriptionField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=描述字段"`

	MediaAltField string `json:"mediaAltField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=主图 alt 字段"`

	GalleryAltField string `json:"galleryAltField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=图集 alt 字段"`

	OptionsField string `json:"optionsField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=规格维度字段"`

	VariantsField string `json:"variantsField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=变体组合字段"`

	CategoriesField string `json:"categoriesField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=分类字段"`

	BrandField string `json:"brandField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=品牌字段"`

	RatingField string `json:"ratingField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=评分字段"`

	RatingCountField string `json:"ratingCountField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=评价数字段"`

	CategoryLinkPrefix string `json:"categoryLinkPrefix,omitempty" ct:"text,maxlen=80,sec=content,label=分类链接前缀"`

	BrandLinkPrefix string `json:"brandLinkPrefix,omitempty" ct:"text,maxlen=80,sec=content,label=品牌链接前缀"`

	Currency string `json:"currency,omitempty" ct:"string,maxlen=8,sec=content,label=货币符号"`

	TitleTag string `json:"titleTag,omitempty" ct:"select,h1=一级标题,h2=二级标题,h3=三级标题,default=h2,sec=content,label=标题层级"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct {
	core.Atom[Props]
}

var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:             Type,
		ValidateExtra:        validateExtra,
		PaletteCategory:      core.PaletteCategoryBasic,
		PaletteEntityContext: true,
		DisplayName:          "商品详情",
		Hint:                 "吃商品数据的详情组件",
		DefaultProps: map[string]any{
			"source":           "product",
			"titleField":       "product.name",
			"subtitleField":    "product.subtitle",
			"mediaField":       "product.defaultImage",
			"galleryField":     "product.images",
			"priceField":       "product.priceRange",
			"descriptionField": "product.description",
			"currency":         "¥",
			"titleTag":         "h2",
		},
	},
}}

func validateExtra(p *Props, nodeID string) (err error) {
	source := effectiveSource(p)
	slots := p.slotFields()
	declared := 0
	for _, s := range slots {
		if s.Field != "" {
			declared++
		}
	}
	if declared == 0 {
		return fmt.Errorf("至少需要声明一个商品字段（主图/图集/标题/副标题/价格/划线价/描述/规格维度/变体组合）")
	}
	for _, s := range slots {
		if s.Field == "" {
			continue
		}
		if !fieldPathRe.MatchString(s.Field) {
			return fmt.Errorf("无效的字段路径 %q（期望 数据源.字段名，如 product.name）", s.Field)
		}
		if typ := strings.SplitN(s.Field, ".", 2)[0]; typ != source {
			return fmt.Errorf("字段 %q 不属于数据源 %q", s.Field, source)
		}
	}
	switch p.TitleTag {
	case "", "h1", "h2", "h3":
	default:
		return fmt.Errorf("无效的标题层级 %q", p.TitleTag)
	}
	return nil
}

func (c *Component) FieldBindings(node *core.Node) (refs []core.FieldRef, err error) {
	if node == nil {
		return nil, nil
	}
	var p Props
	if len(node.Props) > 0 {
		if uerr := json.Unmarshal(node.Props, &p); uerr != nil {
			return nil, fmt.Errorf("props 反序列化失败: %w", uerr)
		}
	}
	for _, s := range p.slotFields() {
		if s.Field == "" {
			continue
		}
		parts := strings.SplitN(s.Field, ".", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("无效的字段路径 %q", s.Field)
		}
		refs = append(refs, core.FieldRef{EntityType: parts[0], Field: parts[1]})
	}
	return refs, nil
}

type slotField struct {
	Slot string

	Field string
}

func (p *Props) slotFields() []slotField {
	if p == nil {
		return nil
	}
	return []slotField{
		{Slot: slotMedia, Field: strings.TrimSpace(p.MediaField)},
		{Slot: slotMediaAlt, Field: strings.TrimSpace(p.MediaAltField)},
		{Slot: slotGallery, Field: strings.TrimSpace(p.GalleryField)},
		{Slot: slotGalleryAlt, Field: strings.TrimSpace(p.GalleryAltField)},
		{Slot: slotTitle, Field: strings.TrimSpace(p.TitleField)},
		{Slot: slotSubtitle, Field: strings.TrimSpace(p.SubtitleField)},
		{Slot: slotPrice, Field: strings.TrimSpace(p.PriceField)},
		{Slot: slotComparePrice, Field: strings.TrimSpace(p.ComparePriceField)},
		{Slot: slotDescription, Field: strings.TrimSpace(p.DescriptionField)},
		{Slot: slotOptions, Field: strings.TrimSpace(p.OptionsField)},
		{Slot: slotVariants, Field: strings.TrimSpace(p.VariantsField)},
		{Slot: slotCategories, Field: strings.TrimSpace(p.CategoriesField)},
		{Slot: slotBrand, Field: strings.TrimSpace(p.BrandField)},
		{Slot: slotRating, Field: strings.TrimSpace(p.RatingField)},
		{Slot: slotRatingCount, Field: strings.TrimSpace(p.RatingCountField)},
	}
}

const (
	slotMedia        = "media"
	slotMediaAlt     = "mediaAlt"
	slotGallery      = "gallery"
	slotGalleryAlt   = "galleryAlt"
	slotTitle        = "title"
	slotSubtitle     = "subtitle"
	slotPrice        = "price"
	slotComparePrice = "comparePrice"
	slotDescription  = "description"
	slotOptions      = "options"
	slotVariants     = "variants"

	slotCategories  = "categories"
	slotBrand       = "brand"
	slotRating      = "rating"
	slotRatingCount = "ratingCount"
)

func effectiveSource(p *Props) string {
	if p == nil || strings.TrimSpace(p.Source) == "" {
		return defaultSource
	}
	return strings.TrimSpace(p.Source)
}

func effectiveCurrency(p *Props) string {
	if p == nil || p.Currency == "" {
		return defaultCurrency
	}
	return p.Currency
}

func effectiveTitleTag(p *Props) string {
	switch p.TitleTag {
	case "h1", "h2", "h3":
		return p.TitleTag
	default:
		return "h2"
	}
}

//go:embed product.css
var productCSS string

func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, productCSS); err != nil {
		panic(fmt.Sprintf("product 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("product", productTemplate)
}

//go:embed product.jet
var productTemplate string
