// Package addtocart — Jet 渲染路径（视图组装）。
//
// 规格标签与变体行的组装**复用 core.product 导出的同名函数**：加购按钮旁边的
// 「红色 · L ¥99.00」必须与详情页规格选择器、商品卡上显示的完全一致 ——
// 各写一份解析，迟早在某一次字段改动后分叉，而分叉的表现为「按钮加购的是另一档规格」。
package addtocart

import (
	"fmt"
	"net/url"
	"strings"

	product "go_wp/internal/builder/components/product"
	"go_wp/internal/builder/core"
)

// Row 当前商品的一项可购买规格，不携带作者指定商品或价格展示配置。
type Row struct{ VariantID, Label string }

// View 加购组件渲染视图。
type View struct {
	// UnavailableText 为当前合法商品无可购买规格时的静态禁用提示。
	UnavailableText string
	CartReadURL     string

	// Action 公开访客加购 API。
	Action string
	// ProjectID 站点工程 id（构建上下文提供）。
	ProjectID string
	// Lang 本次编译的目标语言（core.RenderContext.Lang），输出为表单 hidden input。
	//
	// 空语言（单语言站点）不输出该域。
	Lang string
	// Label 按钮文字（参与内容翻译）。
	Label string
	// QtyAria 数量输入框的无障碍名（构建期按当前语言填充，多语言 P4）。
	//
	// 此前硬编码在模板里（aria-label="数量"）：读屏器在英文站点上念中文，
	// 而页面上根本看不到这串字 —— 只有看产物源码或用读屏器才会发现。
	QtyAria string
	// ShowQuantity 是否输出数量输入。
	ShowQuantity bool
	// Rows 加购行。
	Rows []Row
	// ChooseVariant 为真时由买家选择规格，未选不提交。
	ChooseVariant bool
	VariantAria   string
}

// 界面文案键（多语言 P5b）。
//
// 与 Translatable 的分工：作者**没填**按钮文字时用这里的译文兜底；
// 作者填了自定义文案，那条文案属于内容，走 sys_translation 的内容翻译链路。
// 两条链路各管一段，互不覆盖。
const (
	TextKeyButton      = "site.component.addToCart.button"
	TextKeyUnavailable = "site.component.addToCart.unavailable"
	// TextKeyQtyAria 数量输入框的无障碍名（模板里此前硬编码「数量」）。
	TextKeyQtyAria     = "site.component.addToCart.qtyAria"
	TextKeyVariantAria = "site.component.addToCart.variantAria"
)

// 中文兜底：取词函数为 nil（未接入 i18n）时用这些值，产物与接入前逐字一致。
const (
	// textFallbackButton 按钮文字的中文兜底。
	textFallbackButton = defaultLabel
	// textFallbackQtyAria 数量输入框无障碍名的中文兜底。
	textFallbackQtyAria = "数量"
)

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
//
// 三段**各自独立**判定，不能整函数早退：按钮文字「作者是否自定义」与数量框
// 无障碍名「有没有译文」是两件事 —— 早退会让作者一填自定义按钮文字，
// aria-label 就永远是中文（读屏器在英文站点念中文，页面上看不出来）。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {
		// BuildView 已落中文兜底；这里补上零值构造（测试直连 View）的情况。
		v.QtyAria = textFallbackQtyAria
		return
	}
	if v.UnavailableText != "" {
		v.UnavailableText = text(TextKeyUnavailable, "当前商品暂不可购买")
	}
	v.QtyAria = text(TextKeyQtyAria, textFallbackQtyAria)
	v.VariantAria = text(TextKeyVariantAria, "请选择规格")
	// 按钮文字：作者填过的那条走内容翻译（sys_translation），这里只管缺省值。
	if v.Label == textFallbackButton {
		v.Label = text(TextKeyButton, textFallbackButton)
	}

}

// CompileCSS 导出样式编译（复用组件内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// BuildView 生成加购视图。
//
// content 是当前详情商品的构建期解析器，字段固定，不接受作者查询或其它商品引用。
//
// lang 为本次编译的目标语言，随静态表单保留。
func BuildView(p *Props, content core.ContentResolver, projectID, lang string) (View, error) {
	view := View{
		Action:       CartAddPath,
		ProjectID:    strings.TrimSpace(projectID),
		Lang:         strings.TrimSpace(lang),
		CartReadURL:  "/api/site/cart?" + url.Values{"projectId": {strings.TrimSpace(projectID)}}.Encode(),
		Label:        effectiveLabel(p),
		QtyAria:      textFallbackQtyAria,
		ShowQuantity: p.ShowQuantity,
		VariantAria:  "请选择规格",
	}
	if content == nil {
		return View{}, fmt.Errorf("%w: 请将加购按钮放在商品详情中，按钮会自动使用当前商品", core.ErrIncompleteNode)
	}
	if view.ProjectID == "" {
		return View{}, fmt.Errorf("%w: 当前商品缺少站点工程", core.ErrIncompleteNode)
	}

	rawOptions, err := content.ResolveString("product.options")
	if err != nil {
		return View{}, fmt.Errorf("解析规格维度字段 %q 失败: %w", "product.options", err)
	}
	rawVariants, err := content.ResolveString("product.variants")
	if err != nil {
		return View{}, fmt.Errorf("解析变体组合字段 %q 失败: %w", "product.variants", err)
	}

	groups := product.ParseOptionGroups(rawOptions)
	options := product.ParseVariantOptions(rawVariants, groups, "", view.ProjectID, view.Lang)
	if len(options) == 0 {
		view.UnavailableText = "当前商品暂不可购买"
		return view, nil
	}
	for _, option := range options {
		view.Rows = append(view.Rows, Row{VariantID: option.ID, Label: option.Labels})
	}
	view.ChooseVariant = len(view.Rows) > 1
	return view, nil
}

// DeclareFeatures 只为可提交表单登记 JSON 读写与渐进启用基座。
func (v View) DeclareFeatures() (attrs, classes []string) {
	if len(v.Rows) == 0 {
		return nil, nil
	}
	return []string{"data-site-read", "data-site-cart", "data-write", "data-requires-js", "data-component-instance"}, nil
}
