// Package userenums 用户模块的响应与错误消息（issue #36）。
package userenums

import (
	"fmt"
)

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
	MsgRegisterSuccess, MsgActivateSuccess, MsgResetMailSent,
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

// —— 账号状态 / 邮箱验证 / 订单状态的展示标签（枚举 → 展示名的唯一真源）——
//
// 为什么放在 enums 而不是 service 或 inbound/http：**这一层之上的每个出口都要用它**
//（后台页取 (key, 兜底) 交给模板取词、/api/customer/* 在 handler 就地取词），
// 各写一份必然漂移 —— 此前就有三份（http 的字面量表、service 的 customerStatusLabel、
// 页面 tabs 自带的一份 key），而其中两份没有 key，英文界面上永远是中文。
// enums 零依赖，任何层都能 import。
//
// 注意 service **不再**填 dto 的文案（原先它代填中文兜底）：service 拿不到请求语言，
// 代填的表现是接口响应恒中文而不报错。展示文案一律在出口取词。
// 先例：masterdata/enums 的 LabelProduct + EntityTypeLabel / ActionLabel / FieldLabel。
//
// **形态是 (key, fallback) 两个返回值**，不是单个文案：
//   · 只返回中文 → 英文界面恒中文（拿不到词条）；
//   · 只返回 key → 词条缺失时页面显示裸 key（`admin.customers.badge.active`）；
//   · 两个都给 → 调用点 tr(key, fallback)：命中出译文，未命中出中文兜底。
//
// 词条一律**复用库里已存在的**（admin.customers.badge.*，中英成对，迁移 190 / register_admin_i18n）；
// 本文件不新增词条、不改 seed —— 需要新词条的地方在调用点留中文兜底并登记待补清单。

// 账号状态取值。
//
// 与 usermodel.UserStatus* 同值：enums 零依赖（不 import model，否则 model 的依赖会顺着
// enums 扩散到每一个只想拿文案的层）。取值本身是 users.status 的语义，真源仍在 model。
const (
	// StatusDisabled 已停用（管理动作，不会自己解除）。
	StatusDisabled = 0
	// StatusActive 正常。
	StatusActive = 1
	// StatusPending 待激活（注册后未完成邮箱验证，登不上去）。
	StatusPending = 2
	// StatusAll 状态筛选的「不过滤」取值。
	//
	// 用 -1 而不是 0：0 是「已停用」这个**合法**的筛选值，
	// 拿 0 表示「全部」会永远筛不出停用账号（而它看起来完全正常）。
	StatusAll = -1
)

// 展示标签的词条 key（均已在 sys_i18n 中，中英成对）。
const (
	LabelKeyStatusAll      = "admin.customers.badge.all"
	LabelKeyStatusActive   = "admin.customers.badge.active"
	LabelKeyStatusDisabled = "admin.customers.badge.disabled"
	LabelKeyStatusPending  = "admin.customers.badge.pending"
	LabelKeyStatusLocked   = "admin.customers.badge.locked"
	LabelKeyVerified       = "admin.customers.badge.verified"
	LabelKeyUnverified     = "admin.customers.badge.unverified"
	// 状态说明：列表页表头 .help 与详情页的状态解释**共用同一条词条**。
	//
	// 三句话语义一一对应（待激活 / 已锁定 / 失败次数未清零），分两份就会漂移成
	// 「同一件事在两个页面上说法不同」，而运营会以为它们说的是两件事。
	LabelKeyPendingHint  = "admin.customers.status.help.pending"
	LabelKeyLockedHint   = "admin.customers.status.help.locked"
	LabelKeyFailuresHint = "admin.customers.status.help.failures"
)

// 词条缺失时的中文兜底（与库内 zh-CN 值逐字一致：不一致时，兜底生效与否会表现出两种文案）。
const (
	LabelStatusAll      = "全部"
	LabelStatusActive   = "正常"
	LabelStatusDisabled = "已停用"
	LabelStatusPending  = "待激活"
	LabelStatusLocked   = "已锁定"
	LabelVerified       = "邮箱已验证"
	LabelUnverified     = "邮箱未验证"
	// 状态说明的兜底（词条缺失时用；与库内 zh-CN 同义，与列表页表头 .help 是同一句话）。
	LabelPendingHint = "待激活：客户还没完成邮箱验证。这类账号本来就登不上去，" +
		"客户验证完邮箱后状态会变成「正常」，那时再决定是否停用。"
	LabelLockedHint = "该账号因连续登录失败被临时锁定（到点会自动解除），客户目前登不上去 —— " +
		"如果确认是本人操作，点「解除锁定」让他不用等。"
	LabelFailuresHint = "该账号有未清零的登录失败次数：再失败几次就会进入锁定。"
)

// LabelEmptyField 空字段的展示占位（表格里的空白单元格读不出「没有值」）。
const LabelEmptyField = "—"

// StatusLabel 账号状态 → (词条 key, 中文兜底)。
//
// 认不出的取值：key 留空、兜底带上原始数字。后台出现状态取值漂移时，
// 「未知状态(7)」能让人立刻去查数据，而只说「未知状态」只能引来一句「哪里未知？」；
// key 为空串时取词函数据 fallback 原样返回（pkg/i18n.Translate 的兜底链第 3 档），
// 不会退化成「显示裸 key」。
func StatusLabel(status int) (key, fallback string) {
	switch status {
	case StatusActive:
		return LabelKeyStatusActive, LabelStatusActive
	case StatusDisabled:
		return LabelKeyStatusDisabled, LabelStatusDisabled
	case StatusPending:
		return LabelKeyStatusPending, LabelStatusPending
	case StatusAll:
		return LabelKeyStatusAll, LabelStatusAll
	default:
		return "", fmt.Sprintf("未知状态(%d)", status)
	}
}

// EmailVerifiedLabel 邮箱验证 → (词条 key, 中文兜底)。
//
// 兜底带主语（「邮箱已验证」而不是「已验证」）：这个徽章与状态徽章并排出现，
// 少一个词就读成「账号已验证」。
func EmailVerifiedLabel(verified bool) (key, fallback string) {
	if verified {
		return LabelKeyVerified, LabelVerified
	}
	return LabelKeyUnverified, LabelUnverified
}

// 订单状态的展示标签**不在这里**：真源是 order 模块的 enums
//（orderenums.OrderStatusLabel），跨模块经 ordercontract.OrderStatusLabel 引用 ——
// 后台客户页的「最近一单」与订单页必须共用同一份映射（各写一张表的结果是
// 「改一处、另一处静默留在旧说法上」），而 user 模块 import 别的模块的 enums
// 会破坏「跨模块只用 contract/dto」的边界。
