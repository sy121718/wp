// Package usercontract 用户模块对外契约（issue #36）。
package usercontract

import (
	"context"

	mailcontract "go_wp/internal/module/mail/contract"
	userdto "go_wp/internal/module/user/dto"
)

// VisitorContextKey 访客身份在 gin context 里的键。
//
// 放在契约包里：写入方是 user 模块的中间件，读取方在 runtimefragment ——
// 两边各自写字面量，改一处漏一处的表现是「访客永远没登录」，
// 而且这种失败在本地开发（cookie 刚设过）里常常复现不出来。
const VisitorContextKey = "gowp_visitor_user_id"

// VisitorCSRFContextKey 访客 CSRF token 在 gin context 里的键。
//
// 访问面的片段渲染表单（登录 / 注册 / 找回密码）必须带上访客域的 CSRF token，
// 而 token 存在**签名会话 cookie** 里、静态产物烘不进去 —— 只能由片段现读现写。
// 与 VisitorContextKey 同理：两边各写字面量，改一处漏一处的表现是「表单永远 403」。
const VisitorCSRFContextKey = "gowp_visitor_csrf_token"

// VisitorIdentityResolver 把访客会话令牌解成 user id。
//
// 只给这一条能力：访问面的片段层需要的是「这个请求是谁」，
// 不是账号资料、更不是账号管理。依赖面越小，越不容易被顺手用出越权。
type VisitorIdentityResolver interface {
	// ResolveVisitorID 令牌有效且未过期时返回 userID 与 true。
	// 令牌为空 / 无效 / 已过期一律返回 (0, false)：调用方只关心「认出来了没有」。
	ResolveVisitorID(ctx context.Context, token string) (userID uint64, ok bool)
}

// UserService 用户模块对外能力。
//
// 目前只暴露注册链路 —— 它是**邮件模块的第一个真实消费者**（注册验证邮件），
// 也是「模板写好了但没人用」与「真的被业务用上」之间的差别。
// admin 侧 CRUD 与用户侧登录 / 账号中心属 #36 的后续部分，未在此接口内。
type UserService interface {
	// VisitorIdentityResolver 来访客会话令牌 → user id（片段层解析访客身份用）。
	VisitorIdentityResolver

	// GuestAccountProvisioner 来访客下单自动开号（单方法接口）。
	// 嵌进来的理由同 product 的 VariantSnapshotPort：装配处拿到的 UserService
	// 天然也能当开号端口传给订单模块，不必再做类型断言。
	GuestAccountProvisioner

	// VisitorAccountPort 来账号中心片段读**本人**的资料 / 偏好 / 登录设备。
	//
	// 嵌进来的理由同上：装配处拿到的 UserService 直接就能当这个端口传给 runtimefragment。
	// 但注意它**本身是收窄的**（只有两条只读方法）—— 片段层拿到的那份接口里
	// 没有注册、没有改密码、没有踢出设备，userID 也只能由会话推出。
	VisitorAccountPort

	// Register 注册：写用户（待激活）并发送验证邮件。
	//
	// 邮件发送失败**不回滚注册** —— 用户已经建好了，验证邮件可以重发；
	// 回滚会让「SMTP 临时故障」变成「用户注册不了」。返回值的 MailQueued 如实反映。
	Register(ctx context.Context, req *userdto.RegisterReq) (*userdto.RegisterResp, error)
	// ActivateEmail 用邮件里的凭据完成邮箱验证。
	ActivateEmail(ctx context.Context, req *userdto.ActivateEmailReq) (*userdto.ActivateResp, error)
	// ResendActivation 重发验证邮件（未激活的用户）。
	ResendActivation(ctx context.Context, req *userdto.ResendActivationReq) error
	// RequestPasswordReset 申请重置密码。
	//
	// **无论邮箱是否存在都返回 nil**：对存在与否给出不同回应会把这个接口变成账号枚举器。
	RequestPasswordReset(ctx context.Context, req *userdto.PasswordResetReqRequest) error
	// ResetPassword 用重置码改密。
	ResetPassword(ctx context.Context, req *userdto.ResetPasswordReq) error
}

// GuestAccountProvisioner 供订单域为访客下单自动开号。
//
// 单独一个接口而不是并进 UserService：订单只需要这一条能力，而 UserService 是访客侧的
// 完整契约（注册 / 激活 / 登录 / 重置 / 账号中心）。理由同下面的 MailSender ——
// 依赖面越小，越不容易在不经意间用上不该用的能力。
type GuestAccountProvisioner interface {
	// EnsureGuestAccount 确保 email 对应账号存在。
	//
	// 不存在才建号（随机初始密码，经邮件发给客户）；**已存在则只返回既有账号，
	// 绝不触碰它的密码** —— 否则任何人拿别人邮箱下一单就能把对方密码换掉。
	EnsureGuestAccount(ctx context.Context, in *GuestAccountInput) (res *GuestAccountResult, err error)
}

// GuestAccountInput 为访客下单自动开号的入参 —— 契约自有形状，不是 dto 的别名。
//
// 只有四个字段，因为「替访客开号」这件事需要的信息就这四件：账号身份（邮箱）、
// 展示名、模板语言、以及注册来源 IP（留痕用，由调用方从请求上下文取）。
// 借 dto 的形状会让调用方（订单模块）认识用户模块的绑定层字段（审计 CQ-004）。
type GuestAccountInput struct {
	// Email 客户下单时填的邮箱（也是账号身份）。
	Email string
	// Name 客户名（用作昵称；为空时展示名退回用户名）。
	Name string
	// Locale 决定用哪套语言模板。
	Locale string
	// RegisterIP 由调用方从请求上下文取，客户端不可伪造。
	RegisterIP string
}

// GuestAccountResult 开号结果。
//
// Created 与 PasswordMailed 是调用方**唯一**要判定的两件事：
// 前者说明这次是不是真新建了账号，后者说明初始密码邮件有没有寄出去 ——
// 两个都为真才该提示客户「去邮箱收密码」（已有账号时绝不发密码，
// 那种情况下提示客户查收会让他在邮箱里白找一场）。
type GuestAccountResult struct {
	UserID   uint64
	Username string
	// Created 本次是否**新建**了账号；false 表示该邮箱已有账号，只做了关联。
	Created bool
	// PasswordMailed 初始密码邮件是否已受理（仅新建且发信成功时为真）。
	PasswordMailed bool
}

// MailSender 用户模块需要的邮件能力 —— **只有发送事务邮件这一条**。
//
// 为什么不直接依赖 mailcontract.MailService：那个接口有二十来个方法（账号 / 模板 / 群发 / 报表），
// 用户模块一条都用不上。接口隔离不只是美观问题：
//
//	· 依赖面越大，越容易在不经意间用上不该用的能力；
//	· 测试要造替身时，二十个方法的空实现会把测试意图淹掉。
//
// 入参 / 返回类型取自 mailcontract 的**事务发送端口**（SendInput / SendOutcome），
// 不是 mail 的 dto：dto 服务 HTTP 绑定，形状随绑定需求变；契约形状只随语义变。
// 这里刻意保留本模块的具名接口（而不是直接写 mailcontract.TransactionalSender 别名）——
// 用户模块要表达的是「我要发出一封事务邮件」这条**需求**，
// 至于它由 mail 模块还是别的东西满足，是装配期的事。
//
// mail 的 Service 天然满足这个接口（它有 SendTransactional），装配时直接传即可。
type MailSender interface {
	SendTransactional(ctx context.Context, in *mailcontract.SendInput) (*mailcontract.SendOutcome, error)
}
