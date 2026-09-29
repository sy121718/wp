// membership_req.go — membership 模块的请求结构（数据流 inbound → service → inbound）。
package membershipdto

// ListTiersReq 列出某工程的等级（含各自权益）。
type ListTiersReq struct {
	// ProjectID 工程 id（uuid）。必填 —— 等级按工程建键，没有工程就没有可查的范围。
	ProjectID string `json:"projectId" form:"projectId"`
}

// GetTierReq 取单个等级详情。
type GetTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

// EntitlementReq 一条权益（建等级 / 存权益共用）。
//
// 用整数字段而不是 map[string]any 承载取值：这张表参与金额计算，
// `{"percent":"20"}` 这类形态在 Go 侧会算成 0 或 panic，强类型进不来。
type EntitlementReq struct {
	// Kind 权益类型（membershipenums.KindFreeShipping / KindDiscount）。
	Kind string `json:"kind" form:"kind"`
	// ValueInt 按 kind 解释：免运费取 0/1；折扣取 1..100（扣减百分比）。
	ValueInt int64 `json:"valueInt" form:"valueInt"`
}

// CreateTierReq 新建等级（可一次带上权益）。
type CreateTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	Name      string `json:"name" form:"name"`
	// SortOrder 等级高低，越大越高。语义见 dto 的 TierResp.SortOrder。
	SortOrder int `json:"sortOrder" form:"sortOrder"`
	// ThresholdAmount 升级门槛，**单位分**（与 orders 的金额列同口径）。
	ThresholdAmount int64 `json:"thresholdAmount" form:"thresholdAmount"`
	// IsDefault 是否设为默认等级。工程内只能有一个（部分唯一索引承载）。
	IsDefault bool   `json:"isDefault" form:"isDefault"`
	Remark    string `json:"remark" form:"remark"`
	// Entitlements 可选的权益清单；同 kind 重复时后一条覆盖前一条（按 kind 去重后落库）。
	Entitlements []EntitlementReq `json:"entitlements" form:"entitlements"`
}

// UpdateTierReq 更新等级（指针字段 = 只在非 nil 时改，nil 保持不变）。
type UpdateTierReq struct {
	ProjectID string  `json:"projectId" form:"projectId"`
	TierID    int64   `json:"tierId" form:"tierId"`
	Name      *string `json:"name" form:"name"`
	SortOrder *int    `json:"sortOrder" form:"sortOrder"`
	// ThresholdAmount 单位分。
	ThresholdAmount *int64  `json:"thresholdAmount" form:"thresholdAmount"`
	IsDefault       *bool   `json:"isDefault" form:"isDefault"`
	Remark          *string `json:"remark" form:"remark"`
}

// DeleteTierReq 删除等级（软删；仍挂着归属时被拒）。
type DeleteTierReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

// SaveEntitlementsReq 全量保存某等级的权益。
//
// 「全量」的语义：清单就是最终状态 —— 没列出的 kind 会被删掉，
// 而不是「只增不减」。这样「取消折扣」不需要一个单独的删除接口。
type SaveEntitlementsReq struct {
	ProjectID string           `json:"projectId" form:"projectId"`
	TierID    int64            `json:"tierId" form:"tierId"`
	Items     []EntitlementReq `json:"items" form:"items"`
}

// ResolveReq 解析某访客在某工程的会员身份（读路径）。
type ResolveReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	// UserID 访客账号 id（users.id）。必填 —— 0 是无效 id，
	// 静默当成「没有归属」会让调用方以为解析成功。
	UserID uint64 `json:"userId" form:"userId"`
}

// ListAssignmentsReq 列出某工程的会员归属（分页 + 可选筛选）。
type ListAssignmentsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	// TierID 只看某个等级的归属；0 = 不筛。
	TierID int64 `json:"tierId" form:"tierId"`
	// Source 只看某个来源（auto / manual）；空 = 不筛。
	Source string `json:"source" form:"source"`
	// UserID 只看某个访客；0 = 不筛。
	UserID uint64 `json:"userId" form:"userId"`
	Page   int    `json:"page" form:"page"`
	Size   int    `json:"size" form:"size"`
}

// CountAssignmentsReq 与 ListAssignmentsReq 同一组筛选条件（分页条要先算总数）。
type CountAssignmentsReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	TierID    int64  `json:"tierId" form:"tierId"`
	Source    string `json:"source" form:"source"`
	UserID    uint64 `json:"userId" form:"userId"`
}

// AssignManualReq 手工指定某访客在某工程的等级。
//
// 语义：**手工指定总是覆盖当前归属**（包括覆盖上一条手工指定的），
// 并把 source 置 manual —— 此后日结自动重算不会动它（见 service.AssignManual）。
type AssignManualReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	UserID    uint64 `json:"userId" form:"userId"`
	TierID    int64  `json:"tierId" form:"tierId"`
}

// UnlockManualReq 取消手工锁定：把该归属交还自动重算。
//
// 只改 source、不动 tier_id —— 等级要等下一次日结按消费额重算才变，
// 这样「解锁」本身不会造成一次可见的等级跳变（运营能先看一眼再让它跑）。
type UnlockManualReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	UserID    uint64 `json:"userId" form:"userId"`
}

// RecalcProjectReq 手工触发一次某工程的归属重算（后台 / 排障用）。
type RecalcProjectReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
}
