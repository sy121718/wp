package runtimefragment

// user_forms.go — 访客账号表单片段（issue #36 / BIZ-1）。
//
// 为什么这些表单必须走片段，而不是烘进静态产物：
//   · 表单要带**访客域的 CSRF token**，而 token 存在签名会话 cookie 里 ——
//     静态产物对所有人是同一份字节，烘不进一个随会话变化的值；
//   · 「已登录 / 未登录」是两种形态（账号面板），静态页面给不了。
//
// 提交目标仍是 user 模块**已有的 POST 路由**（/user/login 等）：
// 那些路由带着完整链路（会话、登录成功轮换 CSRF、失败计数与锁定），
// 片段只负责把表单渲染出来。
//
// 这样安排的好处落在降级路径上：无 JS 时表单照样原生提交，整页跳到 user 模块的页面；
// 有 JS 时它在页面作者的任意页面上出现（配合 core.userForms 组件）。

import (
	"context"
	"strings"

	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/templates"
)

// userFormMode 表单形态（同时是模板名后缀与 data-fragment 值）。
type userFormMode string

const (
	userFormLogin    userFormMode = "login"
	userFormRegister userFormMode = "register"
	userFormForgot   userFormMode = "forgot"
	userFormReset    userFormMode = "reset"
	userFormAccount  userFormMode = "account"
)

func init() {
	Register(Spec{Type: "loginForm", Method: "GET", Auth: AuthAnonymous, Render: renderUserForm(userFormLogin)})
	Register(Spec{Type: "registerForm", Method: "GET", Auth: AuthAnonymous, Render: renderUserForm(userFormRegister)})
	Register(Spec{Type: "forgotForm", Method: "GET", Auth: AuthAnonymous, Render: renderUserForm(userFormForgot)})
	Register(Spec{Type: "resetForm", Method: "GET", Auth: AuthAnonymous, Render: renderUserForm(userFormReset)})
	Register(Spec{Type: "accountPanel", Method: "GET", Auth: AuthAnonymous, Render: renderUserForm(userFormAccount)})
}

// userFormData 五个表单共用的模板数据。
//
// 共用一个结构而不是五份：它们的数据几乎是同一批（token、回跳、互链），
// 拆开只会让「新增一个互链」变成要改五处。
type userFormData struct {
	Mode string
	// CSRFToken 访客域的 CSRF token（缺失时表单提交必然 403，所以模板要能看出它为空）。
	CSRFToken string
	// Next 登录 / 注册成功后的回跳目标（来自页面作者配的 hx-get 参数）。
	Next   string
	Notice string

	// 系统页面槽位互链（没配就不输出对应链接 —— 不猜路径）。
	LoginURL    string
	RegisterURL string
	ForgotURL   string
	AccountURL  string

	// 重置密码表单的预填（邮件链接里带过来）。
	Email string
	Key   string

	// 账号面板：是否已登录（未登录时给引导，不渲染「已登录才有的」东西）。
	LoggedIn bool
}

type userLoginFormView struct {
	userFormData
	Labels userLoginLabels
}

type userRegisterFormView struct {
	userFormData
	Labels userRegisterLabels
}

type userForgotFormView struct {
	userFormData
	Labels userForgotLabels
}

type userResetFormView struct {
	userFormData
	Labels userResetLabels
}

type userAccountPanelView struct {
	userFormData
	Labels userAccountPanelLabels
}

// renderUserForm 生成某个形态的渲染函数。
func renderUserForm(mode userFormMode) func(ctx context.Context, r *Request) (string, error) {
	return func(_ context.Context, r *Request) (string, error) {
		projectID := paramOf(r, "projectId")
		slots := cartSitePages(r, projectID)
		data := userFormData{
			Mode:        string(mode),
			CSRFToken:   strings.TrimSpace(r.CSRFToken),
			Next:        paramOf(r, "next"),
			LoginURL:    slots[pageenums.SiteSlotLogin],
			RegisterURL: slots[pageenums.SiteSlotRegister],
			ForgotURL:   slots[pageenums.SiteSlotForgot],
			AccountURL:  slots[pageenums.SiteSlotAccount],
			Email:       paramOf(r, "email"),
			Key:         paramOf(r, "key"),
		}
		if _, ok := visitorIDOf(r); ok {
			data.LoggedIn = true
		}
		switch mode {
		case userFormLogin:
			return templates.RenderFragment("user_login", userLoginFormView{userFormData: data, Labels: userLoginLabelsOf(r)})
		case userFormRegister:
			return templates.RenderFragment("user_register", userRegisterFormView{userFormData: data, Labels: userRegisterLabelsOf(r)})
		case userFormForgot:
			return templates.RenderFragment("user_forgot", userForgotFormView{userFormData: data, Labels: userForgotLabelsOf(r)})
		case userFormReset:
			return templates.RenderFragment("user_reset", userResetFormView{userFormData: data, Labels: userResetLabelsOf(r)})
		case userFormAccount:
			return templates.RenderFragment("user_account", userAccountPanelView{userFormData: data, Labels: userAccountPanelLabelsOf(r)})
		default:
			return "", nil
		}
	}
}
