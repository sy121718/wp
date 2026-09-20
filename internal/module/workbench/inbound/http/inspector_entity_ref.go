package workbenchhttp

import (
	"context"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
)

// entityRefInspectorOptions 把集合源可选筛选项转成检查器下拉（EDT-005）。
func entityRefInspectorOptions(ctx context.Context, h *Handle, projectID, refKind, selected string) []inspectorOption {
	out := []inspectorOption{{Value: "", Label: "（不限）", Selected: selected == ""}}
	// 导航菜单项不是集合筛选项（不在商品数据源里），单独走导航模块的列表端口。
	if refKind == "navigation" {
		return navigationInspectorOptions(ctx, h, projectID, selected)
	}
	if h == nil || h.products == nil || projectID == "" {
		return out
	}
	provider, ok := h.products.(core.CollectionFilterOptionsProvider)
	if !ok {
		return out
	}
	// 检查器持有的就是商品数据源：源标识用 contract 常量，不写第二份字面量。
	opts, err := provider.CollectionFilterOptions(ctx, productcontract.CollectionSourceProduct, projectID)
	if err != nil {
		return out
	}
	var choices []core.CollectionFilterChoice
	switch refKind {
	case "category":
		choices = opts.Categories
	case "brand":
		choices = opts.Brands
	case "tag":
		choices = opts.Tags
	default:
		return out
	}
	for _, ch := range choices {
		out = append(out, inspectorOption{
			Value: ch.ID, Label: ch.Name, Selected: ch.ID == selected,
		})
	}
	return out
}
