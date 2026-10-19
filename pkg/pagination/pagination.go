package pagination

// pagination.go — 分页的**事实**：在第几页、每页多少条、共多少条、链接怎么接。
//
// 这里只回答「值是多少」，不回答「显示成什么样」：页数、本页覆盖区间（第 X-Y 条）、
// 信息行句子、上/下一页文字、href 串接、选中与禁用态都由模板层算
// （internal/templates/pager.go + admin/partials/pagination.html）。
//
// 为什么不留 Facts 这样的构造函数：它算出来的全是显示派生量。留在 pkg 会让「后端拼显示」
// 再长回来 —— 那是本仓反复踩过的坑（dto 带 Pages/Start/End，模板照着抄）。
//
// 本包只做 SQL 之外的两件事：页码收敛（取数之前必须先收敛，否则出现「表格为空、
// 分页条显示第 999 页」）与内存切片分页。

// Page 是分页事实。
type Page struct {
	Total int64
	Page  int
	Limit int

	PageParam string // 页码参数名（默认 page）
	DropLimit bool   // 链接里去掉 limit（零值＝带上，与现行链接一致）
	Base      string // 链接基址：页面路径 + 已知筛选条件（不含 page / limit）
}
