package maildto

// mail_option_resp.go — 邮件后台页的**下拉选项**形状。
//
// 邮件的自动化画布有 9 处下拉（步骤类型、条件、目标、账号、模板…），形状完全相同：
// Value + 已取好词的 Label + Selected。合成一个类型比 9 份 gin.H 更可靠 ——
// 同一形状在手写 map 里是「每处都可能拼错一个键」，而拼错的表现是选项渲染成空白。
//
// 字段名 = 模板键（mail_automation_*.html 读 `.Value` / `.Label` / `.Selected`）。
type OptionResp struct {
	Value    string
	Label    string
	Selected bool
}
