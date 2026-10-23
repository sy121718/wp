package admindto

import "go_wp/pkg/pagination"

// admin_drawer_resp.go — 后台抽屉**片段**的渲染视图（htmx 局部替换）。
//
// 与整页视图分开：片段只有一个槽位，整页是一屏数据；两者失败形态不同。
// **导出字段名就是模板契约**：internal/templates/admin/system/*_drawer.html 的 `.Field`。

// DrawerSavedResp 写动作成功后的抽屉片段（模板只渲染一个「已保存」标记）。
type DrawerSavedResp struct {
	Saved bool
}

// AdminListPageResp 管理员列表整页（/admin/administrators）。
//
// 字段名 = 模板键。降级渲染（取数失败）时行与总数给零值、Err 给归口文案 ——
// 页面照常渲染，而不是把用户丢在空白上。
type AdminListPageResp struct {
	Title       string `view:"title"`
	Menu        string `view:"menu"`
	Rows        any
	Total       int64
	FilterName  string
	FilterEmail string
	Err         string
	// ListQuery 批量/表单动作 action 上的筛选上下文。
	ListQuery string

	Pagination pagination.Page
}

// RuleListPageResp 数据规则列表页（/admin/datarules）。
type RuleListPageResp struct {
	Title        string `view:"title"`
	Menu         string `view:"menu"`
	Rows         any
	Total        int64
	Domains      any
	FilterDomain string
	Err          string
	ListQuery    string

	Pagination pagination.Page
}

// DepartmentListPageResp 部门列表页（/admin/departments）。
type DepartmentListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string

	Pagination pagination.Page
}

// MenuListPageResp 菜单列表页（/admin/menus）。
//
// 列表是**树状分页**：Rows 已按 DFS 前序摊平（顶级行 + 其子树），层级用 Depth 表达、
// 初始可见性用 Hidden/Expanded 表达 —— 摊平与可见性都归 service，模板只渲染。
type MenuListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Parents       any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string
	// SearchMode 让模板切到搜索态（给命中行标「匹配」、给祖先标「上级路径」）。
	SearchMode bool
	// PermChoices 新建抽屉的权限点候选（新建时没有任何已绑码）。
	PermChoices any

	Pagination pagination.Page
}

// RoleListPageResp 角色列表页（/admin/roles）。
type RoleListPageResp struct {
	Title         string `view:"title"`
	Menu          string `view:"menu"`
	Rows          any
	Total         int64
	FilterKeyword string
	Err           string
	ListQuery     string

	Pagination pagination.Page
}
