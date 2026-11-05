package userforms

type userCommonLabels struct {
	SessionNotReady string
	GoLogin         string
}

func userCommonLabelsOf(text func(string, string) string) userCommonLabels {
	return userCommonLabels{
		SessionNotReady: text("site.fragment.common.session_not_ready", "会话未就绪，请刷新页面后重试。"),
		GoLogin:         text("site.fragment.common.go_login", "去登录"),
	}
}

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

func userLoginLabelsOf(text func(string, string) string) userLoginLabels {
	return userLoginLabels{
		userCommonLabels: userCommonLabelsOf(text),
		Identity:         text("site.fragment.user.login.identity", "邮箱或用户名"),
		Password:         text("site.fragment.user.login.password", "密码"),
		Remember:         text("site.fragment.user.login.remember", "记住我（7 天）"),
		Submit:           text("site.fragment.user.login.submit", "登录"),
		NoAccount:        text("site.fragment.user.login.no_account", "还没有账号？"),
		Register:         text("site.fragment.user.login.register", "去注册"),
		Forgot:           text("site.fragment.user.login.forgot", "忘记密码？"),
	}
}

type userRegisterLabels struct {
	userCommonLabels
	Username     string
	Email        string
	Nickname     string
	Password     string
	Submit       string
	AfterNote    string
	HasAccount   string
	Login        string
	VerifyCode   string
	VerifySubmit string
	VerifyNote   string
	Resend       string
	HaveCode     string
	BackRegister string
	MailFailed   string
	Activated    string
}

func userRegisterLabelsOf(text func(string, string) string) userRegisterLabels {
	return userRegisterLabels{
		userCommonLabels: userCommonLabelsOf(text),
		Username:         text("site.fragment.user.register.username", "用户名"),
		Email:            text("site.fragment.user.register.email", "邮箱"),
		Nickname:         text("site.fragment.user.register.nickname", "昵称（可选）"),
		Password:         text("site.fragment.user.register.password", "密码"),
		Submit:           text("site.fragment.user.register.submit", "注册"),
		AfterNote:        text("site.fragment.user.register.after_note", "注册后在这里输入邮箱验证码，完成账号激活。"),
		HasAccount:       text("site.fragment.user.register.has_account", "已经有账号？"),
		Login:            text("site.fragment.user.register.login", "去登录"),
		VerifyCode:       text("site.fragment.user.register.verify_code", "邮箱验证码"),
		VerifySubmit:     text("site.fragment.user.register.verify_submit", "完成激活"),
		VerifyNote:       text("site.fragment.user.register.verify_note", "请输入邮箱收到的 8 位验证码。"),
		Resend:           text("site.fragment.user.register.resend", "重新发送验证码"),
		HaveCode:         text("site.fragment.user.register.have_code", "已有验证码？"),
		BackRegister:     text("site.fragment.user.register.back_register", "返回注册"),
		MailFailed:       text("site.fragment.user.register.mail_failed", "账号已创建，验证邮件暂未发送，请重试发送验证码。"),
		Activated:        text("site.fragment.user.register.activated", "邮箱验证成功，请登录。"),
	}
}

type userForgotLabels struct {
	userCommonLabels
	Email     string
	Submit    string
	Privacy   string
	BackLogin string
	Sent      string
}

func userForgotLabelsOf(text func(string, string) string) userForgotLabels {
	return userForgotLabels{
		userCommonLabels: userCommonLabelsOf(text),
		Email:            text("site.fragment.user.forgot.email", "注册邮箱"),
		Submit:           text("site.fragment.user.forgot.submit", "发送重置验证码"),
		Privacy:          text("site.fragment.user.forgot.privacy", "如果该邮箱已注册，你将收到一封包含重置验证码的邮件。"),
		BackLogin:        text("site.fragment.user.forgot.back_login", "返回登录"),
		Sent:             text("site.fragment.user.forgot.sent", "若邮箱已注册，请查收 8 位验证码并设置新密码。"),
	}
}

type userResetLabels struct {
	userCommonLabels
	Email      string
	Code       string
	Password   string
	Submit     string
	RevokeNote string
}

func userResetLabelsOf(text func(string, string) string) userResetLabels {
	return userResetLabels{
		userCommonLabels: userCommonLabelsOf(text),
		Email:            text("site.fragment.user.reset.email", "邮箱"),
		Code:             text("site.fragment.user.reset.code", "邮箱验证码"),
		Password:         text("site.fragment.user.reset.password", "新密码"),
		Submit:           text("site.fragment.user.reset.submit", "设置新密码"),
		RevokeNote:       text("site.fragment.user.reset.revoke_note", "改完密码会撤销该账号的全部登录会话（包括其它设备）。"),
	}
}

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

func userAccountPanelLabelsOf(text func(string, string) string) userAccountPanelLabels {
	return userAccountPanelLabels{
		userCommonLabels: userCommonLabelsOf(text),
		LoggedIn:         text("site.fragment.user.panel.logged_in", "你已登录。"),
		AccountCenter:    text("site.fragment.user.panel.account_center", "进入账号中心"),
		AccountHint:      text("site.fragment.user.panel.account_hint", "（资料 / 偏好 / 改密码 / 登录设备）"),
		Logout:           text("site.fragment.user.panel.logout", "退出登录"),
		Guest:            text("site.fragment.user.panel.guest", "你还没有登录。"),
		NoAccount:        text("site.fragment.user.panel.no_account", "没有账号？"),
		Register:         text("site.fragment.user.panel.register", "注册一个"),
	}
}

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

func userAccountProfileLabelsOf(text func(string, string) string) userAccountProfileLabels {
	return userAccountProfileLabels{
		userCommonLabels: userCommonLabelsOf(text),
		NeedLogin:        text("site.fragment.user.account.need_login_profile", "请先登录后再查看账号资料。"),
		Unavailable:      text("site.fragment.user.account.unavailable", "账号资料暂不可用，请稍后再试。"),
		Username:         text("site.fragment.user.account.username", "用户名"),
		Email:            text("site.fragment.user.account.email", "邮箱"),
		Verified:         text("site.fragment.user.account.verified", "已验证"),
		Unverified:       text("site.fragment.user.account.unverified", "未验证"),
		RegisteredAt:     text("site.fragment.user.account.registered_at", "注册时间"),
		LastLogin:        text("site.fragment.user.account.last_login", "上次登录"),
		Nickname:         text("site.fragment.user.account.nickname", "昵称"),
		Gender:           text("site.fragment.user.account.gender", "性别"),
		GenderUnset:      text("site.fragment.user.account.gender_unset", "未填写"),
		GenderMale:       text("site.fragment.user.account.gender_male", "男"),
		GenderFemale:     text("site.fragment.user.account.gender_female", "女"),
		FirstName:        text("site.fragment.user.account.first_name", "名"),
		LastName:         text("site.fragment.user.account.last_name", "姓"),
		Birthday:         text("site.fragment.user.account.birthday", "生日"),
		Phone:            text("site.fragment.user.account.phone", "电话"),
		Website:          text("site.fragment.user.account.website", "个人主页"),
		Company:          text("site.fragment.user.account.company", "公司 / 组织"),
		Country:          text("site.fragment.user.account.country", "国家 / 地区"),
		Province:         text("site.fragment.user.account.province", "省 / 州"),
		City:             text("site.fragment.user.account.city", "城市"),
		Zip:              text("site.fragment.user.account.zip", "邮编"),
		Address:          text("site.fragment.user.account.address", "地址"),
		Bio:              text("site.fragment.user.account.bio", "简介"),
		Save:             text("site.fragment.user.account.save_profile", "保存资料"),
	}
}

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

func userAccountPreferenceLabelsOf(text func(string, string) string) userAccountPreferenceLabels {
	return userAccountPreferenceLabels{
		userCommonLabels:  userCommonLabelsOf(text),
		NeedLogin:         text("site.fragment.user.account.need_login_preference", "请先登录后再查看账号偏好。"),
		Unavailable:       text("site.fragment.user.account.unavailable_preference", "账号偏好暂不可用，请稍后再试。"),
		PageSize:          text("site.fragment.user.account.page_size", "每页条数"),
		Visibility:        text("site.fragment.user.account.visibility", "主页可见性"),
		VisibilityPublic:  text("site.fragment.user.account.visibility_public", "所有人可见"),
		VisibilityMembers: text("site.fragment.user.account.visibility_members", "仅登录用户"),
		VisibilityPrivate: text("site.fragment.user.account.visibility_private", "仅自己"),
		Timezone:          text("site.fragment.user.account.timezone", "时区"),
		EmailNotify:       text("site.fragment.user.account.email_notify", "接收邮件通知"),
		SmsNotify:         text("site.fragment.user.account.sms_notify", "接收短信通知"),
		ShowOnline:        text("site.fragment.user.account.show_online", "公开我的在线状态"),
		Save:              text("site.fragment.user.account.save_preference", "保存偏好"),
	}
}

type userAccountPasswordLabels struct {
	userCommonLabels
	NeedLogin   string
	Current     string
	NewPassword string
	Hint        string
	Submit      string
}

func userAccountPasswordLabelsOf(text func(string, string) string) userAccountPasswordLabels {
	return userAccountPasswordLabels{
		userCommonLabels: userCommonLabelsOf(text),
		NeedLogin:        text("site.fragment.user.account.need_login_password", "请先登录后再修改密码。"),
		Current:          text("site.fragment.user.account.current_password", "当前密码"),
		NewPassword:      text("site.fragment.user.account.new_password", "新密码"),
		Hint:             text("site.fragment.user.account.password_hint", "至少 8 位。修改成功后所有设备都会退出登录。"),
		Submit:           text("site.fragment.user.account.change_password", "修改密码"),
	}
}

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

func userAccountSessionsLabelsOf(text func(string, string) string) userAccountSessionsLabels {
	return userAccountSessionsLabels{
		userCommonLabels: userCommonLabelsOf(text),
		Unavailable:      text("site.fragment.user.account.sessions_unavailable", "登录设备暂不可用，请稍后再试。"),
		NeedLogin:        text("site.fragment.user.account.need_login_sessions", "请先登录后再查看登录设备。"),
		Empty:            text("site.fragment.user.account.sessions_empty", "没有其它登录设备。"),
		UnknownBrowser:   text("site.fragment.user.account.unknown_browser", "未知浏览器"),
		CurrentDevice:    text("site.fragment.user.account.current_device", "当前设备"),
		IPUnknown:        text("site.fragment.user.account.ip_unknown", "IP 未知"),
		LastActive:       text("site.fragment.user.account.last_active", "最近活跃"),
		LoggedInAt:       text("site.fragment.user.account.logged_in_at", "登录于"),
		Revoke:           text("site.fragment.user.account.revoke", "踢出"),
		RevokeOthers:     text("site.fragment.user.account.revoke_others", "退出其它所有设备"),
	}
}

type Labels struct {
	Login      userLoginLabels
	Register   userRegisterLabels
	Forgot     userForgotLabels
	Reset      userResetLabels
	Account    userAccountPanelLabels
	Profile    userAccountProfileLabels
	Preference userAccountPreferenceLabels
	Password   userAccountPasswordLabels
	Sessions   userAccountSessionsLabels
}

func labelsOf(text func(string, string) string) Labels {
	return Labels{Login: userLoginLabelsOf(text), Register: userRegisterLabelsOf(text), Forgot: userForgotLabelsOf(text), Reset: userResetLabelsOf(text), Account: userAccountPanelLabelsOf(text), Profile: userAccountProfileLabelsOf(text), Preference: userAccountPreferenceLabelsOf(text), Password: userAccountPasswordLabelsOf(text), Sessions: userAccountSessionsLabelsOf(text)}
}
