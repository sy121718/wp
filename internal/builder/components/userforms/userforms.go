// Package userforms 实现 core.userForms 访客账号表单组件。
//
// 它解决的是「登录 / 注册 / 找回密码 / 账号面板这些表单，怎么出现在作者自己排的页面上」：
// 一个容器，进页面就拉对应形态的片段。
//
// 为什么表单必须由片段现拉，而不是构建期烘进产物：
//
//	· 表单要带**访客域的 CSRF token**，而 token 在签名会话 cookie 里 ——
//	  静态产物对所有人是同一份字节，烘不进一个随会话变化的值；
//	· 账号面板要看登录态，静态页面给不了两种形态。
//
// 降级：无 JS 时容器里保留一个指向**内置页面**的链接。
// 内置路由（/user/login 等）是稳定契约，且不会与槽位互相指向（槽位可能就指向当前这一页，
// 用槽位当降级目标会自指）。
package userforms

import (
	_ "embed" // user_forms_widget.jet / userforms.css 经 //go:embed 打进二进制
	"fmt"
	"net/url"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.userForms"

// 形态取值。
const (
	ModeLogin    = "login"
	ModeRegister = "register"
	ModeForgot   = "forgot"
	ModeReset    = "reset"
	ModeAccount  = "account"
)

// formSpec 形态 → 片段能力与内置页面。
//
// 两张表放一起：加一个形态时，「片段叫什么」与「无 JS 时去哪」必须同时给出，
// 分开写在两处一定会有人漏改其中一个。
type formSpec struct {
	fragment string
	page     string
	title    string
}

var formSpecs = map[string]formSpec{
	ModeLogin:    {fragment: "loginForm", page: "/user/login", title: "登录"},
	ModeRegister: {fragment: "registerForm", page: "/user/register", title: "注册"},
	ModeForgot:   {fragment: "forgotForm", page: "/user/forgot", title: "找回密码"},
	ModeReset:    {fragment: "resetForm", page: "/user/reset", title: "重置密码"},
	ModeAccount:  {fragment: "accountPanel", page: "/user/account", title: "我的账号"},
}

// Props 访客账号表单属性。
type Props struct {
	// Mode 形态（登录 / 注册 / 找回密码 / 重置密码 / 账号面板）。
	Mode string `json:"mode,omitempty" ct:"select,login=登录,register=注册,forgot=找回密码,reset=重置密码,account=账号面板,default=login,sec=content,label=表单形态"`
	// Title 区块标题（留空用该形态的默认标题）。
	Title string `json:"title,omitempty" ct:"text,maxlen=30,sec=content,label=标题"`
	// ShowTitle 是否显示标题。
	ShowTitle bool `json:"showTitle,omitempty" ct:"bool,sec=content,label=显示标题"`
	// Next 成功后的回跳路径（如 /account；留空则回到 user 模块的默认落点）。
	Next string `json:"next,omitempty" ct:"text,maxlen=200,sec=content,label=成功后跳转"`
	// Color 主色（提交按钮）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=主色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName: Type,
		// 标题是作者填的文案，参与内容翻译。
		Translatable: []string{"title"},
	},
}

// effectiveMode 有效形态（未知值兜底登录 —— 登录表单是这组里最常见的入口）。
func effectiveMode(p *Props) string {
	if spec, ok := formSpecs[strings.TrimSpace(p.Mode)]; ok {
		_ = spec
		return strings.TrimSpace(p.Mode)
	}
	return ModeLogin
}

// effectiveTitle 有效标题。
func effectiveTitle(p *Props) string {
	if t := strings.TrimSpace(p.Title); t != "" {
		return t
	}
	return formSpecs[effectiveMode(p)].title
}

// effectiveColor 有效主色。
func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return "var(--sky-c-primary, #2563eb)"
}

//go:embed userforms.css
var userFormsCSS string

// compileCSS 组件样式（含片段内容的基础样式 —— 片段 HTML 运行时才渲染，页面作者看不到）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"color": effectiveColor(p)}
	if err := core.ApplyComponentCSSTmpl(b, sel, userFormsCSS, vars); err != nil {
		panic(fmt.Sprintf("userForms 组件样式解析失败: %v", err))
	}
}

//go:embed user_forms_widget.jet
var userFormsTemplate string

// init 注册访客账号表单组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("user_forms_widget", userFormsTemplate)
}

// buildFragmentURL 片段地址（工程 + 形态 + 回跳 + 语言）。
func buildFragmentURL(mode, projectID, lang, next string) (fragmentURL, fallbackURL string) {
	spec := formSpecs[mode]
	q := url.Values{}
	q.Set("projectId", strings.TrimSpace(projectID))
	if n := strings.TrimSpace(next); n != "" {
		q.Set("next", n)
	}
	if l := strings.TrimSpace(lang); l != "" {
		q.Set("lang", l)
	}
	return "/_fragments/" + spec.fragment + "?" + q.Encode(), spec.page
}
