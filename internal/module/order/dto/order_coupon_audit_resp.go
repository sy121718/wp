package orderdto

type CouponCountMismatch struct {
	CouponID  uint64 `json:"couponId"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`

	UsedCount int64 `json:"usedCount"`

	ActualCount int64 `json:"actualCount"`

	Diff int64 `json:"diff"`
}

type CouponCountAuditResp struct {
	Checked int `json:"checked"`

	Mismatched int `json:"mismatched"`

	Items []CouponCountMismatch `json:"items"`
}
