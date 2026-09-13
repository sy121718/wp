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
	m  *usermodel.UserModel
	sm *usermodel.UserSessionModel
	// pm / prefm 是账号中心的两张一对一表（扩展资料 / 前台偏好）。
	pm    *usermodel.UserProfileModel
	prefm *usermodel.UserPreferenceModel
	mail  usercontract.MailSender
	// siteName 用于欢迎邮件等模板变量。
	siteName string
}

// NewService 构造。四个 model 与 mail 都允许为 nil：
//   - mail 为 nil → 注册仍可用，只是发不出验证邮件；
//   - Session / Profile / Preference 为 nil → 注册与密码重置不碰它们，
//     纯注册链路的测试不必为了构造而准备整个依赖图。
func NewService(
	m *usermodel.UserModel,
	sm *usermodel.UserSessionModel,
	pm *usermodel.UserProfileModel,
	prefm *usermodel.UserPreferenceModel,
	mail usercontract.MailSender,
	siteName string,
) *Service {
	if siteName == "" {
		siteName = "本站"
	}
	return &Service{m: m, sm: sm, pm: pm, prefm: prefm, mail: mail, siteName: siteName}
}

// 编译期断言：本模块服务满足对外契约。
var _ usercontract.UserService = (*Service)(nil)

// 后台客户管理是**另一条契约**（不并进 UserService，理由见 contract/customer_admin.go）：
// 断言放在这里，装配处拿到的 *Service 两个面都有。
var _ usercontract.CustomerAdminPort = (*Service)(nil)
