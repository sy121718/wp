package orderdto

type CouponCountAuditReq struct {
	ProjectID string `form:"projectId"`

	Limit int `form:"limit"`
}
