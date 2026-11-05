package addtocart

import (
	"fmt"
	"net/url"
	"strings"

	product "go_wp/internal/builder/extensions/product"
	"go_wp/pkg/builder/core"
)

type Row struct{ VariantID, Label string }

type View struct {
	UnavailableText string
	CartReadURL     string

	Action string

	ProjectID string

	Lang string

	Label string

	QtyAria string

	ShowQuantity bool

	Rows []Row

	ChooseVariant bool
	VariantAria   string
}

const (
	TextKeyButton      = "site.component.addToCart.button"
	TextKeyUnavailable = "site.component.addToCart.unavailable"

	TextKeyQtyAria     = "site.component.addToCart.qtyAria"
	TextKeyVariantAria = "site.component.addToCart.variantAria"
)

const (
	textFallbackButton = defaultLabel

	textFallbackQtyAria = "数量"
)

func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	if text == nil {

		v.QtyAria = textFallbackQtyAria
		return
	}
	if v.UnavailableText != "" {
		v.UnavailableText = text(TextKeyUnavailable, "当前商品暂不可购买")
	}
	v.QtyAria = text(TextKeyQtyAria, textFallbackQtyAria)
	v.VariantAria = text(TextKeyVariantAria, "请选择规格")

	if v.Label == textFallbackButton {
		v.Label = text(TextKeyButton, textFallbackButton)
	}

}

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

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

func (v View) DeclareFeatures() (attrs, classes []string) {
	if len(v.Rows) == 0 {
		return nil, nil
	}
	return []string{"data-site-read", "data-site-cart", "data-write", "data-requires-js", "data-component-instance"}, nil
}
