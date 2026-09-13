package runtimefragment

// user_account_forms.go — 账号中心四张表单的片段（资料 / 偏好 / 改密码 / 登录设备）。
//
// 与 user_forms.go 的五个形态（登录 / 注册 / 找回 / 重置 / 面板）是同一族的延续，
// 拆成两个文件只有一个原因：这四个形态**要读账号事实**（收窄端口 VisitorAccountPort），
// 而前五个是纯渲染（不查任何东西）。
//
// 三个必须走片段的理由：
//   · 表单要带**访客域的 CSRF token**，而 token 在签名会话 cookie 里 ——
//     静态产物对所有人是同一份字节，烘不进一个随会话变化的值；
//   · 账号资料 / 偏好 / 登录设备是**每个访客各自的数据**，静态产物无从承载；
//   · 未登录与已登录是两种形态，静态页面给不了。
//
// 提交目标仍是 user 模块**已有的 POST 路由**（/user/account/*）：
// 那些路由带着完整链路（会话、CSRF、失败计数、撤销全部会话），片段只负责渲染。
// 无 JS 时表单原生提交，整页跳到同一批路由 —— 链路完全一样。

import (
	"context"
	"strings"

	pageenums "go_wp/internal/module/page/enums"
	usercontract "go_wp/internal/module/user/contract"
	userdto "go_wp/internal/module/user/dto"
	"go_wp/internal/templates"
)

// visitorAccountPort 访客账号事实的读取端口（装配期注入一次）。
//
// 未注入时渲染「账号功能暂不可用」的可见文案，而不是 500：
// 与 sitePageResolver / variantAvailability 同一口径 —— 缺失的能力降级成一句人话。
var visitorAccountPort usercontract.VisitorAccountPort

// SetVisitorAccountPort 注入访客账号读取端口（装配期调用）。
func SetVisitorAccountPort(p usercontract.VisitorAccountPort) { visitorAccountPort = p }

// 账号中心四个片段能力名。
const (
	accountFragmentProfile    = "accountProfileForm"
	accountFragmentPreference = "accountPreferenceForm"
	accountFragmentPassword   = "accountPasswordForm"
	accountFragmentSessions   = "accountSessionsPanel"
)

// accountFormSpec 片段能力 → 模板与取数需求。
//
// 三样东西放一起（能力名 / 模板名 / 要不要查库）：加一个形态时它们必须同时给出，
// 分开写在两处一定会有人漏改其中一个，而漏改的表现是「模板找不到」这种运行时才知道的错误。
type accountFormSpec struct {
	template     string
	needAccount  bool
	needSessions bool
}

var accountFormSpecs = map[string]accountFormSpec{
	accountFragmentProfile:    {template: "user_account_profile", needAccount: true},
	accountFragmentPreference: {template: "user_account_preference", needAccount: true},
	accountFragmentPassword:   {template: "user_account_password"},
	accountFragmentSessions:   {template: "user_account_sessions", needSessions: true},
}

func init() {
	// 四个都是 GET anonymous：
	//   · GET —— 只读渲染，没有副作用；
	//   · anonymous —— **不是**因为「谁都能看」，而是因为未登录必须得到一句引导。
	//     AuthSession 会直接回 401，而 HTMX 默认不替换 401 响应的目标节点，
	//     访客会看到一个毫无变化的面板，完全不知道发生了什么（同 ordersList 的取舍）。
	//     真正的门槛在提交侧：那批 POST 路由挂着 requireUser()，未登录根本提交不了。
	Register(Spec{Type: accountFragmentProfile, Method: "GET", Auth: AuthAnonymous, Render: renderAccountForm(accountFragmentProfile)})
	Register(Spec{Type: accountFragmentPreference, Method: "GET", Auth: AuthAnonymous, Render: renderAccountForm(accountFragmentPreference)})
	Register(Spec{Type: accountFragmentPassword, Method: "GET", Auth: AuthAnonymous, Render: renderAccountForm(accountFragmentPassword)})
	Register(Spec{Type: accountFragmentSessions, Method: "GET", Auth: AuthAnonymous, Render: renderAccountForm(accountFragmentSessions)})
}

// userAccountFormData 账号中心四张表单共用的模板数据。
//
// 共用一个结构而不是四份：它们的数据高度重叠（token、槽位互链、三种形态），
// 拆开只会让「新增一个互链」变成要改四处。
type userAccountFormData struct {
	// Fragment 能力名（模板写进 data 属性，便于作者按形态覆写样式）。
	Fragment string
	// CSRFToken 访客域的 CSRF token（为空时表单提交必然 403，模板必须能看出来）。
	CSRFToken string
	// NeedLogin 未登录：渲染引导 + 登录链接，而不是一个提交必然失败的空白表单。
	NeedLogin bool
	// Unavailable 账号读取端口未接入（装配缺失）：渲染可见提示，不渲染空表单 ——
	// 一个所有字段都是空的「资料表单」会让访客以为自己的资料被清空了。
	Unavailable bool

	// Next 保存成功后的站内回跳目标（作者在 hx-get 里配的）。
	//
	// 片段把它渲染成一个隐藏域，真正的校验在 user 模块的 handler 里 ——
	// 回跳目标最终要写进响应头，那一步不接受任何未经验证的自报值。
	// 改密码**不带**它：那一步刻意把人送去登录页（全部会话都被撤销了）。
	Next string

	// 系统页面槽位互链（没配就不输出链接 —— 不猜路径）。
	LoginURL   string
	AccountURL string

	// Account 账号概览与资料 / 偏好（NeedLogin / Unavailable 时为空）。
	Account *userdto.AccountResp
	// Sessions 登录设备台账（仅登录设备片段使用）。
	Sessions []*userdto.SessionItem
}

// renderAccountForm 生成某个形态的渲染函数。
func renderAccountForm(fragment string) func(ctx context.Context, r *Request) (string, error) {
	spec, ok := accountFormSpecs[fragment]
	if !ok {
		// 只可能由本文件的常量调用；写错就是一个永远渲染不出来的能力，
		// 在装配期炸掉比在访客面前返回空片段好。
		panic("账号中心片段未登记: " + fragment)
	}
	return func(ctx context.Context, r *Request) (string, error) {
		// 槽位只解析一次（请求内本身也有缓存，但让「同一次渲染用同一份槽位表」在代码里可见）。
		slots := cartSitePages(r, paramOf(r, "projectId"))
		data := userAccountFormData{
			Fragment:   fragment,
			CSRFToken:  strings.TrimSpace(r.CSRFToken),
			Next:       paramOf(r, "next"),
			LoginURL:   slots[pageenums.SiteSlotLogin],
			AccountURL: accountPageURL(slots),
		}
		userID, ok := visitorIDOf(r)
		if !ok {
			// 未登录：没有任何可渲染的数据，也不必查库。
			data.NeedLogin = true
			return templates.RenderFragment(spec.template, data)
		}
		if visitorAccountPort == nil {
			data.Unavailable = true
			return templates.RenderFragment(spec.template, data)
		}
		if spec.needAccount {
			acc, err := visitorAccountPort.AccountOf(ctx, userID)
			if err != nil {
				// 读账号失败是**服务端故障**，不降级成「你的账号没有资料」——
				// 后者会让故障表现成数据问题，排查方向直接跑偏。
				return "", err
			}
			if acc == nil {
				data.Unavailable = true
				return templates.RenderFragment(spec.template, data)
			}
			data.Account = acc
		}
		if spec.needSessions {
			items, err := visitorAccountPort.SessionsOf(ctx, userID, strings.TrimSpace(r.VisitorToken))
			if err != nil {
				return "", err
			}
			data.Sessions = items
		}
		return templates.RenderFragment(spec.template, data)
	}
}

// accountPageURL 账号页地址：优先用槽位，槽位没配就回落到 user 模块的内置页面。
//
// 与 user_forms.go 的降级口径不同（那里刻意不用槽位当降级目标）：这四个片段
// **本来就在**账号页上，回落到内置页面 /user/account 是稳定契约，不会自指。
func accountPageURL(slots map[string]string) string {
	if u := slots[pageenums.SiteSlotAccount]; u != "" {
		return u
	}
	return "/user/account"
}
