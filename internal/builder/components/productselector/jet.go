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
	ShowStock bool
	// StockFallback 无脚本时的兜底文案。
	StockFallback string
	// StockFragmentPath 可用量片段端点。
	StockFragmentPath string

	// HasEmpty / EmptyText 无规格时的空态（文案留空则整块不输出）。
	HasEmpty  bool
	EmptyText string
}

// BuildView 生成规格选择器视图。
//
// content 为构建期注入的解析器（商品实体解析器）：两个槽位字段都必须能取到值，
// 取不到（越界字段 / 缺解析器）即报错终止构建 —— 静默渲染一个没有规格的选择器
// 比构建失败危险得多（页面上看不出少了什么）。
func BuildView(p *Props, content core.ContentResolver) (View, error) {
	if p == nil {
		return View{}, fmt.Errorf("规格选择器属性为空")
	}
	if content == nil {
		return View{}, fmt.Errorf("编译上下文缺少内容解析器，无法解析商品规格字段（数据源 product）")
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
	rows := product.ParseVariantOptions(strings.TrimSpace(variantsRaw), groups, EffectiveCurrency(p))

	view := View{
		OptionGroups:      groups,
		VariantOptions:    rows,
		HasOptions:        len(groups) > 0 && len(rows) > 1,
		ShowStock:         ShowStock(p),
		StockFallback:     StockFallback,
		StockFragmentPath: StockFragmentPath,
	}
	if !view.HasOptions {
		view.EmptyText = EffectiveEmptyText(p)
		view.HasEmpty = view.EmptyText != ""
	}
	return view, nil
}
