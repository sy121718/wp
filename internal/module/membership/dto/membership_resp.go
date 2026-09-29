// membership_resp.go — membership 模块的响应结构。
//
// 时间字段一律 utils.JSONTime（AGENTS.md §数据库：对外 JSON 只到秒）。
package membershipdto

import "go_wp/pkg/utils"

// EntitlementResp 一条权益。
type EntitlementResp struct {
	// Kind 权益类型（membershipenums.KindFreeShipping / KindDiscount）。
	Kind string `json:"kind"`
	// ValueInt 按 kind 解释的整数值（免运费 0/1；折扣 1..100）。
	ValueInt int64 `json:"valueInt"`
}

// TierResp 一个等级（含它的权益）。
type TierResp struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
	// SortOrder 等级高低，**越大越高**：解析「消费额落在哪一档」时按它降序取第一个满足门槛的档。
	SortOrder int `json:"sortOrder"`
	// ThresholdAmount 升级门槛，**单位分**（与 orders 的金额列同口径）。
	ThresholdAmount int64 `json:"thresholdAmount"`
	// IsDefault 是否是本工程的默认等级（每工程恰一个）。
	IsDefault bool   `json:"isDefault"`
	Remark    string `json:"remark"`
	// Entitlements 该等级的权益清单（按 kind 升序，读起来稳定）。
	Entitlements []EntitlementResp `json:"entitlements"`
	CreateTime   utils.JSONTime    `json:"createTime"`
	UpdateTime   utils.JSONTime    `json:"updateTime"`
}

// MembershipResp 一次会员身份解析的结果（读路径的对外形状）。
//
// 这份结构是**消费侧唯一该拿到的东西**：order 要 discountPercent 算折扣、
// cart 要 freeShipping 免运费、片段与客户页要名字与等级名。
// 它刻意不含 user_id 之外的任何账号事实（邮箱 / 手机 / 状态），
// 因为消费方没有一条判据需要它们 —— 越权防护靠接口形状，不靠调用方自觉。
type MembershipResp struct {
	UserID    uint64 `json:"userId"`
	ProjectID string `json:"projectId"`
	// TierID / TierName 生效的等级；回退到默认等级时也照常给出（IsDefaultTier 为 true）。
	TierID   int64  `json:"tierId"`
	TierName string `json:"tierName"`
	// Source 这份归属的来源（auto / manual）；回退到默认等级时为空串
	// —— 「没有归属行」与「归属指向 auto」是两件事，不该混成一个值。
	Source string `json:"source"`
	// AssignedAt 归属生效时刻；回退到默认等级时为 nil（本次不是任何一条归属行的结论）。
	AssignedAt *utils.JSONTime `json:"assignedAt"`
	// IsDefaultTier 本次结果来自**默认等级兜底**（该访客还没有归属行）。
	// 消费侧据此区分「他是这个等级的会员」与「他还没成为会员，等级是兜底值」。
	IsDefaultTier bool `json:"isDefaultTier"`
	// FreeShipping 是否免运费（free_shipping 权益）。
	FreeShipping bool `json:"freeShipping"`
	// DiscountPercent 折扣扣减百分比（1..100，20 = 打八折）；0 = 无折扣权益。
	//
	// 与订单侧的约定是**相加扣减**：会员折扣与券各自独立计账，
	// 落到 orders 上分别是 membership_discount_total 与 discount_total
	//（不改既有 discount_total 的语义 —— SEC-001：无券时折扣恒为 0）。
	DiscountPercent int64 `json:"discountPercent"`
}

// AssignmentResp 一条会员归属（后台列表用）。
type AssignmentResp struct {
	ID        int64  `json:"id"`
	ProjectID string `json:"projectId"`
	UserID    uint64 `json:"userId"`
	TierID    int64  `json:"tierId"`
	// TierName 等级名（后台列表要显示名字，而列表不该为每行再查一次等级）。
	TierName string `json:"tierName"`
	// Source auto / manual。
	Source string `json:"source"`
	// AssignedAt 归属生效时刻。
	AssignedAt utils.JSONTime `json:"assignedAt"`
	UpdateTime utils.JSONTime `json:"updateTime"`
}

// SaveEntitlementsResp 全量保存权益的结果。
type SaveEntitlementsResp struct {
	TierID int64 `json:"tierId"`
	// Saved 落库后的权益条数（不是「新增」条数 —— 全量保存下这个数字就是最终状态）。
	Saved int `json:"saved"`
}

// RecalcResult 一次归属重算的结果。
//
// 三个计数分开是有意的：**Changed = 0 可能意味着「口径对但没人变档」，也可能意味着
// 「一个候选都没扫到」**，后者通常说明订单侧的批量聚合端口有问题（或本批没注入）。
// 把 Scanned 一起给出来，排障时不必再猜。
type RecalcResult struct {
	// Scanned 扫到的候选访客数（有可计入消费的人）。
	Scanned int `json:"scanned"`
	// Changed 实际写入或改档的归属行数。
	Changed int `json:"changed"`
	// Skipped 被手工锁定而跳过的行数（source = manual）。
	Skipped int `json:"skipped"`
}
