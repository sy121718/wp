package pagedto

// BulkDeleteReq 是页面批量删除的入参：一次调用按同一套规则整批删除，
// 任一目标缺失、被引用或归属变化都整批拒绝，不返回部分成功。
//
// IDs 保留调用方提交的原始字符串（含空串与非法格式），由 service 统一判定，
// 控制器不预先过滤或去重 —— 否则非法项会静默消失成一次部分提交。
type BulkDeleteReq struct {
	IDs []string `json:"ids" form:"ids"`
}

// PageBulkDeleteFormReq 是后台批量删除表单的原生绑定形态。
type PageBulkDeleteFormReq struct {
	IDs []string `form:"ids"`
}
