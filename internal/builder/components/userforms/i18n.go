package userforms

import "fmt"

// i18n.go — 访客账号表单外壳的固定文案取词（审计 I18N-010）。

// 文案 key（sys_i18n）：site.component.userForms.<语义>。
const (
	TextKeyScriptHint   = "site.component.userForms.scriptHint"
	TextKeyOpenFallback = "site.component.userForms.openFallback"
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

// 中文兜底。两句都含 %s：
//   - scriptHint  的 %s 是表单名（登录 / 注册 / 找回密码…）
//   - openFallback 的 %s 同上
//
// 用占位符而不是拼接，是因为「打开 X 页」在别的语言里未必是「打开」在前 ——
// 整句交给译文，位置由译者决定。占位符只允许 %s（pkg/i18n 约定）。
const (
	fallbackScriptHint   = "%s表单需要脚本加载；也可以"
	fallbackOpenFallback = "打开%s页"
)

// ApplyI18n 按当前语言回填固定文案（实现 core.I18nAware）。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	hint := fallbackScriptHint
	open := fallbackOpenFallback
	if text != nil {
		hint = text(TextKeyScriptHint, fallbackScriptHint)
		open = text(TextKeyOpenFallback, fallbackOpenFallback)
	}
	// 默认标题跟随语言；作者显式填过的**不动**（与 addToCart 的「用户配置优先」同一规则）。
	// 判据是「当前值恰好等于中文默认」——BuildView 已经把默认值写进 Title/FallbackText，
	// 这里不做区分的话，作者自己写的标题会在构建时被译文覆盖掉。
	if key, zh := titleKeyOf(v.Mode); key != "" {
		translated := zh
		if text != nil {
			translated = text(key, zh)
		}
		if v.Title == zh {
			v.Title = translated
		}
		if v.FallbackText == zh {
			v.FallbackText = translated
		}
	}
	v.ScriptHint = fmt.Sprintf(hint, v.FallbackText)
	v.OpenFallbackText = fmt.Sprintf(open, v.FallbackText)
}
