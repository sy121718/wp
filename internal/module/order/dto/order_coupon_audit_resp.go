package orderdto

// CouponCountMismatch 一张券的「计数与明细不一致」。
type CouponCountMismatch struct {
	CouponID  uint64 `json:"couponId"`
	ProjectID string `json:"projectId"`
	Code      string `json:"code"`
	// UsedCount coupons.used_count（投影值）。
	UsedCount int64 `json:"usedCount"`
	// ActualCount coupon_redemptions 行数（真源）。
	ActualCount int64 `json:"actualCount"`
	// Diff = UsedCount - ActualCount：正数说明计数多记（可能少放了额度），
	// 负数说明计数少记（可能超出 max_uses 继续核销）。两个方向都要人看一眼。
	Diff int64 `json:"diff"`
}

// CouponCountAuditResp 对账结果。
type CouponCountAuditResp struct {
	// Checked 参与对账的券数量（分母）。
	Checked int `json:"checked"`
	// Mismatched 不一致的券数量。
	Mismatched int `json:"mismatched"`
	// Items 不一致明细（至多 limit 条）。
	Items []CouponCountMismatch `json:"items"`
}
