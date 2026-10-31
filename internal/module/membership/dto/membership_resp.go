package membershipdto

import "go_wp/pkg/utils"

type EntitlementResp struct {
	Kind string `json:"kind"`

	ValueInt int64 `json:"valueInt"`
}

type TierResp struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`

	SortOrder int `json:"sortOrder"`

	ThresholdAmount int64 `json:"thresholdAmount"`

	IsDefault bool   `json:"isDefault"`
	Remark    string `json:"remark"`

	Entitlements []EntitlementResp `json:"entitlements"`
	CreateTime   utils.JSONTime    `json:"createTime"`
	UpdateTime   utils.JSONTime    `json:"updateTime"`
}

type MembershipResp struct {
	UserID    uint64 `json:"userId"`
	ProjectID string `json:"projectId"`

	TierID   int64  `json:"tierId"`
	TierName string `json:"tierName"`

	Source string `json:"source"`

	AssignedAt *utils.JSONTime `json:"assignedAt"`

	IsDefaultTier bool `json:"isDefaultTier"`

	FreeShipping bool `json:"freeShipping"`

	DiscountPercent int64 `json:"discountPercent"`
}

type AssignmentResp struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"projectId"`
	UserID    uint64 `json:"userId"`
	TierID    int64  `json:"tierId"`

	TierName string `json:"tierName"`

	Source string `json:"source"`

	AssignedAt utils.JSONTime `json:"assignedAt"`
	UpdateTime utils.JSONTime `json:"updateTime"`
}

type SaveEntitlementsResp struct {
	TierID int64 `json:"tierId"`

	Saved int `json:"saved"`
}

type RecalcResult struct {
	Scanned int `json:"scanned"`

	Changed int `json:"changed"`

	Skipped int `json:"skipped"`
}
