package orderdto

// CouponCountAuditReq 对账请求。
type CouponCountAuditReq struct {
	// ProjectID 可选：留空对账全部工程。
	ProjectID string `form:"projectId"`
	// Limit 返回明细条数上限（<=0 用默认 100）。
	Limit int `form:"limit"`
}
