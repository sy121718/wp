package runtimefragment

import rfenums "go_wp/internal/module/runtimefragment/enums"

// fragment_i18n_user.go — 访客账号表单片段文案（I18N-012，site.fragment.user.*）。

// userCommonLabels 账号类片段共用文案。
type userCommonLabels struct {
	SessionNotReady string
	GoLogin         string
}

func userCommonLabelsOf(r *Request) userCommonLabels {
	return userCommonLabels{
		SessionNotReady: r.tr(rfenums.CommonSessionNotReady, "会话未就绪，请刷新页面后重试。"),
		GoLogin:         r.tr(rfenums.CommonGoLogin, "去登录"),
	}
}

// userLoginLabels 登录表单。
type userLoginLabels struct {
	userCommonLabels
	Identity  string
	Password  string
	Remember  string
	Submit    string
	NoAccount string
	Register  string
	Forgot    string
}

func userLoginLabelsOf(r *Request) userLoginLabels {
	return userLoginLabels{
		userCommonLabels: userCommonLabelsOf(r),
		Identity:         r.tr(rfenums.UserLoginIdentity, "邮箱或用户名"),
		Password:         r.tr(rfenums.UserLoginPassword, "密码"),
		Remember:         r.tr(rfenums.UserLoginRemember, "记住我（7 天）"),
		Submit:           r.tr(rfenums.UserLoginSubmit, "登录"),
		NoAccount:        r.tr(rfenums.UserLoginNoAccount, "还没有账号？"),
		Register:         r.tr(rfenums.UserLoginRegister, "去注册"),
		Forgot:           r.tr(rfenums.UserLoginForgot, "忘记密码？"),
	}
}

// userRegisterLabels 注册表单。
type userRegisterLabels struct {
	userCommonLabels
	Username   string
	Email      string
	Nickname   string
	Password   string
	Submit     string
	AfterNote  string
	HasAccount string
	Login      string
}

func userRegisterLabelsOf(r *Request) userRegisterLabels {
	return userRegisterLabels{
		userCommonLabels: userCommonLabelsOf(r),
		Username:         r.tr(rfenums.UserRegisterUsername, "用户名"),
		Email:            r.tr(rfenums.UserRegisterEmail, "邮箱"),
		Nickname:         r.tr(rfenums.UserRegisterNickname, "昵称（可选）"),
		Password:         r.tr(rfenums.UserRegisterPassword, "密码"),
		Submit:           r.tr(rfenums.UserRegisterSubmit, "注册"),
		AfterNote:        r.tr(rfenums.UserRegisterAfterNote, "注册后需要去邮箱点激活链接才能登录。"),
		HasAccount:       r.tr(rfenums.UserRegisterHasAccount, "已经有账号？"),
		Login:            r.tr(rfenums.UserRegisterLogin, "去登录"),
	}
}

// userForgotLabels 忘记密码。
type userForgotLabels struct {
	userCommonLabels
	Email     string
	Submit    string
	Privacy   string
	BackLogin string
}

func userForgotLabelsOf(r *Request) userForgotLabels {
	return userForgotLabels{
		userCommonLabels: userCommonLabelsOf(r),
		Email:            r.tr(rfenums.UserForgotEmail, "注册邮箱"),
		Submit:           r.tr(rfenums.UserForgotSubmit, "发送重置链接"),
		Privacy:          r.tr(rfenums.UserForgotPrivacy, "无论邮箱是否存在，提交后都会显示同一句提示 —— 那样可以避免被人拿来试探哪些邮箱注册过。"),
		BackLogin:        r.tr(rfenums.UserForgotBackLogin, "返回登录"),
	}
}

// userResetLabels 重置密码。
type userResetLabels struct {
	userCommonLabels
	Email      string
	Password   string
	Submit     string
	RevokeNote string
}

func userResetLabelsOf(r *Request) userResetLabels {
	return userResetLabels{
		userCommonLabels: userCommonLabelsOf(r),
		Email:            r.tr(rfenums.UserResetEmail, "邮箱"),
		Password:         r.tr(rfenums.UserResetPassword, "新密码"),
		Submit:           r.tr(rfenums.UserResetSubmit, "设置新密码"),
		RevokeNote:       r.tr(rfenums.UserResetRevokeNote, "改完密码会撤销该账号的全部登录会话（包括其它设备）。"),
	}
}

// userAccountPanelLabels 账号面板。
type userAccountPanelLabels struct {
	userCommonLabels
	LoggedIn      string
	AccountCenter string
	AccountHint   string
	Logout        string
	Guest         string
	NoAccount     string
	Register      string
}

func userAccountPanelLabelsOf(r *Request) userAccountPanelLabels {
	return userAccountPanelLabels{
		userCommonLabels: userCommonLabelsOf(r),
		LoggedIn:         r.tr(rfenums.UserPanelLoggedIn, "你已登录。"),
		AccountCenter:    r.tr(rfenums.UserPanelAccountCenter, "进入账号中心"),
		AccountHint:      r.tr(rfenums.UserPanelAccountHint, "（资料 / 偏好 / 改密码 / 登录设备）"),
		Logout:           r.tr(rfenums.UserPanelLogout, "退出登录"),
		Guest:            r.tr(rfenums.UserPanelGuest, "你还没有登录。"),
		NoAccount:        r.tr(rfenums.UserPanelNoAccount, "没有账号？"),
		Register:         r.tr(rfenums.UserPanelRegister, "注册一个"),
	}
}

// userAccountProfileLabels 资料表单。
type userAccountProfileLabels struct {
	userCommonLabels
	NeedLogin    string
	Unavailable  string
	Username     string
	Email        string
	Verified     string
	Unverified   string
	RegisteredAt string
	LastLogin    string
	Nickname     string
	Gender       string
	GenderUnset  string
	GenderMale   string
	GenderFemale string
	FirstName    string
	LastName     string
	Birthday     string
	Phone        string
	Website      string
	Company      string
	Country      string
	Province     string
	City         string
	Zip          string
	Address      string
	Bio          string
	Save         string
}

func userAccountProfileLabelsOf(r *Request) userAccountProfileLabels {
	return userAccountProfileLabels{
		userCommonLabels: userCommonLabelsOf(r),
		NeedLogin:        r.tr(rfenums.UserAccountNeedLoginProfile, "请先登录后再查看账号资料。"),
		Unavailable:      r.tr(rfenums.UserAccountUnavailable, "账号资料暂不可用，请稍后再试。"),
		Username:         r.tr(rfenums.UserAccountUsername, "用户名"),
		Email:            r.tr(rfenums.UserAccountEmail, "邮箱"),
		Verified:         r.tr(rfenums.UserAccountVerified, "已验证"),
		Unverified:       r.tr(rfenums.UserAccountUnverified, "未验证"),
		RegisteredAt:     r.tr(rfenums.UserAccountRegisteredAt, "注册时间"),
		LastLogin:        r.tr(rfenums.UserAccountLastLogin, "上次登录"),
		Nickname:         r.tr(rfenums.UserAccountNickname, "昵称"),
		Gender:           r.tr(rfenums.UserAccountGender, "性别"),
		GenderUnset:      r.tr(rfenums.UserAccountGenderUnset, "未填写"),
		GenderMale:       r.tr(rfenums.UserAccountGenderMale, "男"),
		GenderFemale:     r.tr(rfenums.UserAccountGenderFemale, "女"),
		FirstName:        r.tr(rfenums.UserAccountFirstName, "名"),
		LastName:         r.tr(rfenums.UserAccountLastName, "姓"),
		Birthday:         r.tr(rfenums.UserAccountBirthday, "生日"),
		Phone:            r.tr(rfenums.UserAccountPhone, "电话"),
		Website:          r.tr(rfenums.UserAccountWebsite, "个人主页"),
		Company:          r.tr(rfenums.UserAccountCompany, "公司 / 组织"),
		Country:          r.tr(rfenums.UserAccountCountry, "国家 / 地区"),
		Province:         r.tr(rfenums.UserAccountProvince, "省 / 州"),
		City:             r.tr(rfenums.UserAccountCity, "城市"),
		Zip:              r.tr(rfenums.UserAccountZip, "邮编"),
		Address:          r.tr(rfenums.UserAccountAddress, "地址"),
		Bio:              r.tr(rfenums.UserAccountBio, "简介"),
		Save:             r.tr(rfenums.UserAccountSaveProfile, "保存资料"),
	}
}

// userAccountPreferenceLabels 偏好表单。
type userAccountPreferenceLabels struct {
	userCommonLabels
	NeedLogin         string
	Unavailable       string
	PageSize          string
	Visibility        string
	VisibilityPublic  string
	VisibilityMembers string
	VisibilityPrivate string
	Timezone          string
	EmailNotify       string
	SmsNotify         string
	ShowOnline        string
	Save              string
}

func userAccountPreferenceLabelsOf(r *Request) userAccountPreferenceLabels {
	return userAccountPreferenceLabels{
		userCommonLabels:  userCommonLabelsOf(r),
		NeedLogin:         r.tr(rfenums.UserAccountNeedLoginPreference, "请先登录后再查看账号偏好。"),
		Unavailable:       r.tr(rfenums.UserAccountUnavailablePreference, "账号偏好暂不可用，请稍后再试。"),
		PageSize:          r.tr(rfenums.UserAccountPageSize, "每页条数"),
		Visibility:        r.tr(rfenums.UserAccountVisibility, "主页可见性"),
		VisibilityPublic:  r.tr(rfenums.UserAccountVisibilityPublic, "所有人可见"),
		VisibilityMembers: r.tr(rfenums.UserAccountVisibilityMembers, "仅登录用户"),
		VisibilityPrivate: r.tr(rfenums.UserAccountVisibilityPrivate, "仅自己"),
		Timezone:          r.tr(rfenums.UserAccountTimezone, "时区"),
		EmailNotify:       r.tr(rfenums.UserAccountEmailNotify, "接收邮件通知"),
		SmsNotify:         r.tr(rfenums.UserAccountSmsNotify, "接收短信通知"),
		ShowOnline:        r.tr(rfenums.UserAccountShowOnline, "公开我的在线状态"),
		Save:              r.tr(rfenums.UserAccountSavePreference, "保存偏好"),
	}
}

// userAccountPasswordLabels 改密码。
type userAccountPasswordLabels struct {
	userCommonLabels
	NeedLogin   string
	Current     string
	NewPassword string
	Hint        string
	Submit      string
}

func userAccountPasswordLabelsOf(r *Request) userAccountPasswordLabels {
	return userAccountPasswordLabels{
		userCommonLabels: userCommonLabelsOf(r),
		NeedLogin:        r.tr(rfenums.UserAccountNeedLoginPassword, "请先登录后再修改密码。"),
		Current:          r.tr(rfenums.UserAccountCurrentPassword, "当前密码"),
		NewPassword:      r.tr(rfenums.UserAccountNewPassword, "新密码"),
		Hint:             r.tr(rfenums.UserAccountPasswordHint, "至少 8 位。修改成功后所有设备都会退出登录。"),
		Submit:           r.tr(rfenums.UserAccountChangePassword, "修改密码"),
	}
}

// userAccountSessionsLabels 登录设备。
type userAccountSessionsLabels struct {
	userCommonLabels
	Unavailable    string
	NeedLogin      string
	Empty          string
	UnknownBrowser string
	CurrentDevice  string
	IPUnknown      string
	LastActive     string
	LoggedInAt     string
	Revoke         string
	RevokeOthers   string
}

func userAccountSessionsLabelsOf(r *Request) userAccountSessionsLabels {
	return userAccountSessionsLabels{
		userCommonLabels: userCommonLabelsOf(r),
		Unavailable:      r.tr(rfenums.UserAccountSessionsUnavailable, "登录设备暂不可用，请稍后再试。"),
		NeedLogin:        r.tr(rfenums.UserAccountNeedLoginSessions, "请先登录后再查看登录设备。"),
		Empty:            r.tr(rfenums.UserAccountSessionsEmpty, "没有其它登录设备。"),
		UnknownBrowser:   r.tr(rfenums.UserAccountUnknownBrowser, "未知浏览器"),
		CurrentDevice:    r.tr(rfenums.UserAccountCurrentDevice, "当前设备"),
		IPUnknown:        r.tr(rfenums.UserAccountIPUnknown, "IP 未知"),
		LastActive:       r.tr(rfenums.UserAccountLastActive, "最近活跃"),
		LoggedInAt:       r.tr(rfenums.UserAccountLoggedInAt, "登录于"),
		Revoke:           r.tr(rfenums.UserAccountRevoke, "踢出"),
		RevokeOthers:     r.tr(rfenums.UserAccountRevokeOthers, "退出其它所有设备"),
	}
}
