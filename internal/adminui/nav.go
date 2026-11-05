package adminui

import (
	admindto "go_wp/internal/module/admin/dto"
	jetpkg "go_wp/pkg/view/jet"
)

// NavItemsOf 把授权菜单树投影成导航原始事实（不做激活/展开/首链接派生）。
//
// 展示派生由公共模板函数按当前路径计算；后端只交原始菜单与请求事实。
func NavItemsOf(nodes []admindto.MenuTreeNode) []jetpkg.NavItem {
	out := make([]jetpkg.NavItem, 0, len(nodes))
	for _, n := range nodes {
		if n.IsHidden == 1 {
			continue
		}
		item := jetpkg.NavItem{
			ID: n.ID, Title: n.Title, TitleKey: n.TitleKey,
			Path: n.Path, Icon: n.Icon,
		}
		item.Children = NavItemsOf(n.Children)
		if len(item.Children) == 0 && item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
