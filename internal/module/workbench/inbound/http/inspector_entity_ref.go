package workbenchhttp

import (
	"context"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

// entityRefInspectorOptions 把集合源可选筛选项转成检查器下拉（EDT-005）。
//
// tr 是「key → 当前语言文案」的取词函数（workbenchTrFunc）：空选项与导航位置的
// 标签都会直接进面板 HTML，模板层不参与，所以取词在这里完成。
func entityRefInspectorOptions(ctx context.Context, h *Handle, projectID, refKind, selected string, tr func(key string) string) []inspectorOption {
	out := []inspectorOption{{Value: "", Label: tr(workbenchenums.InspectorNavAny), Selected: selected == ""}}
	// 导航菜单项不是集合筛选项（不在商品数据源里），单独走导航模块的列表端口。
	if refKind == "navigation" {
		return navigationInspectorOptions(ctx, h, projectID, selected, tr)
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
