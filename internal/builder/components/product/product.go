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
// 槽位用自由文本而不是 bindingfield 下拉：商品集合源（issue #9）尚未注册进
// 集合元数据，bindingfield 的下拉当前取不到任何 product 字段，用它会让作者在
// #9 落地前根本无法选择商品字段；白名单已由上述两道服务端校验兜住。
package product

import (
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
	MediaField string `json:"mediaField,omitempty" ct:"string,maxlen=60,sec=content,label=主图字段"`
	// GalleryField 图集字段（JSON 数组，如 product.images）。
	GalleryField string `json:"galleryField,omitempty" ct:"string,maxlen=60,sec=content,label=图集字段"`
	// TitleField 标题字段（如 product.name）。
	TitleField string `json:"titleField,omitempty" ct:"string,maxlen=60,sec=content,label=标题字段"`
	// SubtitleField 副标题字段（如 product.subtitle）。
	SubtitleField string `json:"subtitleField,omitempty" ct:"string,maxlen=60,sec=content,label=副标题字段"`
	// PriceField 价格字段（如 product.priceRange）。
	PriceField string `json:"priceField,omitempty" ct:"string,maxlen=60,sec=content,label=价格字段"`
	// ComparePriceField 划线价字段（如 product.comparePrice）。
	ComparePriceField string `json:"comparePriceField,omitempty" ct:"string,maxlen=60,sec=content,label=划线价字段"`
	// DescriptionField 描述字段（富文本清洗后输出，如 product.description）。
	DescriptionField string `json:"descriptionField,omitempty" ct:"string,maxlen=60,sec=content,label=描述字段"`
	// OptionsField 规格维度字段（JSON 数组，如 product.options）：由商品的属性组派生，
	// 声明后才可能输出规格选择器（issue #8）。
	OptionsField string `json:"optionsField,omitempty" ct:"string,maxlen=60,sec=content,label=规格维度字段"`
	// VariantsField 变体组合字段（JSON 数组，如 product.variants）：由商品的变体派生，
	// 规格组合不足两个时不输出选择器（单变体商品不显示规格选择器）。
	VariantsField string `json:"variantsField,omitempty" ct:"string,maxlen=60,sec=content,label=变体组合字段"`
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
	Spec: core.AtomSpec[Props]{TypeName: Type, ValidateExtra: validateExtra},
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
		{Slot: slotGallery, Field: strings.TrimSpace(p.GalleryField)},
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
	slotGallery      = "gallery"
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

// compileCSS 商品详情样式：桌面两栏（媒体 + 信息），窄屏纵向堆叠。
//
// 宽度一律走 min(100%, …) / minmax(0, …)，不写死像素宽度（多端适配硬规则）：
// 大卡片 + 窄视口组合下也不会溢出。
func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: grid",
		"grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr)",
		"gap: 24px",
		"align-items: start",
	})
	b.Add(core.BreakpointMobile, sel, []string{
		"display: flex",
		"flex-direction: column",
		"gap: 16px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-media img", []string{
		"display: block",
		"width: min(100%, 100%)",
		"height: auto",
		"border-radius: 12px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-gallery", []string{
		"display: grid",
		"grid-template-columns: repeat(auto-fill, minmax(min(100%, 96px), 1fr))",
		"gap: 8px",
		"margin-top: 8px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-gallery img", []string{
		"display: block",
		"width: min(100%, 100%)",
		"height: auto",
		"aspect-ratio: 1 / 1",
		"object-fit: cover",
		"border-radius: 8px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-info", []string{
		"display: flex",
		"flex-direction: column",
		"gap: 10px",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-title", []string{
		"margin: 0",
		"font-size: 1.5rem",
		"line-height: 1.35",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-subtitle", []string{
		"margin: 0",
		"color: var(--sky-c-muted, rgba(0,0,0,0.6))",
		"line-height: 1.6",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-price-row", []string{
		"display: flex",
		"align-items: baseline",
		"gap: 10px",
		"flex-wrap: wrap",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-price", []string{
		"font-size: 1.5rem",
		"font-weight: 700",
		"color: var(--sky-c-primary, #2563eb)",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-compare", []string{
		"text-decoration: line-through",
		"color: var(--sky-c-muted, rgba(0,0,0,0.45))",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-description", []string{
		"line-height: 1.7",
		"word-break: break-word",
	})

	// 规格选择器（issue #8）：规格维度用原生 radio + label（键盘模型免费拿到：
	// Tab 进组、方向键切换），组合清单用 flex 行 + wrap（窄屏不横向溢出）。
	// 宽度一律 min(100%, …) / max-width: 100%，不写死像素（多端适配硬规则）。
	b.Add(core.BreakpointDesktop, sel+" .sky-product-options", []string{
		"display: flex",
		"flex-direction: column",
		"gap: 12px",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-option", []string{
		"display: flex",
		"flex-wrap: wrap",
		"align-items: center",
		"gap: 8px",
		"border: 0",
		"margin: 0",
		"padding: 0",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-option legend", []string{
		"padding: 0",
		"margin-right: 4px",
		"font-size: 13px",
		"color: var(--sky-c-muted, rgba(0,0,0,0.6))",
	})
	// radio 视觉隐藏但**保留可聚焦**（display:none 会把整组从键盘序列里移除）。
	b.Add(core.BreakpointDesktop, sel+" .sky-product-option-radio", []string{
		"position: absolute",
		"width: 1px",
		"height: 1px",
		"margin: -1px",
		"padding: 0",
		"border: 0",
		"clip-path: inset(50%)",
		"overflow: hidden",
		"white-space: nowrap",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-option-value", []string{
		"display: inline-flex",
		"align-items: center",
		"justify-content: center",
		"width: auto",
		"max-width: 100%",
		"min-height: 36px",
		"padding: 6px 14px",
		"border: 1px solid var(--sky-c-border, rgba(0,0,0,0.15))",
		"border-radius: 8px",
		"font-size: 13px",
		"line-height: 1.4",
		"cursor: pointer",
		"user-select: none",
		"word-break: break-word",
	})
	// 选中态（不依赖 :hover，触屏同样可见）。
	b.Add(core.BreakpointDesktop, sel+" .sky-product-option-radio:checked + .sky-product-option-value", []string{
		"border-color: var(--sky-c-primary, #2563eb)",
		"color: var(--sky-c-primary, #2563eb)",
		"background: rgba(37,99,235,0.08)",
	})
	// 键盘聚焦可见：焦点环画在对应标签上。
	b.Add(core.BreakpointDesktop, sel+" .sky-product-option-radio:focus-visible + .sky-product-option-value", []string{
		"outline: 2px solid var(--sky-c-primary, #2563eb)",
		"outline-offset: 2px",
	})
	// 鼠标/触摸板悬停：AddHover 自动包 @media (hover: hover)，触屏上不输出。
	b.AddHover(sel+" .sky-product-option-value", []string{
		"border-color: var(--sky-c-primary, #2563eb)",
	})
	// 按压反馈：AddActive 不带媒体查询，触屏按压同样生效（触屏唯一可靠的反馈）。
	b.AddActive(sel+" .sky-product-option-value", []string{
		"transform: translateY(1px)",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-variants", []string{
		"display: flex",
		"flex-direction: column",
		"gap: 6px",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-variant", []string{
		"display: flex",
		"flex-wrap: wrap",
		"align-items: baseline",
		"gap: 4px 10px",
		"font-size: 13px",
		"min-width: 0",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-variant-options", []string{
		"color: var(--sky-c-muted, rgba(0,0,0,0.6))",
		"min-width: 0",
		"word-break: break-word",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-product-variant-price", []string{
		"font-weight: 600",
	})
	// 窄视口（手机）：按压目标抬到 44px 高，标签内边距放宽 —— 触屏可点性优先。
	b.Add(core.BreakpointMobile, sel+" .sky-product-option-value", []string{
		"min-height: 44px",
		"padding: 8px 16px",
	})
}

// init 注册商品详情组件。
func init() {
	core.Register(Widget)
}
