package userenums

import (
	"fmt"
	"go_wp/pkg/facing"
	"net/http"
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

const (
	ErrLoginRequired = "user.err.loginRequired"

	ErrBadCredentials = "user.err.badCredentials"

	ErrAccountLocked = "user.err.accountLocked"

	ErrAccountDisabled = "user.err.accountDisabled"

	ErrAccountPending = "user.err.accountPending"

	ErrPasswordLoginUnavailable = "user.err.passwordLoginUnavailable"
	ErrSessionNotFound          = "user.err.sessionNotFound"
	ErrSessionExpired           = "user.err.sessionExpired"
	ErrLogoutFailed             = "user.err.logoutFailed"
	ErrOldPasswordWrong         = "user.err.oldPasswordWrong"
	ErrNewPasswordSame          = "user.err.newPasswordSame"
	ErrProfileVisibilityInvalid = "user.err.profileVisibilityInvalid"
	ErrPageSizeInvalid          = "user.err.pageSizeInvalid"
	ErrNotLoggedIn              = "user.err.notLoggedIn"

	ErrInternal = "user.err.internal"
)

const (
	MsgCustomerEnabled  = "user.msg.customerEnabled"
	MsgCustomerDisabled = "user.msg.customerDisabled"
	MsgCustomerUnlocked = "user.msg.customerUnlocked"

	MsgCustomerNotLocked = "user.msg.customerNotLocked"

	MsgCustomerFailuresCleared = "user.msg.customerFailuresCleared"
	MsgCustomerBulkUnlock      = "user.msg.customerBulkUnlock"

	ErrCustomerStatusInvalid = "user.err.customerStatusInvalid"
)

const (
	StatusDisabled = 0

	StatusActive = 1

	StatusPending = 2

	StatusAll = -1
)

const (
	LabelKeyStatusAll      = "admin.customers.badge.all"
	LabelKeyStatusActive   = "admin.customers.badge.active"
	LabelKeyStatusDisabled = "admin.customers.badge.disabled"
	LabelKeyStatusPending  = "admin.customers.badge.pending"
	LabelKeyStatusLocked   = "admin.customers.badge.locked"
	LabelKeyVerified       = "admin.customers.badge.verified"
	LabelKeyUnverified     = "admin.customers.badge.unverified"

	LabelKeyPendingHint  = "admin.customers.status.help.pending"
	LabelKeyLockedHint   = "admin.customers.status.help.locked"
	LabelKeyFailuresHint = "admin.customers.status.help.failures"
)

const (
	KeyCustomerCohortTitle              = "admin.customer.cohort.title"
	KeyCustomerCohortUnavailable        = "admin.customer.cohort.unavailable"
	KeyCustomersHeading                 = "admin.customers.heading"
	KeyCustomerDetailHeading            = "admin.customer_detail.heading"
	KeyCustomerActionDisable            = "admin.customers.action.disable"
	KeyCustomerActionEnable             = "admin.customers.action.enable"
	KeyCustomerCapabilityMissing        = "admin.customers.capability_missing"
	KeyCustomerFormInvalidID            = "admin.customers.form.invalid_id"
	KeyCustomerOrdersUnavailable        = "admin.customer_detail.orders.unavailable"
	KeyCustomerBulkNoneSelected         = "admin.customers.bulk.none_selected"
	KeyCustomerFilterRfmUnavailable     = "admin.customer.filter.rfm_unavailable"
	KeyCustomerFilterSegmentUnavailable = "admin.customer.filter.segment_unavailable"
	KeyCustomerFilterTierTruncated      = "admin.customer.filter.tier_truncated"
	KeyCustomerFilterTierUnavailable    = "admin.customer.filter.tier_unavailable"
	KeyCustomerOverviewTitle            = "admin.customer.overview.title"
	KeyCustomerRfmTitle                 = "admin.customer.rfm.title"
	KeyCustomerRfmTabAll                = "admin.customer.rfm.tab.all"
	KeyCustomerRfmSegmentVip            = "admin.customer.rfm.segment.vip"
	KeyCustomerRfmSegmentPotential      = "admin.customer.rfm.segment.potential"
	KeyCustomerRfmSegmentLowValue       = "admin.customer.rfm.segment.lowValue"
	KeyCustomerRfmUnavailable           = "admin.customer.rfm.unavailable"
	KeyCustomerMembershipUnavailable    = "admin.customer_detail.membership.unavailable"
	KeyCustomerMembershipNoProject      = "admin.customer_detail.membership.no_project"
	KeyCustomerMembershipFailed         = "admin.customer_detail.membership.failed"
)

const (
	LabelStatusAll      = "全部"
	LabelStatusActive   = "正常"
	LabelStatusDisabled = "已停用"
	LabelStatusPending  = "待激活"
	LabelStatusLocked   = "已锁定"
	LabelVerified       = "邮箱已验证"
	LabelUnverified     = "邮箱未验证"

	LabelPendingHint = "待激活：客户还没完成邮箱验证。这类账号本来就登不上去，" +
		"客户验证完邮箱后状态会变成「正常」，那时再决定是否停用。"
	LabelLockedHint = "该账号因连续登录失败被临时锁定（到点会自动解除），客户目前登不上去 —— " +
		"如果确认是本人操作，点「解除锁定」让他不用等。"
	LabelFailuresHint = "该账号有未清零的登录失败次数：再失败几次就会进入锁定。"
)

const LabelEmptyField = "—"

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

func EmailVerifiedLabel(verified bool) (key, fallback string) {
	if verified {
		return LabelKeyVerified, LabelVerified
	}
	return LabelKeyUnverified, LabelUnverified
}

var errFallbacks = map[string]string{
	ErrInvalidParam:             "参数不合法",
	ErrUsernameRequired:         "用户名不能为空",
	ErrUsernameTooShort:         "用户名至少 3 个字符",
	ErrUsernameTooLong:          "用户名最多 32 个字符",
	ErrUsernameTaken:            "用户名已被占用",
	ErrEmailRequired:            "邮箱不能为空",
	ErrEmailInvalid:             "邮箱格式不正确",
	ErrEmailTaken:               "邮箱已被注册",
	ErrPasswordTooShort:         "密码至少 8 个字符",
	ErrUserNotFound:             "用户不存在",
	ErrActivationInvalid:        "验证链接无效或已过期",
	ErrMailUnavailable:          "验证邮件暂时发不出去，请稍后重试或联系管理员",
	ErrLoginRequired:            "请输入账号和密码",
	ErrBadCredentials:           "账号或密码不正确",
	ErrAccountLocked:            "账号已锁定，请在 %s 后重试",
	ErrAccountDisabled:          "账号已被禁用",
	ErrAccountPending:           "账号尚未完成邮箱验证，请先查收验证邮件",
	ErrPasswordLoginUnavailable: "该账号不支持密码登录，请使用第三方账号登录",
	ErrSessionNotFound:          "登录设备不存在或已被移除",
	ErrSessionExpired:           "登录已过期，请重新登录",
	ErrLogoutFailed:             "退出登录失败，请重试",
	ErrOldPasswordWrong:         "当前密码不正确",
	ErrNewPasswordSame:          "新密码不能与当前密码相同",
	ErrProfileVisibilityInvalid: "主页可见性取值不合法",
	ErrPageSizeInvalid:          "每页条数超出允许范围",
	ErrNotLoggedIn:              "请先登录",
	ErrInternal:                 "操作失败，请稍后重试",
	ErrCustomerStatusInvalid:    "账号状态取值不合法",
}

func Err(key string) error {
	status := http.StatusBadRequest
	switch key {
	case ErrNotLoggedIn, ErrBadCredentials, ErrSessionExpired:
		status = http.StatusUnauthorized
	case ErrAccountLocked, ErrAccountDisabled, ErrAccountPending, ErrPasswordLoginUnavailable:
		status = http.StatusForbidden
	case ErrUserNotFound, ErrSessionNotFound:
		status = http.StatusNotFound
	case ErrUsernameTaken, ErrEmailTaken:
		status = http.StatusConflict
	case ErrInternal, ErrMailUnavailable, ErrLogoutFailed:
		status = http.StatusInternalServerError
	}
	return facing.WithStatus(facing.New(key, errFallbacks[key]), status)
}

func FallbackFor(key string) string { return errFallbacks[key] }
