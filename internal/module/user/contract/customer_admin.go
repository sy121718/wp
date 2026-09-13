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
