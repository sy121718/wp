// Package userenums 用户模块的响应与错误消息（issue #36）。
package userenums

const (
	MsgRegisterSuccess = "注册成功，请查收验证邮件"
	MsgActivateSuccess = "邮箱验证成功"
	MsgLoginSuccess    = "登录成功"
	MsgLogoutSuccess   = "已退出登录"
	MsgProfileSaved    = "资料已保存"
	MsgPreferenceSaved = "偏好已保存"
	MsgPasswordChanged = "密码已修改，其它设备已退出登录"
	MsgResetMailSent   = "如果该邮箱已注册，重置邮件已发出"
)

const (
	ErrInvalidParam      = "参数不合法"
	ErrUsernameRequired  = "用户名不能为空"
	ErrUsernameTooShort  = "用户名至少 3 个字符"
	ErrUsernameTooLong   = "用户名最多 32 个字符"
	ErrUsernameTaken     = "用户名已被占用"
	ErrEmailRequired     = "邮箱不能为空"
	ErrEmailInvalid      = "邮箱格式不正确"
	ErrEmailTaken        = "邮箱已被注册"
	ErrPasswordTooShort  = "密码至少 8 个字符"
	ErrUserNotFound      = "用户不存在"
	ErrActivationInvalid = "验证链接无效或已过期"
	ErrMailUnavailable   = "验证邮件暂时发不出去，请稍后重试或联系管理员"
)

// 登录与会话。
const (
	// ErrLoginRequired 账号或密码为空（与「凭据不对」分开：这是表单没填全，不是密码错）。
	ErrLoginRequired = "请输入账号和密码"
	// ErrBadCredentials 凭据错误。**用户不存在与密码错误必须共用这一条** ——
	// 区分它们等于把登录接口做成账号枚举器。
	ErrBadCredentials = "账号或密码不正确"
	// ErrAccountLocked 连续失败被锁定（文案里的 %s 由调用处填入剩余时间）。
	ErrAccountLocked = "账号已锁定，请在 %s 后重试"
	// ErrAccountDisabled 被管理侧禁用（与「锁定」不同：禁用不会自己解除）。
	ErrAccountDisabled = "账号已被禁用"
	// ErrAccountPending 注册后未完成邮箱验证。
	ErrAccountPending = "账号尚未完成邮箱验证，请先查收验证邮件"
	// ErrPasswordLoginUnavailable 第三方注册的账号没有本站密码。
	//
	// 与「密码错误」分开说：真正的密码错误用户可以重试，而这个账号根本没有密码可试，
	// 混在一起会让人陷在「改了密码还是登不上」里。
	ErrPasswordLoginUnavailable = "该账号不支持密码登录，请使用第三方账号登录"
	ErrSessionNotFound          = "登录设备不存在或已被移除"
	ErrSessionExpired           = "登录已过期，请重新登录"
	ErrOldPasswordWrong         = "当前密码不正确"
	ErrNewPasswordSame          = "新密码不能与当前密码相同"
	ErrProfileVisibilityInvalid = "主页可见性取值不合法"
	ErrPageSizeInvalid          = "每页条数超出允许范围"
	ErrNotLoggedIn              = "请先登录"
	// ErrInternal 未归类的系统错误对外统一文案。
	//
	// 存在的理由：基础设施错误（数据库 / Redis）的原文可能带表名、列名甚至 SQL 片段，
	// 那是给运维看的，不是给访客看的。展示层据此把「不是本模块业务文案」的错误全部落到这一条。
	ErrInternal = "操作失败，请稍后重试"
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
	MsgCustomerEnabled  = "账号已启用"
	MsgCustomerDisabled = "账号已停用"
	MsgCustomerUnlocked = "账号已解除锁定"
	// MsgCustomerNotLocked 解锁时账号本来就没锁：如实回执，不谎报「已解除」。
	MsgCustomerNotLocked = "该账号没有处于锁定状态，无需解除"
	// MsgCustomerFailuresCleared 没被锁、但有残留的失败计数被清掉：
	// 与「无需解除」分开说，否则运营会以为这个按钮什么都没做。
	MsgCustomerFailuresCleared = "账号未处于锁定状态，登录失败计数已清零"
	// ErrCustomerStatusInvalid 只允许「正常」与「已停用」两个目标值。
	ErrCustomerStatusInvalid = "账号状态取值不合法"
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
