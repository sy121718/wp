package productselector

import (
	"fmt"
	"strings"

	product "go_wp/internal/builder/components/product"
	"go_wp/internal/builder/core"
)

type View struct {
	HasOptions bool

	OptionGroups []product.OptionGroup

	VariantOptions []product.VariantOption

	ShowStock bool

	Currency      string
	StockFallback string
	LiveLabels    product.LiveLabels

	HasEmpty  bool
	EmptyText string
}

func BuildView(p *Props, content core.ContentResolver, projectID, lang string) (View, error) {
	if p == nil {
		return View{}, fmt.Errorf("规格选择器属性为空")
	}
	if content == nil {
		return View{}, fmt.Errorf("%w: 当前页面没有商品，请选择指定商品", core.ErrIncompleteNode)
	}
	optionsField, variantsField := EffectiveOptionsField(p), EffectiveVariantsField(p)
	optionsRaw, err := content.ResolveString(optionsField)
	if err != nil {
		return View{}, fmt.Errorf("解析商品字段 %q 失败: %w", optionsField, err)
	}
	variantsRaw, err := content.ResolveString(variantsField)
	if err != nil {
		return View{}, fmt.Errorf("解析商品字段 %q 失败: %w", variantsField, err)
	}

	groups := product.ParseOptionGroups(strings.TrimSpace(optionsRaw))
	rows := product.ParseVariantOptions(strings.TrimSpace(variantsRaw), groups, EffectiveCurrency(p), projectID, lang)
	if len(rows) == 0 {
		return View{}, fmt.Errorf("%w: 商品暂无可用规格，请选择有效商品", core.ErrIncompleteNode)
	}

	view := View{
		OptionGroups:   groups,
		VariantOptions: rows,
		HasOptions:     len(groups) > 0 && len(rows) > 1,
		ShowStock:      ShowStock(p),
		Currency:       EffectiveCurrency(p),
		StockFallback:  StockFallback,
		LiveLabels:     product.TranslateLiveLabels(nil),
	}
	if !view.HasOptions {
		view.EmptyText = EffectiveEmptyText(p)
		view.HasEmpty = view.EmptyText != ""
	}
	return view, nil
}

func (v View) DeclareFeatures() (attrs, classes []string) {

	for _, vo := range v.VariantOptions {
		if (v.ShowStock && vo.StockFactsURL != "") || vo.PriceFactsURL != "" {
			return []string{"data-site-read", "data-site-product"}, nil
		}
	}
	return nil, nil
}

func (v *View) ApplyI18n(text func(string, string) string) {
	v.LiveLabels = product.TranslateLiveLabels(text)
	v.StockFallback = v.LiveLabels.Unknown
}
