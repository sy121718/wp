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

// 页面表单：普通控件提交，服务端负责把「门槛元」与「权益勾选」映射成业务对象。
// 复杂结构（EntitlementReq 清单）不从这里走 —— 表单只有真实控件能表达的字段。

type TierFormReq struct {
	ProjectID       string `form:"projectId"`
	TierID          string `form:"tierId"`
	Name            string `form:"name"`
	ThresholdYuan   string `form:"thresholdYuan"`
	SortOrder       string `form:"sortOrder"`
	IsDefault       string `form:"isDefault"`
	Remark          string `form:"remark"`
	FreeShipping    string `form:"freeShipping"`
	DiscountPercent string `form:"discountPercent"`
}

type EntitlementFormReq struct {
	ProjectID       string `form:"projectId"`
	TierID          string `form:"tierId"`
	FreeShipping    string `form:"freeShipping"`
	DiscountPercent string `form:"discountPercent"`
}

type AssignFormReq struct {
	ProjectID string `form:"projectId"`
	UserID    string `form:"userId"`
	TierID    string `form:"tierId"`
}

type UnlockFormReq struct {
	ProjectID string `form:"projectId"`
	UserID    string `form:"userId"`
}

type DeleteTierFormReq struct {
	ProjectID string `form:"projectId"`
	TierID    string `form:"tierId"`
}
