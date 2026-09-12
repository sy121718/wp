// Package usercontract 用户模块对外契约（issue #36）。
package usercontract

import (
	"context"

	maildto "go_wp/internal/module/mail/dto"
	userdto "go_wp/internal/module/user/dto"
)

// UserService 用户模块对外能力。
//
// 目前只暴露注册链路 —— 它是**邮件模块的第一个真实消费者**（注册验证邮件），
// 也是「模板写好了但没人用」与「真的被业务用上」之间的差别。
// admin 侧 CRUD 与用户侧登录 / 账号中心属 #36 的后续部分，未在此接口内。
type UserService interface {
	// Register 注册：写用户（待激活）并发送验证邮件。
	//
	// 邮件发送失败**不回滚注册** —— 用户已经建好了，验证邮件可以重发；
	// 回滚会让「SMTP 临时故障」变成「用户注册不了」。返回值的 MailQueued 如实反映。
	Register(ctx context.Context, req *userdto.RegisterReq) (*userdto.RegisterResp, error)
	// ActivateEmail 用邮件里的凭据完成邮箱验证。
	ActivateEmail(ctx context.Context, req *userdto.ActivateEmailReq) (*userdto.ActivateResp, error)
	// ResendActivation 重发验证邮件（未激活的用户）。
	ResendActivation(ctx context.Context, req *userdto.ResendActivationReq) error
}

// MailSender 用户模块需要的邮件能力 —— **只有发送这一条**。
//
// 为什么不直接依赖 mailcontract.MailService：那个接口有二十来个方法（账号 / 模板 / 群发 / 报表），
// 用户模块一条都用不上。接口隔离不只是美观问题：
//
//	· 依赖面越大，越容易在不经意间用上不该用的能力；
//	· 测试要造替身时，二十个方法的空实现会把测试意图淹掉。
//
// mail 的 Service 天然满足这个接口（它有 SendTemplate），装配时直接传即可。
type MailSender interface {
	SendTemplate(ctx context.Context, req *maildto.SendTemplateReq) (*maildto.SendResult, error)
}
