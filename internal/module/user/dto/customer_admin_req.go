package userdto

// customer_admin_req.go — 后台「客户管理」的请求（收窄：读列表 / 读详情 / 停用启用 / 解除锁定）。
//
// 与访客侧的 user_req.go 分开：那一侧是「操作自己的账号」，这一侧是「管理别人的账号」，
// 两者的越权面完全不同 —— 合成一个文件，读代码的人分不清哪条路径需要后台身份。

import (
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

// CustomerUnlockReq 解除登录锁定（清 locked_until_time 与失败计数）。
type CustomerUnlockReq struct {
	CustomerID uint64
}
