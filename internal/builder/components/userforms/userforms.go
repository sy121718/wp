// Package userforms 在构建期生成账号表单，访客状态通过 JSON 数据接口读取。
package userforms

import (
	_ "embed" // user_forms_widget.jet / userforms.css 经 //go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.userForms"

// 形态取值。
//
// 前五个是**匿名**形态（登录前与登录后都能看），后四个是**账号中心**形态：
// 它们读取访客本人的资料、偏好与设备；普通页面未登录时显示同组件的登录入口。
const (
	ModeLogin    = "login"
	ModeRegister = "register"
	ModeForgot   = "forgot"
	ModeReset    = "reset"
	ModeAccount  = "account"

	ModeProfile    = "profile"
	ModePreference = "preference"
	ModePassword   = "password"
	ModeSessions   = "sessions"
)

type formSpec struct {
	title    string
	titleKey string
}

var formSpecs = map[string]formSpec{
	ModeLogin:      {title: "登录", titleKey: TextKeyTitleLogin},
	ModeRegister:   {title: "注册", titleKey: TextKeyTitleRegister},
	ModeForgot:     {title: "找回密码", titleKey: TextKeyTitleForgot},
	ModeReset:      {title: "重置密码", titleKey: TextKeyTitleReset},
	ModeAccount:    {title: "我的账号", titleKey: TextKeyTitleAccount},
	ModeProfile:    {title: "账号资料", titleKey: TextKeyTitleProfile},
	ModePreference: {title: "账号偏好", titleKey: TextKeyTitlePreference},
	ModePassword:   {title: "修改密码", titleKey: TextKeyTitlePassword},
	ModeSessions:   {title: "登录设备", titleKey: TextKeyTitleSessions},
}

// Props 访客账号表单属性。
type Props struct {
	// Mode 账号组件初始显示的表单或面板。
	Mode string `json:"mode,omitempty"`
	// Title 区块标题（留空用该形态的默认标题）。
	Title       string `json:"title,omitempty" ct:"text,maxlen=30,sec=content,label=标题"`
	Description string `json:"description,omitempty" ct:"textarea,maxlen=240,sec=content,label=说明"`
	// ShowTitle 是否显示标题。
	ShowTitle bool `json:"showTitle,omitempty" ct:"bool,sec=content,label=显示标题"`
	// Next 登录成功后的站内路径，留空则在组件中显示账号面板。
	Next string `json:"next,omitempty"`
	// Color 主色（提交按钮）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=主色"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct{ core.Atom[Props] }

// 内部表单切换由账号组件管理，只提供一个可选的登录后目标。
func (*Component) Actions() []core.ActionSpec {
	return []core.ActionSpec{{Key: "authenticated", AllowedKinds: []core.ActionKind{core.ActionPage, core.ActionPresentation, core.ActionLocal, core.ActionInstance}, TargetCapability: "user.account"}}
}
func (*Component) RequiredActions() []string { return nil }
func (*Component) Parts() []core.PartSpec {
	parts := make([]core.PartSpec, 0, 9)
	for _, mode := range []string{ModeLogin, ModeRegister, ModeForgot, ModeReset, ModeAccount, ModeProfile, ModePreference, ModePassword, ModeSessions} {
		parts = append(parts, core.PartSpec{Key: mode, Capabilities: []string{"user." + mode}})
	}
	return parts
}

// Widget 基座实例。
var Widget = &Component{Atom: core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "账号中心",
		Hint:            "登录注册、找回密码、资料偏好与设备管理，自动衔接完整流程",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"mode":      "account",
			"title":     "",
			"showTitle": true,
			"next":      "",
		},
		TypeName: Type,
		// 标题是作者填的文案，参与内容翻译。
		Translatable: []string{"title", "description"},
	},
}}

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
	return "var(--sky-c-primary, #171717)"
}

//go:embed userforms.css
var userFormsCSS string

// compileCSS 生成静态表单的组件样式。
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

// titleKeyOf 形态 → 默认标题 key 与中文兜底（空 key 表示该形态没有默认标题）。
func titleKeyOf(mode string) (key, fallback string) {
	spec, ok := formSpecs[mode]
	if !ok {
		return "", ""
	}
	return spec.titleKey, spec.title
}
