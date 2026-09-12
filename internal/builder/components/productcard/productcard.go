// Package productcard 实现 core.productCard 商品卡组件（issue #22）。
//
// 定位：吃商品数据的**最小展示单元** —— 主图 / 标题 / 价格（区间） / 划线价 / 标签 / 详情链接。
// 它不负责取数（那是集合源与实体解析器的事），只负责「一份商品数据长成一张卡」。
//
// 两种使用方式共用同一份字段白名单：
//
//	集合卡模板：字段写 item.<字段>，被集合组件（商品列表 / cardstack）在展开每一项时
//	            注入集合项作用域（core.ItemScope），一张卡对应一个商品；
//	实体绑定：  字段写 product.<字段>，直接绑当前页面的商品实体（详情页里的推荐位）。
//
// 白名单只有一份：FieldBindings 把两种前缀都翻译成 product.<字段> 自报，于是模板保存时
// 由 builder.ValidateFieldRefs 按实体类型注册表校验、构建期再由商品解析器拒绝一次越界字段
// （不变量 4）—— 组件里不另写一份白名单，也就不会出现两份白名单漂移。
//
// 槽位用自由文本（string 控件）而不是 bindingfield 下拉：与 core.product（#6）同一取舍 ——
// 下拉的可选项取决于工作台当下拿到的数据源元数据，而本组件要同时服务「集合项作用域」与
// 「当前实体」两个场景，自由文本 + 两道服务端白名单校验更稳，也不会因为下拉取不到值而配不出来。
//
// 确定性：样式全部由编译期算好写进静态 CSS，模板只做拼装；同 props 同字节。
package productcard

import (
	_ "embed" // productcard.css 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.productCard"

// 缺省值。
const (
	defaultCurrency = "¥"
	// defaultTitleTag 卡片标题默认 h3：卡片几乎总在区块标题（h2）之下，
	// 默认 h3 才不会造成标题跳级（h1 → h3 对 SEO 与读屏都是噪音）。
	defaultTitleTag = "h3"
)

// fieldPathRe 槽位字段路径：item.<字段> 或 product.<字段>。
//
// 字段名形状与实体字段一致（小写字母开头 + 小写字母数字下划线）；前缀白名单只有两个 ——
// 别的数据源（分类 / 品牌）要出现在卡片上时，应由它们各自的组件承担，而不是让卡片变万能。
var fieldPathRe = regexp.MustCompile(`^(item|product)\.[a-z][a-zA-Z0-9_]*$`)

// Props core.productCard 属性：命名槽位声明本卡片要显示的商品字段。
type Props struct {
	// ImageField 主图字段（如 item.images / product.defaultImage）。
	ImageField string `json:"imageField,omitempty" ct:"string,maxlen=60,sec=content,label=主图字段"`
	// ImageAltField 主图 alt 字段（如 item.imageAlt）；留空用标题兜底。
	ImageAltField string `json:"imageAltField,omitempty" ct:"string,maxlen=60,sec=content,label=主图 alt 字段"`
	// TitleField 标题字段（如 item.name）。
	TitleField string `json:"titleField,omitempty" ct:"string,maxlen=60,sec=content,label=标题字段"`
	// PriceField 价格字段（如 item.priceRange / item.price）。
	PriceField string `json:"priceField,omitempty" ct:"string,maxlen=60,sec=content,label=价格字段"`
	// ComparePriceField 划线价字段（如 item.comparePrice）。
	ComparePriceField string `json:"comparePriceField,omitempty" ct:"string,maxlen=60,sec=content,label=划线价字段"`
	// TagsField 标签字段（如 item.tags，JSON 名称数组）。
	TagsField string `json:"tagsField,omitempty" ct:"string,maxlen=60,sec=content,label=标签字段"`
	// LinkField 链接字段（如 item.slug）：与 LinkPrefix 拼成卡片链接。
	LinkField string `json:"linkField,omitempty" ct:"string,maxlen=60,sec=content,label=链接字段"`
	// LinkPrefix 链接前缀（如 /products/）；留空表示 LinkField 已是完整地址。
	LinkPrefix string `json:"linkPrefix,omitempty" ct:"text,maxlen=200,sec=content,label=链接前缀"`
	// Currency 货币符号（价格槽位前缀；留空用默认符号）。
	Currency string `json:"currency,omitempty" ct:"text,maxlen=8,sec=content,label=货币符号"`
	// TitleTag 标题标签层级（h2~h4，默认 h3）。
	TitleTag string `json:"titleTag,omitempty" ct:"select,h2=二级标题,h3=三级标题,h4=四级标题,default=h3,sec=content,label=标题层级"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Component 商品卡组件：Atom 基座（校验/Advanced/可翻译） + 字段绑定自报。
type Component struct {
	core.Atom[Props]
}

// Widget 组件实例。
var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{TypeName: Type, ValidateExtra: validateExtra},
}}

// slotField 一个命名槽位与其声明的字段路径。
type slotField struct {
	Slot  string
	Field string
}

// 槽位名常量（视图与错误提示共用）。
const (
	slotImage        = "image"
	slotImageAlt     = "imageAlt"
	slotTitle        = "title"
	slotPrice        = "price"
	slotComparePrice = "comparePrice"
	slotTags         = "tags"
	slotLink         = "link"
)

// slotFields 按渲染顺序给出全部槽位（含空槽位，便于统一遍历）。
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

// validateExtra 关系性校验：至少声明一个展示字段，槽位路径形状合法，标题层级在白名单内。
//
// 「至少一个字段」是刻意的：一张什么字段都没声明的商品卡渲染出来是空盒子，
// 与其让它发布出去，不如在保存时就拒绝。
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

// FieldBindings 实现 core.FieldBindingProvider：自报本节点声明的商品字段绑定。
//
// item.<字段> 与 product.<字段> 都翻译成 product.<字段> —— 集合项作用域取的就是商品字段，
// 白名单是同一份（product contract）。合法性由 builder.ValidateFieldRefs 按注册表统一判定，
// 组件只负责「声明了什么」。
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
		// 前缀在 validateExtra 已收紧为 item / product，这里再判定一次是防「未走校验的写入路径」。
		if prefix != "item" && prefix != "product" {
			return nil, fmt.Errorf("无效的字段前缀 %q（只接受 item. 或 product.）", s.Field)
		}
		refs = append(refs, core.FieldRef{EntityType: "product", Field: field})
	}
	return refs, nil
}

// effectiveCurrency 有效货币符号（空取默认符号）。
func effectiveCurrency(p *Props) string {
	if p == nil || strings.TrimSpace(p.Currency) == "" {
		return defaultCurrency
	}
	return p.Currency
}

// effectiveTitleTag 有效标题层级（默认 h3）。
func effectiveTitleTag(p *Props) string {
	switch p.TitleTag {
	case "h2", "h3", "h4":
		return p.TitleTag
	default:
		return defaultTitleTag
	}
}

// compileCSS 商品卡样式：竖排卡片（图 / 标题 / 价格 / 标签），容器级自适应。
//
// productCardCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组，
// 而作用域替换、桶划分（含 @hover / @hovernone / @active）与确定性输出仍由构建期负责。
//
//go:embed productcard.css
var productCardCSS string

// compileCSS 商品卡样式（纯静态，与 Props 无关）。
func compileCSS(id string, _ *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, productCardCSS); err != nil {
		panic(fmt.Sprintf("productCard 组件样式解析失败: %v", err))
	}
}

func init() {
	core.Register(Widget)
}
