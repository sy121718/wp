package dashboardhttp

import "strings"

// 后台导航配置（唯一真源）。
//
// 为什么用代码配置而非直接读 sys_menus：
//  1. 菜单项与「页面路径 + 所需权限码」强绑定，代码里一目了然、随页面重构一起改；
//  2. 权限码与 sys_permission 对齐（seed 050 等），过滤逻辑与 Casbin 同源；
//  3. 需要「动态装菜单」（插件）时，把这份配置 seed 进 sys_menus 再由插件追加即可，
//     配置本身就是 seed 的源，不存在两套数据漂移。
//
// 可点击性由 Path 决定（非类型）：有 Path 则导航，有 Children 则展开，两者都有则
// 「点文字导航 + 点右侧三角展开」（partials/sidebar.html + admin.js 实现）。
type navNode struct {
	Title    string
	Path     string // 页面路径；空 = 纯容器（不可点击）
	Perm     string // 需要的权限码；空 = 登录即可见
	Children []navNode
	// 渲染态（由 buildNav 填充）
	Active bool
	Open   bool
}

type navGroup struct {
	Key      string
	Title    string
	Overview *navNode // 模块首页（主菜单自己的页面）
	Nodes    []navNode
	Active   bool
	// FirstURL 该组第一个可导航页面路径（无二级栏渲染时，点一级图标直接跳这里）。
	FirstURL string
}

// navConfig 后台导航树。Key 供模板选择图标。
//
// 一级项两种形态（由 Nodes 是否为空决定，模板据此渲染 <button> 或 <a>）：
//   - 目录：Nodes 非空 → 点击展开/收起二级栏（如「管理」「系统」）；
//   - 直接链接：Nodes 为空且 Overview 有 Path → 点击直接导航（如「仪表盘」）。
var navConfig = []navGroup{
	{
		// 仪表盘：一级项直接导航（Nodes 为空即「直接链接」形态，不展开二级栏）。
		Key:      "dashboard",
		Title:    "仪表盘",
		Overview: &navNode{Title: "仪表盘", Path: "/admin"},
	},
	{
		Key:   "manage",
		Title: "管理",
		Nodes: []navNode{
			{Title: "管理员", Path: "/admin/administrators", Perm: "admin:list"},
			{Title: "角色管理", Path: "/admin/roles", Perm: "role:list"},
			{Title: "菜单管理", Path: "/admin/menus", Perm: "menu:list"},
			{Title: "权限资源", Path: "/admin/permissions", Perm: "permission:list"},
			{Title: "部门管理", Path: "/admin/departments", Perm: "dept:list"},
			{Title: "数据权限", Path: "/admin/datarules", Perm: "datarule:list"},
		},
	},
	{
		Key:   "content",
		Title: "内容",
		Nodes: []navNode{
			// 文章（INF-1）：CMS 内容实体（contents，迁移 080 起只保留 article）。
			// 权限点用 content:list（迁移 033），与写操作的 content:create/update/delete 同源。
			{Title: "文章", Path: "/admin/articles", Perm: "content:list"},
			// 内容模板（EDT-001）：可视化编辑走 /workbench?template=…，列表在此。
			{Title: "内容模板", Path: "/admin/content-templates", Perm: "contenttemplate:list"},
		},
	},
	{
		Key:   "system",
		Title: "系统",
		Nodes: []navNode{
			{Title: "页面", Path: "/admin/pages", Perm: "page:list"},
			// 系统页面槽位：后台侧栏读的是这份 navConfig（迁移 140 seed 的 sys_menus 只用于权限菜单管理页）。
			{Title: "系统页面", Path: "/admin/site-slots", Perm: "page:site_slot_list"},
			// 订单与优惠码（BIZ-1）：权限点来自迁移 136（order:list）与 142（order:coupon_list）。
			{Title: "订单", Path: "/admin/orders", Perm: "order:list"},
			// 退货入库（RMA）：订单的售后环节，权限点来自迁移 145。
			{Title: "退货入库", Path: "/admin/returns", Perm: "order:return_list"},
			// 客户管理（访客账号的后台面）：权限点来自迁移 152（user:customer_list）。
			// 访客账号此前只有前台链路，后台看不到 —— 客户列表 / 按客户看订单 / 停用与解锁。
			{Title: "客户管理", Path: "/admin/customers", Perm: "user:customer_list"},
			{Title: "优惠码", Path: "/admin/coupons", Perm: "order:coupon_list"},
			{Title: "主题管理", Path: "/admin/themes", Perm: "project:theme_list"},
			{Title: "全局块", Path: "/admin/blocks", Perm: "block:list"},
			{Title: "导航菜单", Path: "/admin/navigations", Perm: "navigation:list"},
			{Title: "媒体库", Path: "/admin/media", Perm: "media:list"},
			{Title: "插件", Path: "/admin/plugins", Perm: "plugin:list"},
			{Title: "站点设置", Path: "/admin/settings", Perm: "project:detail"},
		},
	},
}

// buildNav 按权限码过滤导航树，并标记当前路径。
//
// 过滤规则：
//   - 节点有 Perm 且用户无该权限 → 剔除；
//   - 目录节点（无 Path）的子节点全部被剔除后，自身也剔除；
//   - 分组内概览与所有节点都不可见 → 整个分组不显示。
func buildNav(permSet map[string]bool, currentPath string) []navGroup {
	out := make([]navGroup, 0, len(navConfig))
	for _, g := range navConfig {
		ng := navGroup{Key: g.Key, Title: g.Title}
		if g.Overview != nil && allowed(permSet, g.Overview.Perm) {
			ov := *g.Overview
			ov.Active = ov.Path != "" && ov.Path == currentPath
			ng.Overview = &ov
		}
		ng.Nodes = filterNavNodes(g.Nodes, permSet, currentPath)
		if ng.Overview == nil && len(ng.Nodes) == 0 {
			continue
		}
		// 分组激活：当前页在本分组内（概览或任一子节点命中）。
		ng.Active = (ng.Overview != nil && ng.Overview.Active) || containsActive(ng.Nodes)
		// 第一个可导航页面：优先概览，其次深度优先找第一个叶子。
		if ng.Overview != nil && ng.Overview.Path != "" {
			ng.FirstURL = ng.Overview.Path
		} else {
			ng.FirstURL = firstNavPath(ng.Nodes)
		}
		out = append(out, ng)
	}
	return out
}

func filterNavNodes(nodes []navNode, permSet map[string]bool, currentPath string) []navNode {
	out := make([]navNode, 0, len(nodes))
	for _, n := range nodes {
		if !allowed(permSet, n.Perm) {
			continue
		}
		nn := navNode{Title: n.Title, Path: n.Path, Perm: n.Perm}
		nn.Children = filterNavNodes(n.Children, permSet, currentPath)
		nn.Active = nn.Path != "" && nn.Path == currentPath
		// 纯容器且子节点全被过滤 → 自身也不显示。
		if nn.Path == "" && len(nn.Children) == 0 {
			continue
		}
		nn.Open = nn.Active || containsActive(nn.Children)
		out = append(out, nn)
	}
	return out
}

func allowed(permSet map[string]bool, perm string) bool {
	if perm == "" {
		return true
	}
	return permSet[perm]
}

// firstNavPath 深度优先找第一个有页面路径的节点（供一级图标在无二级栏时跳转）。
func firstNavPath(nodes []navNode) string {
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

func containsActive(nodes []navNode) bool {
	for _, n := range nodes {
		if n.Active || containsActive(n.Children) {
			return true
		}
	}
	return false
}

// normalizePath 去掉尾部斜杠，供当前页匹配（/admin/pages/ 与 /admin/pages 视为同一页）。
func normalizePath(p string) string {
	if p == "" {
		return p
	}
	return strings.TrimRight(p, "/")
}
