// Package userenums 用户模块的响应与错误消息（issue #36）。
package userenums

const (
	MsgRegisterSuccess = "user.msg.registerSuccess"
	MsgActivateSuccess = "user.msg.activateSuccess"
	MsgLoginSuccess    = "user.msg.loginSuccess"
	MsgLogoutSuccess   = "user.msg.logoutSuccess"
	MsgProfileSaved    = "user.msg.profileSaved"
	MsgPreferenceSaved = "user.msg.preferenceSaved"
	MsgPasswordChanged = "user.msg.passwordChanged"
	MsgResetMailSent   = "user.msg.resetMailSent"
)

const (
	ErrInvalidParam      = "user.err.invalidParam"
	ErrUsernameRequired  = "user.err.usernameRequired"
	ErrUsernameTooShort  = "user.err.usernameTooShort"
	ErrUsernameTooLong   = "user.err.usernameTooLong"
	ErrUsernameTaken     = "user.err.usernameTaken"
	ErrEmailRequired     = "user.err.emailRequired"
	ErrEmailInvalid      = "user.err.emailInvalid"
	ErrEmailTaken        = "user.err.emailTaken"
	ErrPasswordTooShort  = "user.err.passwordTooShort"
	ErrUserNotFound      = "user.err.userNotFound"
	ErrActivationInvalid = "user.err.activationInvalid"
	ErrMailUnavailable   = "user.err.mailUnavailable"
)

// 登录与会话。
const (
	// ErrLoginRequired 账号或密码为空（与「凭据不对」分开：这是表单没填全，不是密码错）。
	ErrLoginRequired = "user.err.loginRequired"
	// ErrBadCredentials 凭据错误。**用户不存在与密码错误必须共用这一条** ——
	// 区分它们等于把登录接口做成账号枚举器。
	ErrBadCredentials = "user.err.badCredentials"
	// ErrAccountLocked 连续失败被锁定（文案里的 %s 由调用处填入剩余时间）。
	ErrAccountLocked = "user.err.accountLocked"
	// ErrAccountDisabled 被管理侧禁用（与「锁定」不同：禁用不会自己解除）。
	ErrAccountDisabled = "user.err.accountDisabled"
	// ErrAccountPending 注册后未完成邮箱验证。
	ErrAccountPending = "user.err.accountPending"
	// ErrPasswordLoginUnavailable 第三方注册的账号没有本站密码。
	//
	// 与「密码错误」分开说：真正的密码错误用户可以重试，而这个账号根本没有密码可试，
	// 混在一起会让人陷在「改了密码还是登不上」里。
	ErrPasswordLoginUnavailable = "user.err.passwordLoginUnavailable"
	ErrSessionNotFound          = "user.err.sessionNotFound"
	ErrSessionExpired           = "user.err.sessionExpired"
	ErrLogoutFailed             = "user.err.logoutFailed"
	ErrOldPasswordWrong         = "user.err.oldPasswordWrong"
	ErrNewPasswordSame          = "user.err.newPasswordSame"
	ErrProfileVisibilityInvalid = "user.err.profileVisibilityInvalid"
	ErrPageSizeInvalid          = "user.err.pageSizeInvalid"
	ErrNotLoggedIn              = "user.err.notLoggedIn"
	// ErrInternal 未归类的系统错误对外统一文案。
	//
	// 存在的理由：基础设施错误（数据库 / Redis）的原文可能带表名、列名甚至 SQL 片段，
	// 那是给运维看的，不是给访客看的。展示层据此把「不是本模块业务文案」的错误全部落到这一条。
	ErrInternal = "user.err.internal"
)

// 后台客户管理（/admin/customers 与 /api/customer/*）的文案。
//
// 与上面的访客文案分开一块：它们服务的是**运营**而不是访客 —— 同一个「账号已停用」
// 对客户是拒绝登录的理由，对运营是刚做完的操作回执。混在一堆里，读代码的人
// 会以为后台的回执也会出现在访客页面上。
//
// 这些文案同样要登记进 UserFacingMessages：后台页面把 service 的错误经那份白名单回显，
// 没登记的话页面上只会显示一句「操作失败，请稍后重试」——
// 运营分不清是「被拒绝了」还是「系统坏了」。
const (
	MsgCustomerEnabled  = "user.msg.customerEnabled"
	MsgCustomerDisabled = "user.msg.customerDisabled"
	MsgCustomerUnlocked = "user.msg.customerUnlocked"
	// MsgCustomerNotLocked 解锁时账号本来就没锁：如实回执，不谎报「已解除」。
	MsgCustomerNotLocked = "user.msg.customerNotLocked"
	// MsgCustomerFailuresCleared 没被锁、但有残留的失败计数被清掉：
	// 与「无需解除」分开说，否则运营会以为这个按钮什么都没做。
	MsgCustomerFailuresCleared = "user.msg.customerFailuresCleared"
	// ErrCustomerStatusInvalid 只允许「正常」与「已停用」两个目标值。
	ErrCustomerStatusInvalid = "user.err.customerStatusInvalid"
)

// UserFacingMessages 可以原样展示给访客的全部业务文案。
//
// 存在的理由：service 的业务错误来自这里，而基础设施错误（数据库 / Redis）的原文
// 可能含表名、列名甚至 SQL 片段。展示层拿这张表做白名单，
// 不在表里的一律落到 ErrInternal 并记日志 —— 白名单漏写只会显示成一句通用提示（一眼可见），
// 黑名单漏写则会把内部细节摆到访客面前（不易发现）。
//
// 新增面向访客的错误文案时必须同步加到这里，否则页面只会显示「操作失败，请稍后重试」。
var UserFacingMessages = []string{
	// 注册 / 验证
	MsgRegisterSuccess, MsgActivateSuccess,
	ErrInvalidParam, ErrUsernameRequired, ErrUsernameTooShort, ErrUsernameTooLong,
	ErrUsernameTaken, ErrEmailRequired, ErrEmailInvalid, ErrEmailTaken,
	ErrPasswordTooShort, ErrUserNotFound, ErrActivationInvalid, ErrMailUnavailable,
	// 登录 / 会话
	ErrLoginRequired, ErrBadCredentials, ErrAccountLocked, ErrAccountDisabled,
	ErrAccountPending, ErrPasswordLoginUnavailable, ErrSessionNotFound,
	ErrSessionExpired, ErrNotLoggedIn,
	// 账号中心
	ErrOldPasswordWrong, ErrNewPasswordSame, ErrProfileVisibilityInvalid,
	ErrPageSizeInvalid,
	// 后台客户管理（页面回显 + /api/customer/* 的错误）
	MsgCustomerEnabled, MsgCustomerDisabled, MsgCustomerUnlocked, MsgCustomerNotLocked,
	MsgCustomerFailuresCleared, ErrCustomerStatusInvalid,
}
