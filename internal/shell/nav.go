package shell

// nav.go — 后台侧栏视图构建 + 子页面路径归并。
//
// 菜单真源是 sys_menus 表：PermContextMiddleware 取到当前用户有效权限码后，由 admin
// 模块的 BuildAuthorizedTree 按权限过滤返回授权树（自动补齐祖先目录、排除按钮 / iframe /
// 外链）。本文件只做三件事：
//
//  1. 把菜单树转成侧栏渲染视图（一级项 + 二级递归节点）；
//  2. 把当前页路径归属到最具体的那个菜单项（按路径段最长前缀，见 navOwnerPath）后标记高亮；
//  3. 丢掉 is_hidden=1 的条目 —— 它在表里仍参与角色授权，只是不进侧栏。
//
// 增删改菜单：改表（写迁移，或在「菜单管理」页编辑），不改代码。
// 图标名取自 sys_menus.icon（Lucide 名），经 core.IconSVG 渲染，模板不做名称映射。

import (
	"strings"

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

// BuildNav 把授权菜单树转成侧栏视图；currentPath 为当前页原始路径（本函数内部归一化）。
//
// 高亮的判据是「当前路径按路径段归属到哪个菜单项」（navOwnerPath），归属到谁就高亮谁：
// 子页面（入口在列表行内、不单独占菜单）自动归属到它的父菜单，不需要任何声明表。
//
// t 为按当前语言取词函数：菜单标题优先取 title_key 译文，未配置或未命中回退 title 原文。
func BuildNav(nodes []admindto.MenuTreeNode, currentPath string, t func(key, fallback string) string) []NavGroup {
	current := pathkit.MatchKey(currentPath)
	owner := navOwnerPath(nodes, current)
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
		g.Nodes = buildNavNodes(n.Children, owner, t)
		if len(g.Nodes) > 0 {
			// 目录：自身不参与导航，高亮与跳转目标都由子节点决定。
			g.Active = containsActive(g.Nodes)
			g.FirstURL = firstNavPath(g.Nodes)
		} else {
			g.Active = n.Path != "" && pathkit.MatchKey(n.Path) == owner
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

// buildNavNodes 递归构建二级节点；owner 为当前页归属的菜单路径（见 navOwnerPath）。
func buildNavNodes(nodes []admindto.MenuTreeNode, owner string, t func(key, fallback string) string) []NavNode {
	out := make([]NavNode, 0, len(nodes))
	for _, n := range nodes {
		if n.IsHidden == 1 {
			continue
		}
		nn := NavNode{
			Title: t(n.TitleKey, n.Title),
			Path:  n.Path,
		}
		nn.Children = buildNavNodes(n.Children, owner, t)
		nn.Active = nn.Path != "" && pathkit.MatchKey(nn.Path) == owner
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

// navOwnerPath 找出「当前路径属于哪个菜单项」：返回菜单树里**最长的**、按**路径段**
// 前缀命中当前路径的那个菜单路径（命中不了返回空串）。
//
// 为什么按路径段而不是字符串前缀：`/admin/mail/campaign` 是 `/admin/mail/campaigns`
// 的字符串前缀，按字符比会让打开 campaigns 页时高亮到 campaign。按段比（相等，或后面
// 紧跟 "/"）才正确。
//
// 为什么取最长：`/admin/mail` 与 `/admin/mail/campaigns` 都是菜单项，打开后者时只能
// 高亮后者 —— 取最长即「最具体的那个菜单」。
//
// 这条规则替代了原先手写的 `navPathAlias`（11 条子页面 → 父菜单映射）：子页面 URL
// 只要挂在父菜单 URL 的路径段下就自动归属，不需要任何声明。原先唯一不满足的是
// 页面翻译工作台（旧路径 `/admin/page/translations` 与父菜单 `/admin/pages` 单复数
// 不一致），已随本批改名成 `/admin/pages/translations`。
//
// 隐藏项（is_hidden=1）不参与归属：它们不进侧栏，认了它们就会出现「高亮指向一个
// 看不见的菜单」。
func navOwnerPath(nodes []admindto.MenuTreeNode, current string) string {
	owner := ""
	var walk func([]admindto.MenuTreeNode)
	walk = func(list []admindto.MenuTreeNode) {
		for _, n := range list {
			if n.IsHidden != 1 {
				if p := pathkit.MatchKey(n.Path); p != "" && isSegmentPrefix(p, current) && len(p) > len(owner) {
					owner = p
				}
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return owner
}

// isSegmentPrefix 判断 prefix 是否是 full 的「路径段前缀」（相等，或后面紧跟 "/"）。
func isSegmentPrefix(prefix, full string) bool {
	return full == prefix || strings.HasPrefix(full, prefix+"/")
}
