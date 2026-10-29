package userforms

// i18n.go — 访客账号表单外壳的固定文案取词（审计 I18N-010）。

// 文案 key（sys_i18n）：site.component.userForms.<语义>。
const (
	TextKeyScriptHint = "site.component.userForms.noScript"
	TextKeyRetry      = "site.component.userForms.retry"
	// 九种形态的**默认标题**（作者没填 Title 时用它，填了就不动）。
	// 默认标题此前是 Go 侧的 formSpecs 常量：模板里看不到中文，契约测试也扫不到，
	// 属于「模板干净、Go 侧还有硬编码」的漏网 —— 英文站点上九个表单标题全是中文。
	TextKeyTitleLogin      = "site.component.userForms.title.login"
	TextKeyTitleRegister   = "site.component.userForms.title.register"
	TextKeyTitleForgot     = "site.component.userForms.title.forgot"
	TextKeyTitleReset      = "site.component.userForms.title.reset"
	TextKeyTitleAccount    = "site.component.userForms.title.account"
	TextKeyTitleProfile    = "site.component.userForms.title.profile"
	TextKeyTitlePreference = "site.component.userForms.title.preference"
	TextKeyTitlePassword   = "site.component.userForms.title.password"
	TextKeyTitleSessions   = "site.component.userForms.title.sessions"
)

const fallbackScriptHint = "请启用 JavaScript 以安全使用账号表单。"

// ApplyI18n 在构建时取词，运行时接口只返回数据事实。
func (v *View) ApplyI18n(text func(string, string) string) {
	if v == nil {
		return
	}
	if text == nil {
		text = func(_, fallback string) string { return fallback }
	}
	v.Labels = labelsOf(text)
	v.ScriptHint = text(TextKeyScriptHint, fallbackScriptHint)
	v.RetryText = text(TextKeyRetry, "重试")
	v.Titles = nil
	for _, mode := range []string{ModeLogin, ModeRegister, ModeForgot, ModeReset, ModeAccount, ModeProfile, ModePreference, ModePassword, ModeSessions} {
		spec := formSpecs[mode]
		v.TitleKeys[mode] = text(spec.titleKey, spec.title)
		v.Titles = append(v.Titles, ModeTitle{Mode: mode, Title: v.TitleKeys[mode]})
	}
	if !v.customTitle {
		v.Title = v.TitleKeys[v.Mode]
	}
}
