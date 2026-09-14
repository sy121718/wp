package pipeline

// navigation_adapter.go — navigation 契约 → builder.NavigationResolver（EDT-003）。

import (
	"context"

	"go_wp/internal/builder/core"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	projectcontract "go_wp/internal/module/project/contract"
)

// NavigationAdapter 构建期导航菜单解析（单次编译内缓存）。
type NavigationAdapter struct {
	Svc       navigationcontract.NavigationService
	Project   projectcontract.ProjectService
	Ctx       context.Context
	ProjectID string
	Lang      string
	cache     map[string][]core.NavigationItem
}

// ResolveMenu 按工程 + 位置返回菜单项树。
func (a *NavigationAdapter) ResolveMenu(projectID, kind string) (items []core.NavigationItem, err error) {
	key := projectID + "|" + kind
	if a.cache != nil {
		if cached, ok := a.cache[key]; ok {
			return cached, nil
		}
	}
	nodes, err := a.Svc.Tree(a.Ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	items = a.itemsOf(nodes, projectID)
	if a.cache == nil {
		a.cache = map[string][]core.NavigationItem{}
	}
	a.cache[key] = items
	return items, nil
}

func (a *NavigationAdapter) itemsOf(nodes []*navigationdto.NavigationNode, projectID string) []core.NavigationItem {
	out := make([]core.NavigationItem, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, core.NavigationItem{
			Label:    n.Title,
			URL:      LocalizeMenuURL(a.Ctx, a.Project, projectID, a.Lang, n.Path),
			Target:   n.Target,
			Children: a.itemsOf(n.Children, projectID),
		})
	}
	return out
}
