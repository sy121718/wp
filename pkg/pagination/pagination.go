package pagination

// pagination.go — 分页的**事实**：在第几页、每页多少条、共多少条、链接怎么接。
//
// 本包回答两类问题：
//   - 值是事实还是显示形状？事实（Total / Page / Limit）由 Page 承载；页码窗口（Window）
//     与取址（QueryURL）是「有主张的显示策略」，同行主流框架一样放在库里而不是视图里
//     —— 它们是策略，不是形式细节。
//   - 剩下由模板层做：本页覆盖区间（第 X-Y 条）、信息行句子、上/下一页文字、选中与禁用态
//     （internal/templates/pager.go + admin/partials/pagination.html）。
//
// 为什么不留 Facts 这样的构造函数：它算出来的全是「后端拼显示」的派生物，留在 pkg 会让
// dto 重新长出 Pages/Start/End 而模板照着抄 —— 那是本仓反复踩过的坑。
//
// 本包只做 SQL 之外的三件事：页码收敛（取数之前必须先收敛，否则出现「表格为空、
// 分页条显示第 999 页」）、内存切片分页、以及窗口与取址这两段有主张的显示策略。

// Page 是分页事实。
type Page struct {
	Total int64
	Page  int
	Limit int

	PageParam string // 页码参数名（默认 page）
	DropLimit bool   // 链接里去掉 limit（零值＝带上，与现行链接一致）
	Base      string // 链接基址：页面路径 + 已知筛选条件（不含 page / limit）
}
