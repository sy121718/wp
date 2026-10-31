package membershipdto

type ListTiersReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
}

type GetTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

type EntitlementReq struct {
	Kind string `json:"kind" form:"kind"`

	ValueInt int64 `json:"valueInt" form:"valueInt"`
}

type CreateTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Name      string `json:"name" form:"name"`

	SortOrder int `json:"sortOrder" form:"sortOrder"`

	ThresholdAmount int64 `json:"thresholdAmount" form:"thresholdAmount"`

	IsDefault bool   `json:"isDefault" form:"isDefault"`
	Remark    string `json:"remark" form:"remark"`

	Entitlements []EntitlementReq `json:"entitlements" form:"entitlements"`
}

type UpdateTierReq struct {
	ProjectID string  `json:"projectId" form:"projectId"`
	TierID    int64   `json:"tierId" form:"tierId"`
	Name      *string `json:"name" form:"name"`
	SortOrder *int    `json:"sortOrder" form:"sortOrder"`

	ThresholdAmount *int64  `json:"thresholdAmount" form:"thresholdAmount"`
	IsDefault       *bool   `json:"isDefault" form:"isDefault"`
	Remark          *string `json:"remark" form:"remark"`
}

type DeleteTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

type SaveEntitlementsReq struct {
	ProjectID string           `json:"projectId" form:"projectId"`
	TierID    int64            `json:"tierId" form:"tierId"`
	Items     []EntitlementReq `json:"items" form:"items"`
}

type ResolveReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	UserID uint64 `json:"userId" form:"userId"`
}

type ListAssignmentsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`

	TierID int64 `json:"tierId" form:"tierId"`

	Source string `json:"source" form:"source"`

	UserID uint64 `json:"userId" form:"userId"`
	Page   int    `json:"page" form:"page"`
	Size   int    `json:"size" form:"size"`
}

type CountAssignmentsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
	Source    string `json:"source" form:"source"`
	UserID    uint64 `json:"userId" form:"userId"`
}

type AssignManualReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	UserID    uint64 `json:"userId" form:"userId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

type UnlockManualReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	UserID    uint64 `json:"userId" form:"userId"`
}

type RecalcProjectReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
}
