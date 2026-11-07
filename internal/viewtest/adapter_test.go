package viewtest

import (
	admindto "go_wp/internal/module/admin/dto"
	view "go_wp/pkg/view"
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
