package userforms

import (
	_ "embed"
	"fmt"
	"strings"

	"go_wp/pkg/builder/core"
)

const Type = "core.userForms"

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

type Props struct {
	Mode string `json:"mode,omitempty"`

	Title       string `json:"title,omitempty" ct:"text,maxlen=30,sec=content,label=标题"`
	Description string `json:"description,omitempty" ct:"textarea,maxlen=240,sec=content,label=说明"`

	ShowTitle bool `json:"showTitle,omitempty" ct:"bool,sec=content,label=显示标题"`

	Next string `json:"next,omitempty"`

	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=主色"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct{ core.Atom[Props] }

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

		Translatable: []string{"title", "description"},
	},
}}

func effectiveMode(p *Props) string {
	if spec, ok := formSpecs[strings.TrimSpace(p.Mode)]; ok {
		_ = spec
		return strings.TrimSpace(p.Mode)
	}
	return ModeLogin
}

func effectiveTitle(p *Props) string {
	if t := strings.TrimSpace(p.Title); t != "" {
		return t
	}
	return formSpecs[effectiveMode(p)].title
}

func effectiveColor(p *Props) string {
	if c := strings.TrimSpace(p.Color); c != "" {
		return c
	}
	return "var(--sky-c-primary, #171717)"
}

//go:embed userforms.css
var userFormsCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	vars := map[string]string{"color": effectiveColor(p)}
	if err := core.ApplyComponentCSSTmpl(b, sel, userFormsCSS, vars); err != nil {
		panic(fmt.Sprintf("userForms 组件样式解析失败: %v", err))
	}
}

//go:embed user_forms_widget.jet
var userFormsTemplate string

func init() {
	core.Register(Widget)
	core.RegisterTemplate("user_forms_widget", userFormsTemplate)
}

func titleKeyOf(mode string) (key, fallback string) {
	spec, ok := formSpecs[mode]
	if !ok {
		return "", ""
	}
	return spec.titleKey, spec.title
}
