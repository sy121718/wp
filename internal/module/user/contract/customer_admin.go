package usercontract

// customer_admin.go — 后台「客户管理」的收窄契约。
//
// 为什么不并进 UserService：那个接口是**访客侧**的完整能力（注册 / 登录 / 重置 / 账号中心），
// 拿到它的地方是访问面片段层。把「读全部客户」「停用别人账号」塞进去，
// 片段层手里就多了一把不该有的钥匙 —— 越权防护靠接口形状，不靠调用方自觉。
//
// 这里的方法数量刻意只有四条：后台客户页要做的事就是这些。
// 没有「批量停用」「改邮箱」「重置别人的密码」—— 那些能力等真的有对应页面时再加，
// 「反正接口在手边」正是越权发生的方式。

import (
	"context"

	userdto "go_wp/internal/module/user/dto"
)

// CustomerAdminPort 后台客户管理能力。
//
// 返回的 dto 里**没有任何凭据字段**（password / activation_key / 令牌）：
// 这既是接口约定，也有测试盯着（public/test/user/unit 里有字段名断言）。
type CustomerAdminPort interface {
	// ListCustomers 客户列表：分页 + 关键词 / 状态 / 邮箱验证 / 注册时间范围筛选。
	ListCustomers(ctx context.Context, req *userdto.CustomerListReq) (res *userdto.CustomerListResp, err error)
	// GetCustomer 单个客户的资料与登录事实（不含订单 —— 订单是另一个模块的数据）。
	GetCustomer(ctx context.Context, customerID uint64) (res *userdto.CustomerResp, err error)
	// SetCustomerStatus 启用 / 停用账号（只接受 normal 与 disabled）。
	SetCustomerStatus(ctx context.Context, req *userdto.CustomerStatusReq) (res *userdto.CustomerStatusResp, err error)
	// UnlockCustomer 解除登录锁定（清 locked_until_time 与失败计数）。
	UnlockCustomer(ctx context.Context, req *userdto.CustomerUnlockReq) (res *userdto.CustomerUnlockResp, err error)
}

// CustomerQueryReader 客户只读视图：给 AI 工具的窄门。
//
// 为什么不直接复用 CustomerAdminPort：那一把钥匙还能**停用账号**与**解除锁定**。
// 工具由模型驱动 —— 让它手里握着全部能力，等于让「顺手把这人停用了」在某次
// 无关改动里悄悄变得可能。越权防护靠接口形状，不靠调用方自觉。
//
// 只读这一点也决定了它**不需要**额外的方法：客户事实全在 CustomerResp 里，
// 订单维度的事实归订单模块（order_find 已经能按客户名/邮箱反查）。
type CustomerQueryReader interface {
	// ListCustomers 客户列表：分页 + 关键词 / 状态 / 邮箱验证 / 注册时间范围筛选。
	ListCustomers(ctx context.Context, req *userdto.CustomerListReq) (res *userdto.CustomerListResp, err error)
	// GetCustomer 单个客户的资料与登录事实。
	GetCustomer(ctx context.Context, customerID uint64) (res *userdto.CustomerResp, err error)
}

// CustomerWriter 客户的账号状态变更：给 AI 工具的窄门（写侧）。
//
// 这一段曾以「让模型能停用账号太危险」为由整块排除。这个结论现在改了，理由要写清，
// 因为它与「危险的能力一律不给」是两条不同的原则：
//
//	· 排除了它，不等于这件事不会发生 —— 用户仍然会去后台点那个按钮，
//	  只是他得自己找到页面、自己确认、自己记。助手能代劳的是**核对**这一半
//	  （这个邮箱是谁、他上次登录是什么时候、他还有没有未发货的订单）。
//	· 真正要守的是「模型不能自作主张」。写成工具的形态已经自带了这道闸：
//	  NewWrite 强制 confirm 参数、idempotencyKey 防重复、每次调用落 ai_tool_call_log。
//	  工具的说明里再写死「执行前必须把客户是谁念给用户确认」。
//	· 与 Review 不同的是，停用是**可逆**的（再启用即可），且不删数据。
//
// 因此这里刻意只有两个方法，且都只动账号状态：
// SetCustomerStatus（正常 / 停用）、UnlockCustomer（解除登录锁定）。
// 没有改邮箱、改密码、改角色 —— 那些确实不该由模型代劳，等有页面时另说。
type CustomerWriter interface {
	// SetCustomerStatus 启用 / 停用账号（只接受 normal 与 disabled）。
	SetCustomerStatus(ctx context.Context, req *userdto.CustomerStatusReq) (res *userdto.CustomerStatusResp, err error)
	// UnlockCustomer 解除登录锁定（清 locked_until_time 与失败计数）。
	UnlockCustomer(ctx context.Context, req *userdto.CustomerUnlockReq) (res *userdto.CustomerUnlockResp, err error)
}
