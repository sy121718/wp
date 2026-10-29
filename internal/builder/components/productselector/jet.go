// Package productselector — Jet 渲染路径（视图组装）。
//
// 规格维度的解析与组合行的组装都复用 core.product 导出的同名函数：
// 两处选择器（详情组件内置的 / 独立拖拽的）必须给出同一份结果，
// 否则同一个商品在两种模板里会显示成两套规格。
package productselector

import (
	"fmt"
	"strings"

	product "go_wp/internal/builder/components/product"
	"go_wp/internal/builder/core"
)

// View 规格选择器渲染视图（供 product_selector.jet 使用）。
type View struct {
	// HasOptions 是否有可切换的规格组合（有维度且组合 ≥2）——
	// 单变体商品不输出选择器（与 #8 的既有口径一致：那时候没有「切换」可言）。
	HasOptions bool
	// OptionGroups 规格维度（原生 radio 值组）。
	OptionGroups []product.OptionGroup
	// VariantOptions 规格组合行（SKU / 标签 / 价格 / 划线价 / 可选实时库存位）。
	VariantOptions []product.VariantOption

	// ShowStock 是否在每档组合挂实时可用量片段（构建期只烘变体 id，可用量现取）。
	//
	// 片段的**地址**不在这里：它随每档组合烘在 VariantOption.StockFactsURL 上
	// （与实时价格核对位同形）—— 地址里必须带工程 id，而工程只有构建期知道，
	// 由 product 包的拼装函数一处产出，两个组件共用同一份口径。
	ShowStock bool
	// StockFallback 无脚本时的兜底文案。
	Currency      string
	StockFallback string
	LiveLabels    product.LiveLabels

	// HasEmpty / EmptyText 无规格时的空态（文案留空则整块不输出）。
	HasEmpty  bool
	EmptyText string
}

// BuildView 生成规格选择器视图。
//
// content 为构建期注入的解析器（商品实体解析器）：两个槽位字段都必须能取到值，
// 取不到（越界字段 / 缺解析器）即报错终止构建 —— 静默渲染一个没有规格的选择器
// 比构建失败危险得多（页面上看不出少了什么）。
//
// projectID 显式写入只读数据 URL；文案由 ApplyI18n 在构建期冻结。
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

// DeclareFeatures 只有实际输出数据位时才注入读取脚本。
func (v View) DeclareFeatures() (attrs, classes []string) {
	// 按**实际烘出的 URL** 判断（不是「网格有规格就登记」）：缺工程 id 时两个片段位
	// 都是空串、模板不输出 hx-get，此时登记会让产物白白多注入一份 htmx 脚本。
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
