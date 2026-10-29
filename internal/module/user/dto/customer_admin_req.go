package userdto

// customer_admin_req.go — 后台「客户管理」的请求（收窄：读列表 / 读详情 / 停用启用 / 解除锁定）。
//
// 与访客侧的 user_req.go 分开：那一侧是「操作自己的账号」，这一侧是「管理别人的账号」，
// 两者的越权面完全不同 —— 合成一个文件，读代码的人分不清哪条路径需要后台身份。

import (
	"bytes"
	"strconv"

	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/bulkids"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

// 邮箱验证筛选的三态取值。
//
// EmailVerifiedAll 用 0（"不过滤"是零值），因此**不能**用 1/0 表达「已验证/未验证」——
// 那样零值会被解释成「只看未验证」，而调用方绝大多数时候想表达的是「都看」。
const (
	EmailVerifiedAll = 0
	EmailVerifiedYes = 1
	EmailVerifiedNo  = 2
)

// CustomerListReq 客户列表请求（分页 + 组合筛选，条件全部可选）。
type CustomerListReq struct {
	// Keyword 模糊匹配 邮箱 / 用户名 / 昵称 / 展示名（匹配哪几列由 user 模块决定）。
	Keyword string
	// Status 账号状态；CustomerStatusAll 表示不过滤。
	Status int
	// EmailVerified EmailVerifiedAll / Yes / No。
	EmailVerified int
	// RegisteredFrom / RegisteredTo 注册时间范围（闭区间，nil = 该端不限）。
	RegisteredFrom *utils.JSONTime
	RegisteredTo   *utils.JSONTime
	// LockedOnly 只取**当前**处于锁定状态的账号（locked_until_time 在未来）。
	//
	// 与 Status 是两条轴（停用与锁定分开，见 service 文件头）：锁定由连续登录失败触发、
	// 到点自己过期，停用是管理动作。做成独立开关而不是塞进 Status 的某个取值，
	// 否则「已锁定的正常账号」这个真实存在的组合无法表达。
	LockedOnly bool
	// UserIDs 把结果限定为这批客户（**nil = 不限制；空切片 = 限定为零个人**）。
	//
	// 来源是订单模块的客户分段（见 ordercontract.CustomerSegmentReader）：
	// 「新客 / 回头客 / 复购」那几条口径只有看得到 orders 表的一侧答得出来，
	// 客户模块拿到 id 之后再筛自己的行。
	//
	// **nil 与空切片必须区别对待**：分段筛出 0 个人时若当成「不限制」，
	// 页面会把「这个分段没人」显示成「全部客户」—— 看起来完全正常。
	UserIDs []int64
	Offset  int
	Limit   int
}

// CustomerStatusAll 列表筛选里「状态不过滤」的取值。
//
// 用 -1 而不是 0：0 是「已停用」这个**合法**的筛选值，
// 拿 0 表示「全部」就等于永远筛不出停用账号。
const CustomerStatusAll = -1

// CustomerStatusReq 启用 / 停用客户账号。
//
// 只接受「正常」与「已停用」两个值：待激活（pending）是注册流程的中间态，
// 一旦允许后台自由设置，就会出现「被手工改成未验证」的账号 ——
// 它既不会收到验证邮件、也没有人能解释它是怎么来的。
type CustomerStatusReq struct {
	CustomerID uint64
	Status     int
}

// CustomerStatusSetReq 启用 / 停用接口的请求体。
//
// Status 故意是**指针**：0 是「已停用」这个有效取值，用 int 兜「没传」会把
// 「漏传状态」读成「停用账号」，一次手滑就把人踢下线。
type CustomerStatusSetReq struct {
	CustomerID uint64 `json:"customerId" form:"customerId"`
	Status     *int   `json:"status" form:"status"`
}

// CustomerUnlockReq 解除登录锁定（清 locked_until_time 与失败计数）。
type CustomerUnlockReq struct {
	CustomerID uint64 `json:"customerId" form:"customerId"`
}

// CustomerBulkUnlockReq 批量解锁的完整请求形状；未知字段和顶层 null 一律拒绝。
type CustomerBulkUnlockReq struct {
	IDs []uint64 `json:"ids,case:strict" form:"ids" binding:"required,min=1"`
}

func (r *CustomerBulkUnlockReq) UnmarshalJSON(data []byte) error {
	type plain CustomerBulkUnlockReq
	var decoded plain
	if err := json.Unmarshal(data, &decoded, json.RejectUnknownMembers(true)); err != nil || decoded.IDs == nil {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	*r = CustomerBulkUnlockReq(decoded)
	return nil
}

func (r CustomerBulkUnlockReq) CustomerBulkValues() ([]uint64, int, error) {
	ids, err := normalizeCustomerBulkIDs(r.IDs)
	return ids, 0, err
}

// CustomerBulkStatusReq 状态必须显式提供，0 是合法的停用目标。
type CustomerBulkStatusReq struct {
	IDs    []uint64 `json:"ids,case:strict" form:"ids" binding:"required,min=1"`
	Status *int     `json:"status,case:strict" form:"status" binding:"required"`
}

func (r *CustomerBulkStatusReq) UnmarshalJSON(data []byte) error {
	// 原协议把显式 null 归为形状错误，把缺失/非法状态归为状态错误。
	// 暂存两个原始字段只用于保留这一区别，不接受自由字段映射。
	var fields struct {
		IDs    json.RawMessage `json:"ids,case:strict"`
		Status json.RawMessage `json:"status,case:strict"`
	}
	if err := json.Unmarshal(data, &fields, json.RejectUnknownMembers(true)); err != nil ||
		bytes.Equal(bytes.TrimSpace(fields.IDs), []byte("null")) || bytes.Equal(bytes.TrimSpace(fields.Status), []byte("null")) {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	var decoded CustomerBulkStatusReq
	if err := json.Unmarshal(fields.IDs, &decoded.IDs); err != nil {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	if err := json.Unmarshal(fields.Status, &decoded.Status); err != nil ||
		decoded.Status == nil || (*decoded.Status != userenums.StatusActive && *decoded.Status != userenums.StatusDisabled) {
		return userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	*r = decoded
	return nil
}

func (r CustomerBulkStatusReq) CustomerBulkValues() ([]uint64, int, error) {
	if r.Status == nil {
		return nil, 0, userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	ids, err := normalizeCustomerBulkIDs(r.IDs)
	return ids, *r.Status, err
}

// normalizeCustomerBulkIDs 保留请求顺序去重，再按唯一 ID 数检查批量上限。
func normalizeCustomerBulkIDs(input []uint64) ([]uint64, error) {
	idsText := make([]string, len(input))
	for i, id := range input {
		idsText[i] = strconv.FormatUint(id, 10)
	}
	normalized, err := bulkids.Normalize(idsText)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	ids := make([]uint64, len(normalized))
	for i, id := range normalized {
		ids[i], _ = strconv.ParseUint(id, 10, 64)
	}
	return ids, nil
}
