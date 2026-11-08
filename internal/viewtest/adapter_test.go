package viewtest

import (
	admindto "go_wp/internal/module/admin/dto"
	view "go_wp/pkg/view"
	jetpkg "go_wp/pkg/view/jet"
)

// navNodes 复刻应用装配层的菜单树投影（routers.view_admin_port.go 的同一逻辑），
// 供导航测试用真实菜单类型构造输入。
func navNodes(nodes []admindto.MenuTreeNode) []view.AdminNavNode {
	out := make([]view.AdminNavNode, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		out = append(out, view.AdminNavNode{
			ID: n.ID, Title: n.Title, TitleKey: n.TitleKey,
			Path: n.Path, Icon: n.Icon,
			Hidden:   n.IsHidden == 1,
			Children: navNodes(n.Children),
		})
	}
	return out
}

// navItemsOf 复刻应用装配层的菜单投影（routers.view_scope_port.go 的同一逻辑），
// 供导航测试用真实菜单类型构造模板可递归的导航事实。
func navItemsOf(nodes []view.AdminNavNode) []jetpkg.NavItem {
	out := make([]jetpkg.NavItem, 0, len(nodes))
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		item := jetpkg.NavItem{
			ID: n.ID, Title: n.Title, TitleKey: n.TitleKey,
			Path: n.Path, Icon: n.Icon,
		}
		item.Children = navItemsOf(n.Children)
		if len(item.Children) == 0 && item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}
