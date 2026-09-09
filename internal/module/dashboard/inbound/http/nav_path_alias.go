package dashboardhttp

// nav_path_alias.go — 子页面 → 所属菜单路径（侧边栏高亮用）。
//
// 导航树（nav_menu.go 的 navConfig）按「菜单项路径 == 当前路径」精确高亮；
// 挂载在某个菜单项下面的子页面（入口在该页列表行内，不单独占菜单）不在树里，
// 不映射的话打开子页面时侧边栏整组失去高亮。
//
// 多语言 P5c：翻译工作台 /admin/page/translations 属「页面」列表（/admin/pages）的子页面。

import "strings"

// navPathAlias 子页面路径 → 所属菜单项路径。
var navPathAlias = map[string]string{
	"/admin/page/translations": "/admin/pages",
}

// navPathFor 返回用于导航高亮的路径：先按既有规则归一（去尾斜杠），再映射别名。
func navPathFor(raw string) string {
	path := normalizePath(strings.TrimSpace(raw))
	if alias, ok := navPathAlias[path]; ok {
		return alias
	}
	return path
}
