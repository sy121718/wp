package productcard

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"math"
	"regexp"
	"strconv"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.productCard"

const (
	defaultCurrency = "¥"

	defaultTitleTag = "h3"
)

var fieldPathRe = regexp.MustCompile(`^(item|product)\.[a-z][a-zA-Z0-9_]*$`)

type Props struct {
	ImageField string `json:"imageField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=主图字段"`

	ImageAltField string `json:"imageAltField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=主图 alt 字段"`

	TitleField string `json:"titleField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=标题字段"`

	PriceField string `json:"priceField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=价格字段"`

	ComparePriceField string `json:"comparePriceField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=划线价字段"`

	TagsField string `json:"tagsField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=标签字段"`

	LinkField string `json:"linkField,omitempty" ct:"bindingfield,prefixes=item|product,maxlen=60,sec=content,label=链接字段"`

	LinkPrefix string `json:"linkPrefix,omitempty" ct:"text,maxlen=200,sec=content,label=链接前缀"`

	Currency string `json:"currency,omitempty" ct:"string,maxlen=8,sec=content,label=货币符号"`

	TitleTag string `json:"titleTag,omitempty" ct:"select,h2=二级标题,h3=三级标题,h4=四级标题,default=h3,sec=content,label=标题层级"`

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
		DisplayName:          "商品卡",
		Hint:                 "吃商品数据的最小展示单元（可作集合卡模板）",
		DefaultProps: map[string]any{
			"imageField":        "item.images",
			"titleField":        "item.name",
			"priceField":        "item.priceRange",
			"comparePriceField": "item.comparePrice",
			"tagsField":         "item.tags",
			"linkField":         "item.url",
			"linkPrefix":        "",
			"currency":          "¥",
			"titleTag":          "h3",
		},
	},
}}

type slotField struct {
	Slot  string
	Field string
}

const (
	slotImage        = "image"
	slotImageAlt     = "imageAlt"
	slotTitle        = "title"
	slotPrice        = "price"
	slotComparePrice = "comparePrice"
	slotTags         = "tags"
	slotLink         = "link"
)

func (p *Props) slotFields() []slotField {
	if p == nil {
		return nil
	}
	return []slotField{
		{Slot: slotImage, Field: strings.TrimSpace(p.ImageField)},
		{Slot: slotImageAlt, Field: strings.TrimSpace(p.ImageAltField)},
		{Slot: slotTitle, Field: strings.TrimSpace(p.TitleField)},
		{Slot: slotPrice, Field: strings.TrimSpace(p.PriceField)},
		{Slot: slotComparePrice, Field: strings.TrimSpace(p.ComparePriceField)},
		{Slot: slotTags, Field: strings.TrimSpace(p.TagsField)},
		{Slot: slotLink, Field: strings.TrimSpace(p.LinkField)},
	}
}

func validateExtra(p *Props, _ string) (err error) {
	declared := 0
	for _, s := range p.slotFields() {
		if s.Field == "" {
			continue
		}
		declared++
		if !fieldPathRe.MatchString(s.Field) {
			return fmt.Errorf("无效的字段路径 %q（期望 item.字段名 或 product.字段名，如 item.name）", s.Field)
		}
	}
	if declared == 0 {
		return fmt.Errorf("至少需要声明一个商品字段（主图 / 标题 / 价格 / 划线价 / 标签 / 链接）")
	}
	switch p.TitleTag {
	case "", "h2", "h3", "h4":
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
		prefix, field, ok := strings.Cut(s.Field, ".")
		if !ok || field == "" {
			return nil, fmt.Errorf("无效的字段路径 %q", s.Field)
		}

		if prefix != "item" && prefix != "product" {
			return nil, fmt.Errorf("无效的字段前缀 %q（只接受 item. 或 product.）", s.Field)
		}
		refs = append(refs, core.FieldRef{EntityType: "product", Field: field})
	}
	return refs, nil
}

func FormatPrice(currency, raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	amount, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return currency + value
	}
	if currency == "" {
		return strconv.FormatFloat(amount, 'f', 2, 64)
	}
	return currency + " " + strconv.FormatFloat(amount, 'f', 2, 64)
}

func DiscountPercent(priceRaw, compareRaw string) int {
	price, perr := strconv.ParseFloat(strings.TrimSpace(priceRaw), 64)
	compare, cerr := strconv.ParseFloat(strings.TrimSpace(compareRaw), 64)
	if perr != nil || cerr != nil || price <= 0 || compare <= price {
		return 0
	}
	percent := int(math.Floor((compare - price) / compare * 100))
	if percent <= 0 {
		return 0
	}
	return percent
}

func effectiveCurrency(p *Props) string {
	if p == nil || strings.TrimSpace(p.Currency) == "" {
		return defaultCurrency
	}
	return p.Currency
}

func effectiveTitleTag(p *Props) string {
	switch p.TitleTag {
	case "h2", "h3", "h4":
		return p.TitleTag
	default:
		return defaultTitleTag
	}
}

//go:embed productcard.css
var productCardCSS string

func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, productCardCSS); err != nil {
		panic(fmt.Sprintf("productCard 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("product_card", productcardTemplate)
}

//go:embed product_card.jet
var productcardTemplate string
