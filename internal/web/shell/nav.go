package shell

// nav.go — 后台侧栏视图构建 + 子页面路径归并。
//
// 菜单真源是 sys_menus 表：PermContextMiddleware 取到当前用户有效权限码后，由 admin
// 模块的 BuildAuthorizedTree 按权限过滤返回授权树（自动补齐祖先目录、排除按钮 / iframe /
// 外链）。本文件只做三件事：
//
//  1. 把菜单树转成侧栏渲染视图（一级项 + 二级递归节点）；
//  2. 用 NavPathFor 归一化当前页路径后标记高亮，并计算父级展开态；
//  3. 丢掉 is_hidden=1 的条目 —— 它在表里仍参与角色授权，只是不进侧栏。
//
// 增删改菜单：改表（写迁移，或在「菜单管理」页编辑），不改代码。
// 图标名取自 sys_menus.icon（Lucide 名），经 core.IconSVG 渲染，模板不做名称映射。

import (
	"go_wp/internal/builder/core"

	admindto "go_wp/internal/module/admin/dto"
	"go_wp/pkg/pathkit"
)

// NavNode 二级栏节点（模板 partials/nav-nodes.html 消费）。
type NavNode struct {
	Title    string
	Path     string
	Children []NavNode
	Active   bool
	Open     bool
}

// NavGroup 一级项（模板 partials/sidebar.html 消费）。
//
// 两种形态，由 Nodes 是否为空区分（模板据此渲染 button 或 a）：
//   - 目录：Nodes 非空 → 点击展开二级栏；
//   - 直接链接：Nodes 为空且有 Path → 点击直接导航（如「仪表盘」）。
//
// ID 进 DOM 的 data-group，供 admin.js 在一级图标与二级栏之间做配对切换；
// 它来自 sys_menus.id，所以菜单表里换个顺序也不影响前端配对。
type NavGroup struct {
	ID    uint64
	Title string
	// Icon 是 sys_menus.icon 里的 Lucide 图标名，IconSVG 是它对应的内联 SVG
	// （名字查不到时为空串，模板退回默认图标）。
	Icon     string
	IconSVG  string
	Path     string
	Nodes    []NavNode
	Active   bool
	FirstURL string
}

// BuildNav 把授权菜单树转成侧栏视图；currentPath 为**已归一化**的当前页路径。
//
// t 为按当前语言取词函数：菜单标题优先取 title_key 译文，未配置或未命中回退 title 原文。
func BuildNav(nodes []admindto.MenuTreeNode, currentPath string, t func(key, fallback string) string) []NavGroup {
	out := make([]NavGroup, 0, len(nodes))
	for _, n := range nodes {
		if n.IsHidden == 1 {
			continue
		}
		iconSVG, _ := core.IconSVG(n.Icon)
		g := NavGroup{
			ID:      n.ID,
			Title:   t(n.TitleKey, n.Title),
			Icon:    n.Icon,
			IconSVG: iconSVG,
			Path:    n.Path,
		}
		g.Nodes = buildNavNodes(n.Children, currentPath, t)
		if len(g.Nodes) > 0 {
			// 目录：自身不参与导航，高亮与跳转目标都由子节点决定。
			g.Active = containsActive(g.Nodes)
			g.FirstURL = firstNavPath(g.Nodes)
		} else {
			g.Active = n.Path != "" && pathkit.MatchKey(n.Path) == currentPath
		}
		// 既没有可见子节点、自身又没有路径的目录不渲染：
		// 否则 rail 上会留下一个点不开的空图标。
		if len(g.Nodes) == 0 && g.Path == "" {
			continue
		}
		out = append(out, g)
	}
	return out
}

// buildNavNodes 递归构建二级节点。
func buildNavNodes(nodes []admindto.MenuTreeNode, currentPath string, t func(key, fallback string) string) []NavNode {
	out := make([]NavNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsHidden == 1 {
			continue
		}
		nn := NavNode{
			Title: t(n.TitleKey, n.Title),
			Path:  n.Path,
		}
		nn.Children = buildNavNodes(n.Children, currentPath, t)
		nn.Active = nn.Path != "" && pathkit.MatchKey(nn.Path) == currentPath
		// 纯容器且子节点全被过滤 → 自身也不显示。
		if nn.Path == "" && len(nn.Children) == 0 {
			continue
		}
		nn.Open = nn.Active || containsActive(nn.Children)
		out = append(out, nn)
	}
	return out
}

// firstNavPath 深度优先找第一个有页面路径的节点
// （无二级栏渲染时，一级图标点击直接跳这里）。
func firstNavPath(nodes []NavNode) string {
	for _, n := range nodes {
		if n.Path != "" {
			return n.Path
		}
		if p := firstNavPath(n.Children); p != "" {
			return p
		}
	}
	return ""
}

// containsActive 子树里是否有当前页。
func containsActive(nodes []NavNode) bool {
	for _, n := range nodes {
		if n.Active || containsActive(n.Children) {
			return true
		}
	}
	return false
}

// navPathAlias 子页面路径 → 所属菜单项路径（侧边栏高亮用）。
//
// 导航树按「菜单项路径 == 当前路径」精确高亮；挂载在某个菜单项下面的子页面
// （入口在该页列表行内，不单独占菜单）不在树里，不映射的话打开子页面时侧边栏整组失去高亮。
//
// 这里只放**子页面**的归属：正式菜单项（哪怕路径不在 /admin 下，如 /api/page/redirect）
// 一律由表自己表达。两类混在一张表里会让「这一页该高亮谁」变得不可推理 ——
// 退货入库曾是正式菜单项却也被映射到 /admin/orders，代价是点它时高亮的是别人。
var navPathAlias = map[string]string{
	"/admin/page/translations":     "/admin/pages",
	"/admin/products/translations": "/admin/products",
	// 文章编辑页挂在「文章」列表下（入口在列表行内，不单独占菜单）。
	"/admin/articles/edit":         "/admin/articles",
	"/admin/articles/translations": "/admin/articles",
	// 客户详情挂在「客户管理」列表下（从列表行点进去，不单独占菜单）。
	"/admin/customers/detail": "/admin/customers",
	// 以下三个是「列表页 + 独立编辑页」的形态：编辑页不单独占菜单项。
	"/admin/themes/settings":        "/admin/themes",
	"/admin/datarules/edit":         "/admin/datarules",
	"/admin/content-templates/edit": "/admin/content-templates",
	// 导航菜单翻译工作台属「导航菜单」列表的子页面。
	"/admin/navigations/translations": "/admin/navigations",
}

// NavPathAlias 返回子页面归属表的副本（只读，供路径归一化契约测试断言）。
func NavPathAlias() map[string]string {
	out := make(map[string]string, len(navPathAlias))
	for k, v := range navPathAlias {
		out[k] = v
	}
	return out
}

// NavPathFor 返回用于导航高亮的路径：先归一成匹配键（去首尾空白与尾部斜杠），再映射别名。
//
// 匹配键的唯一实现是 pkg/pathkit.MatchKey（审计 CQ-012）：这里过去自己写了「去尾部
// 斜杠」，与后台之外几处（pipeline / publication / 构建期 nav）各有一份，对空路径与
// 根路径的处理互不相同。本处行为与改造前逐字等价。
func NavPathFor(raw string) string {
	path := pathkit.MatchKey(raw)
	if alias, ok := navPathAlias[path]; ok {
		return alias
	}
	return path
}
