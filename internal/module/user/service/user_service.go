// Package userservice 用户模块服务（issue #36）。
package userservice

import (
	usercontract "go_wp/internal/module/user/contract"
	usermodel "go_wp/internal/module/user/model"
)

// Service 用户模块服务。
//
// 它持有 mail 的**契约**而不是 mail 的实现：依赖方向是 user → mail（用户模块知道「要发信」，
// 但不知道信怎么发出去）。邮件模块不认识用户模块，这个方向不能反过来。
type Service struct {
	m    *usermodel.UserModel
	mail usercontract.MailSender
	// siteName 用于欢迎邮件等模板变量。
	siteName string
}

// NewService 构造。mail 允许为 nil（邮件模块未装配时注册仍可用，只是发不出验证邮件）。
func NewService(m *usermodel.UserModel, mail usercontract.MailSender, siteName string) *Service {
	if siteName == "" {
		siteName = "本站"
	}
	return &Service{m: m, mail: mail, siteName: siteName}
}

// 编译期断言：本模块服务满足对外契约。
var _ usercontract.UserService = (*Service)(nil)
