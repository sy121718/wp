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
