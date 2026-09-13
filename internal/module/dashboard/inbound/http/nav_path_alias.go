package dashboardhttp

// nav_path_alias.go — 子页面 → 所属菜单路径（侧边栏高亮用）。
//
// 导航树（nav_menu.go 的 navConfig）按「菜单项路径 == 当前路径」精确高亮；
// 挂载在某个菜单项下面的子页面（入口在该页列表行内，不单独占菜单）不在树里，
// 不映射的话打开子页面时侧边栏整组失去高亮。
//
// 多语言 P5c：翻译工作台 /admin/page/translations 属「页面」列表（/admin/pages）的子页面。
// 多语言（issue #12）：商品翻译工作台 /admin/products/translations 属「商品」列表的子页面。

import "strings"

// navPathAlias 子页面路径 → 所属菜单项路径。
var navPathAlias = map[string]string{
	"/admin/page/translations":     "/admin/pages",
	"/admin/products/translations": "/admin/products",
	// 退货入库挂在「订单」菜单下（它是订单的售后环节，不单独占一级菜单）。
	"/admin/returns": "/admin/orders",
	// 文章编辑页挂在「文章」列表下（入口在列表行内，不单独占菜单）。
	"/admin/articles/edit": "/admin/articles",
	// 客户详情挂在「客户管理」列表下（从列表行点进去，不单独占菜单）。
	"/admin/customers/detail": "/admin/customers",
}

// navPathFor 返回用于导航高亮的路径：先按既有规则归一（去尾斜杠），再映射别名。
func navPathFor(raw string) string {
	path := normalizePath(strings.TrimSpace(raw))
	if alias, ok := navPathAlias[path]; ok {
		return alias
	}
	return path
}
