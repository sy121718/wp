package pagedto

// StalePageListReq 待重建清单的只读请求。
//
// Limit 与 OrderBy 由调用方给出：本包不写死「取前几条」「按什么排」——
// 那是调用方的口径（列表页取 8 条、日志摘要取 5 条）。
type StalePageListReq struct {
	// ProjectID 工程作用域；空 = 全部工程（service 逐工程各设一次作用域后合并）。
	ProjectID string
	// Limit 条数上限；<=0 取默认，超上限封顶（口径见 pagemodel.NormalizeStaleListLimit）。
	Limit int
	// OrderBy 排序键（pagemodel.StaleOrder*）；空 = 标记时间。
	OrderBy string
	// Descending 是否降序；「最近被标记的在前」用 true。
	Descending bool
}
