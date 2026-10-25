package adminui

// page_pagination.go — 后台**旧式页面**（gin.H + c.HTML 直出）的分页注入助手。
//
// 为什么还需要这个：分页契约（view.PageList）要求视图是 struct，而 admin 的 7 个列表页
// 与 mail/order/user 等 13 个页面仍是 `Prepare(c, gin.H{...})` + `c.HTML`。让它们
// 逐个改成 struct 视图要同时动 dto、service、模板三处，而它们绝大多数并不需要
// 编译期键检查（gin.H 天生没有）。
//
// 折中做法：把「算基址 → 造事实 → 写键」这三步收成一个调用，键名不再出现在调用点。
// 每个调用点从 5 行样板变成 1 行 —— 这才是这批页面真正重复的东西。
//
// 与 PageList 契约的关系：契约是「新页面该走的路」，本助手是「存量页面的统一写法」。
// 两者写进同一个键（view.PaginationKey），模板侧完全一致。

import (
	"github.com/gin-gonic/gin"
	"reflect"

	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/view"
)

// AttachPagination 把分页事实写进 data 并返回 data（可直接接在 Prepare 后面）。
//
// 用法：
//
//	data := adminui.Prepare(c, gin.H{...})
//	adminui.AttachPagination(data, "/admin/administrators",
//		map[string]string{"name": name, "email": email},
//		pagination.Page{Total: res.Total, Page: page, Limit: limit})
//	c.HTML(http.StatusOK, "admin/system/administrators", data)
//
// base 由 path + filters 现算（filters 里空值自动丢掉）：翻页时保留筛选条件，
// 这是分页最容易坏的地方（丢条件后记录会「变多」，看着像数据错乱）。
//
// 若调用点已经把 Base 填好（例如 analytics 那种 view 参数要固定的情况），
// 传空的 path 即可 —— 此时不动 Base，只写键。
func AttachPagination(data gin.H, path string, filters map[string]string, p pagination.Page) gin.H {
	if data == nil {
		return data
	}
	if path != "" {
		p.Base = filterBaseURL(path, filters)
	}
	data[view.PaginationKey] = p
	return data
}

// filterBaseURL 与 reqparams.FilterBaseURL 同一实现（包一层只是为了本文件自解释）。
func filterBaseURL(path string, filters map[string]string) string {
	return reqparams.FilterBaseURL(path, filters)
}

// AttachPaginationView 把分页事实挂到**强类型页面视图**上（gin.H 时代的
// AttachPagination 只能挂 map）。
//
// 为什么需要这一版：页面视图从 gin.H 换成强类型结构体之后，`data[view.PaginationKey] = p`
// 这条路就没有了 —— 而分页的事实（total/page/limit/base + 页码窗口）属于
// 「现成数据对象」的一部分，不该逼着调用方为了挂它退回 map。
//
// 做法是用反射给视图加一个**导出字段**承载分页对象，字段名即模板键
// （view.PaginationKey = "Pagination"）。三条约束：
//   - 视图必须是**指针**（否则反射写不进调用方的那个值）；
//   - 字段必须已存在且类型可赋 pagination.Page —— 视图作者显式声明它，
//     而不是让渲染层偷偷长出一个字段（模板契约要看得见）；
//   - 失败页（PageStatus 非零）不该被挂分页，由调用方自己决定（这里不管业务）。
//
// 没有这个字段时**静默不挂**而不是 panic：分页条缺失是「少一条 UI」，
// panic 会让整页 500 —— 后者显然更糟。但静默会让问题藏在产物里，所以返回 bool
// 让调用方（或测试）能断言。
func AttachPaginationView(viewResult any, path string, filters map[string]string, p pagination.Page) bool {
	if viewResult == nil {
		return false
	}
	if path != "" {
		p.Base = filterBaseURL(path, filters)
	}
	rv := reflect.ValueOf(viewResult)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return false
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return false
	}
	field := elem.FieldByName(view.PaginationKey)
	if !field.IsValid() || !field.CanSet() {
		return false
	}
	pageVal := reflect.ValueOf(p)
	if !pageVal.Type().AssignableTo(field.Type()) {
		return false
	}
	field.Set(pageVal)
	return true
}
