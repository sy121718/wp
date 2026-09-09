package pageservice

// page_navigation.go — 导航装配：navigation 契约 → builder 的 core.NavigationResolver。
// core.nav 节点绑定菜单位置（header/footer）时，构建期经此把导航记录解析为静态
// 菜单项树；产物仍是纯静态 HTML（访客请求零查库，docs/01 §1 不变量）。
// 与 core.globalref 的 blockResolverAdapter 同源：跨模块只依赖 contract，
// 本模块负责把契约数据适配成内核接口。

import (
	"context"
	"strings"

	"go_wp/internal/builder/core"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
)

// navigationResolverAdapter 适配 navigation 契约为 builder 的 NavigationResolver。
// cache 为单次编译内的解析缓存（键：工程 ID + 位置），同一页面多个导航节点只查一次库。
type navigationResolverAdapter struct {
	svc   navigationcontract.NavigationService
	ctx   context.Context
	cache map[string][]core.NavigationItem
}

// ResolveMenu 按工程 + 位置返回菜单项树（navigation 模块负责树组装与排序）。
func (a navigationResolverAdapter) ResolveMenu(projectID, kind string) (items []core.NavigationItem, err error) {
	key := projectID + "|" + kind
	if a.cache != nil {
		if cached, ok := a.cache[key]; ok {
			return cached, nil
		}
	}
	nodes, err := a.svc.Tree(a.ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	items = navigationItemsOf(nodes)
	if a.cache != nil {
		a.cache[key] = items
	}
	return items, nil
}

// navigationItemsOf 树节点 → 构建期菜单项（递归展开子菜单）。
func navigationItemsOf(nodes []*navigationdto.NavigationNode) []core.NavigationItem {
	out := make([]core.NavigationItem, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, core.NavigationItem{
			Label:    n.Title,
			URL:      n.Path,
			Target:   n.Target,
			Children: navigationItemsOf(n.Children),
		})
	}
	return out
}

// pageContextOf 按页面 ID 取所属站点工程 ID 与访问路径。
// 工程 ID 用于导航解析（缺失时绑定菜单位置的导航节点编译期显式报错）；
// 访问路径用于导航「当前项」高亮（优先激活路径，未发布用草稿路径）。
func (s *Service) pageContextOf(ctx context.Context, pageID string) (projectID, currentPath string) {
	if strings.TrimSpace(pageID) == "" {
		return "", ""
	}
	page, err := s.model.GetByID(ctx, pageID)
	if err != nil || page == nil {
		return "", ""
	}
	currentPath = page.DraftPath
	if page.ActivePath != nil && *page.ActivePath != "" {
		currentPath = *page.ActivePath
	}
	return page.ProjectID, currentPath
}
