// Package product 实现 core.product 商品详情组件（issue #6）。
//
// 定位：吃商品数据的展示组件。它**声明自己需要的商品字段**（一组命名槽位：
// 主图 / 图集 / 标题 / 副标题 / 价格 / 划线价 / 描述，槽位留空 = 不需要该字段），
// 构建期由商品解析器（实体类型注册表 → product 模块）把商品实体的数据静态填入，
// 访客请求期零查库（不变量 1）。
//
// 字段白名单（不变量 4）：槽位值写成 "product.<字段名>"，保存模板时由
// builder.ValidateFieldRefs 按实体类型注册表校验，构建期解析器再拒一次越界字段；
// 白名单的唯一来源是 product 模块的 contract（不在组件里另写一份）。
//
// 槽位用 bindingfield 下拉（审计 EDT-006）：选项来自 content:product 集合源的字段白名单,
// 与保存期 ValidateFieldRefs、构建期解析器读的是**同一份**来源 —— 手写 "prodct.name"
// 这种拼写以前要等到保存或构建才报错，现在根本选不出来。
//
// prefixes=product 限定只列 product 前缀：详情页组件的槽位绑的是当前实体，
// item.<字段> 是集合项作用域（集合里那一行），在详情页里没有含义；两者混在一张下拉里，
// 作者得自己记住哪个能用。
//
// 下拉只是输入的辅助，**不是安全边界**：请求可以绕过编辑器直接提交，白名单仍由
// ValidateFieldRefs 与构建期解析器两道校验兜住。
// 商品集合源 content:product 已在装配层注册（routes.go）。
package product

import (
	_ "embed" // product.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.product"

// 缺省数据源实体类型与货币符号。
const (
	defaultSource   = "product"
	defaultCurrency = "¥"
)

// fieldPathRe 槽位字段路径白名单（与 heading/text/image 的绑定路径同形）。
var fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)

// Props core.product 属性：命名槽位声明本组件需要的商品字段。
type Props struct {
	// Source 数据源实体类型（当前只有 product；跨数据源绑定会被校验拒绝）。
	Source string `json:"source,omitempty" ct:"select,product=商品,default=product,sec=content,label=数据源"`
	// MediaField 主图字段（如 product.defaultImage）。
	MediaField string `json:"mediaField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=主图字段"`
	// GalleryField 图集字段（JSON 数组，如 product.images）。
	GalleryField string `json:"galleryField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=图集字段"`
	// TitleField 标题字段（如 product.name）。
	TitleField string `json:"titleField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=标题字段"`
	// SubtitleField 副标题字段（如 product.subtitle）。
	SubtitleField string `json:"subtitleField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=副标题字段"`
	// PriceField 价格字段（如 product.priceRange）。
	PriceField string `json:"priceField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=价格字段"`
	// ComparePriceField 划线价字段（如 product.comparePrice）。
	ComparePriceField string `json:"comparePriceField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=划线价字段"`
	// DescriptionField 描述字段（富文本清洗后输出，如 product.description）。
	DescriptionField string `json:"descriptionField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=描述字段"`
	// MediaAltField 主图 alt 文本字段（如 product.imageAlt）：作者填写的图片替代文本，
	// 参与内容翻译（issue #12）；留空由商品名兜底。
	MediaAltField string `json:"mediaAltField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=主图 alt 字段"`
	// GalleryAltField 图集 alt 数组字段（JSON 数组，如 product.imageAlts）：与图集逐位对应，
	// 元素可为空串；逐元素按构建语言取译文（issue #12）。
	GalleryAltField string `json:"galleryAltField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=图集 alt 字段"`
	// OptionsField 规格维度字段（JSON 数组，如 product.options）：由商品的属性组派生，
	// 声明后才可能输出规格选择器（issue #8）。
	OptionsField string `json:"optionsField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=规格维度字段"`
	// VariantsField 变体组合字段（JSON 数组，如 product.variants）：由商品的变体派生，
	// 规格组合不足两个时不输出选择器（单变体商品不显示规格选择器）。
	VariantsField string `json:"variantsField,omitempty" ct:"bindingfield,prefixes=product,maxlen=60,sec=content,label=变体组合字段"`
	// Currency 货币符号（价格槽位前缀；留空用默认符号）。
	Currency string `json:"currency,omitempty" ct:"text,maxlen=8,sec=content,label=货币符号"`
	// TitleTag 标题标签层级（h1~h3，默认 h2；h1 由页面标题承担时选 h2）。
	TitleTag string `json:"titleTag,omitempty" ct:"select,h1=一级标题,h2=二级标题,h3=三级标题,default=h2,sec=content,label=标题层级"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Component 商品详情组件：Atom 基座（校验/Advanced/可翻译） + 字段绑定自报。
type Component struct {
	core.Atom[Props]
}

// Widget 组件实例。
var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:        Type,
		ValidateExtra:   validateExtra,
		PaletteCategory: core.PaletteCategoryBasic,
		DisplayName:     "商品详情",
		Hint:            "吃商品数据的详情组件",
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

// validateExtra 关系性校验：槽位字段路径必须规范，且与数据源类型一致。
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
			continue // 空槽位 = 不需要该字段
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

// FieldBindings 实现 core.FieldBindingProvider：自报本节点声明的商品字段绑定。
//
// 只把「声明了什么」报出去，合法性由 builder.ValidateFieldRefs 按注册表统一判定。
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

// slotField 一个命名槽位与其声明的字段路径。
type slotField struct {
	// Slot 槽位名（错误提示与视图填充用）。
	Slot string
	// Field 字段路径（数据源.字段名），空槽位不申报。
	Field string
}

// slotFields 按渲染顺序给出全部槽位（含空槽位，便于统一遍历）。
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
	}
}

// 槽位名常量（视图与错误提示共用）。
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
)

// effectiveSource 有效数据源类型（空取默认 product）。
func effectiveSource(p *Props) string {
	if p == nil || strings.TrimSpace(p.Source) == "" {
		return defaultSource
	}
	return strings.TrimSpace(p.Source)
}

// effectiveCurrency 有效货币符号（空取默认符号）。
func effectiveCurrency(p *Props) string {
	if p == nil || p.Currency == "" {
		return defaultCurrency
	}
	return p.Currency
}

// effectiveTitleTag 有效标题层级（默认 h2）。
func effectiveTitleTag(p *Props) string {
	switch p.TitleTag {
	case "h1", "h2", "h3":
		return p.TitleTag
	default:
		return "h2"
	}
}

// productCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组。
//
//go:embed product.css
var productCSS string

// compileCSS 商品详情样式：桌面两栏（媒体 + 信息），窄屏纵向堆叠。
//
// 规则与 Props 无关（纯静态样式），故不传变量表；样式源里也没有 {{...}} 占位。
func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, productCSS); err != nil {
		panic(fmt.Sprintf("product 组件样式解析失败: %v", err))
	}
}

// init 注册商品详情组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("product", productTemplate)
}

// productTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed product.jet
var productTemplate string
