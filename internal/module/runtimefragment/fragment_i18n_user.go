package runtimefragment

// fragment_i18n_user.go — 访客账号表单片段文案（I18N-012，site.fragment.user.*）。

// userCommonLabels 账号类片段共用文案。
type userCommonLabels struct {
	SessionNotReady string
	GoLogin         string
}

func userCommonLabelsOf(r *Request) userCommonLabels {
	return userCommonLabels{
		SessionNotReady: r.tr("site.fragment.common.session_not_ready", "会话未就绪，请刷新页面后重试。"),
		GoLogin:         r.tr("site.fragment.common.go_login", "去登录"),
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
		Identity:         r.tr("site.fragment.user.login.identity", "邮箱或用户名"),
		Password:         r.tr("site.fragment.user.login.password", "密码"),
		Remember:         r.tr("site.fragment.user.login.remember", "记住我（7 天）"),
		Submit:           r.tr("site.fragment.user.login.submit", "登录"),
		NoAccount:        r.tr("site.fragment.user.login.no_account", "还没有账号？"),
		Register:         r.tr("site.fragment.user.login.register", "去注册"),
		Forgot:           r.tr("site.fragment.user.login.forgot", "忘记密码？"),
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
		Username:         r.tr("site.fragment.user.register.username", "用户名"),
		Email:            r.tr("site.fragment.user.register.email", "邮箱"),
		Nickname:         r.tr("site.fragment.user.register.nickname", "昵称（可选）"),
		Password:         r.tr("site.fragment.user.register.password", "密码"),
		Submit:           r.tr("site.fragment.user.register.submit", "注册"),
		AfterNote:        r.tr("site.fragment.user.register.after_note", "注册后需要去邮箱点激活链接才能登录。"),
		HasAccount:       r.tr("site.fragment.user.register.has_account", "已经有账号？"),
		Login:            r.tr("site.fragment.user.register.login", "去登录"),
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
		Email:            r.tr("site.fragment.user.forgot.email", "注册邮箱"),
		Submit:           r.tr("site.fragment.user.forgot.submit", "发送重置链接"),
		Privacy:          r.tr("site.fragment.user.forgot.privacy", "无论邮箱是否存在，提交后都会显示同一句提示 —— 那样可以避免被人拿来试探哪些邮箱注册过。"),
		BackLogin:        r.tr("site.fragment.user.forgot.back_login", "返回登录"),
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
		Email:            r.tr("site.fragment.user.reset.email", "邮箱"),
		Password:         r.tr("site.fragment.user.reset.password", "新密码"),
		Submit:           r.tr("site.fragment.user.reset.submit", "设置新密码"),
		RevokeNote:       r.tr("site.fragment.user.reset.revoke_note", "改完密码会撤销该账号的全部登录会话（包括其它设备）。"),
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
		LoggedIn:         r.tr("site.fragment.user.panel.logged_in", "你已登录。"),
		AccountCenter:    r.tr("site.fragment.user.panel.account_center", "进入账号中心"),
		AccountHint:      r.tr("site.fragment.user.panel.account_hint", "（资料 / 偏好 / 改密码 / 登录设备）"),
		Logout:           r.tr("site.fragment.user.panel.logout", "退出登录"),
		Guest:            r.tr("site.fragment.user.panel.guest", "你还没有登录。"),
		NoAccount:        r.tr("site.fragment.user.panel.no_account", "没有账号？"),
		Register:         r.tr("site.fragment.user.panel.register", "注册一个"),
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
		NeedLogin:        r.tr("site.fragment.user.account.need_login_profile", "请先登录后再查看账号资料。"),
		Unavailable:      r.tr("site.fragment.user.account.unavailable", "账号资料暂不可用，请稍后再试。"),
		Username:         r.tr("site.fragment.user.account.username", "用户名"),
		Email:            r.tr("site.fragment.user.account.email", "邮箱"),
		Verified:         r.tr("site.fragment.user.account.verified", "已验证"),
		Unverified:       r.tr("site.fragment.user.account.unverified", "未验证"),
		RegisteredAt:     r.tr("site.fragment.user.account.registered_at", "注册时间"),
		LastLogin:        r.tr("site.fragment.user.account.last_login", "上次登录"),
		Nickname:         r.tr("site.fragment.user.account.nickname", "昵称"),
		Gender:           r.tr("site.fragment.user.account.gender", "性别"),
		GenderUnset:      r.tr("site.fragment.user.account.gender_unset", "未填写"),
		GenderMale:       r.tr("site.fragment.user.account.gender_male", "男"),
		GenderFemale:     r.tr("site.fragment.user.account.gender_female", "女"),
		FirstName:        r.tr("site.fragment.user.account.first_name", "名"),
		LastName:         r.tr("site.fragment.user.account.last_name", "姓"),
		Birthday:         r.tr("site.fragment.user.account.birthday", "生日"),
		Phone:            r.tr("site.fragment.user.account.phone", "电话"),
		Website:          r.tr("site.fragment.user.account.website", "个人主页"),
		Company:          r.tr("site.fragment.user.account.company", "公司 / 组织"),
		Country:          r.tr("site.fragment.user.account.country", "国家 / 地区"),
		Province:         r.tr("site.fragment.user.account.province", "省 / 州"),
		City:             r.tr("site.fragment.user.account.city", "城市"),
		Zip:              r.tr("site.fragment.user.account.zip", "邮编"),
		Address:          r.tr("site.fragment.user.account.address", "地址"),
		Bio:              r.tr("site.fragment.user.account.bio", "简介"),
		Save:             r.tr("site.fragment.user.account.save_profile", "保存资料"),
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
		NeedLogin:         r.tr("site.fragment.user.account.need_login_preference", "请先登录后再查看账号偏好。"),
		Unavailable:       r.tr("site.fragment.user.account.unavailable_preference", "账号偏好暂不可用，请稍后再试。"),
		PageSize:          r.tr("site.fragment.user.account.page_size", "每页条数"),
		Visibility:        r.tr("site.fragment.user.account.visibility", "主页可见性"),
		VisibilityPublic:  r.tr("site.fragment.user.account.visibility_public", "所有人可见"),
		VisibilityMembers: r.tr("site.fragment.user.account.visibility_members", "仅登录用户"),
		VisibilityPrivate: r.tr("site.fragment.user.account.visibility_private", "仅自己"),
		Timezone:          r.tr("site.fragment.user.account.timezone", "时区"),
		EmailNotify:       r.tr("site.fragment.user.account.email_notify", "接收邮件通知"),
		SmsNotify:         r.tr("site.fragment.user.account.sms_notify", "接收短信通知"),
		ShowOnline:        r.tr("site.fragment.user.account.show_online", "公开我的在线状态"),
		Save:              r.tr("site.fragment.user.account.save_preference", "保存偏好"),
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
		NeedLogin:        r.tr("site.fragment.user.account.need_login_password", "请先登录后再修改密码。"),
		Current:          r.tr("site.fragment.user.account.current_password", "当前密码"),
		NewPassword:      r.tr("site.fragment.user.account.new_password", "新密码"),
		Hint:             r.tr("site.fragment.user.account.password_hint", "至少 8 位。修改成功后所有设备都会退出登录。"),
		Submit:           r.tr("site.fragment.user.account.change_password", "修改密码"),
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
		Unavailable:      r.tr("site.fragment.user.account.sessions_unavailable", "登录设备暂不可用，请稍后再试。"),
		NeedLogin:        r.tr("site.fragment.user.account.need_login_sessions", "请先登录后再查看登录设备。"),
		Empty:            r.tr("site.fragment.user.account.sessions_empty", "没有其它登录设备。"),
		UnknownBrowser:   r.tr("site.fragment.user.account.unknown_browser", "未知浏览器"),
		CurrentDevice:    r.tr("site.fragment.user.account.current_device", "当前设备"),
		IPUnknown:        r.tr("site.fragment.user.account.ip_unknown", "IP 未知"),
		LastActive:       r.tr("site.fragment.user.account.last_active", "最近活跃"),
		LoggedInAt:       r.tr("site.fragment.user.account.logged_in_at", "登录于"),
		Revoke:           r.tr("site.fragment.user.account.revoke", "踢出"),
		RevokeOthers:     r.tr("site.fragment.user.account.revoke_others", "退出其它所有设备"),
	}
}
