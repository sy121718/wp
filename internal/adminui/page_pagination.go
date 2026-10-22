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
