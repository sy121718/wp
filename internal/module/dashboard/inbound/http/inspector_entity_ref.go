package dashboardhttp

import (
	"context"

	"go_wp/internal/builder/core"
)

// entityRefInspectorOptions 把集合源可选筛选项转成检查器下拉（EDT-005）。
func entityRefInspectorOptions(ctx context.Context, h *Handle, projectID, refKind, selected string) []inspectorOption {
	out := []inspectorOption{{Value: "", Label: "（不限）", Selected: selected == ""}}
	if h == nil || h.products == nil || projectID == "" {
		return out
	}
	provider, ok := h.products.(core.CollectionFilterOptionsProvider)
	if !ok {
		return out
	}
	opts, err := provider.CollectionFilterOptions(ctx, projectID)
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
