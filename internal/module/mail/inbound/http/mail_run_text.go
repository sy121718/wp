package mailhttp

// mail_run_text.go — 运行文案的**出口**取词（页面与 JSON API 共用的两个助手）。
//
// 为什么在出口做：写侧（service）落库的是「key + 参数」编码（见 mail/enums/mail_run_text.go），
// 只有到 handler 这一层才知道请求语言。页面出口用 shell.TranslateFor(c)，
// JSON API 出口用 pkg/i18n.TranslateFunc(response.RequestLanguage(c))。
//
// 为什么两个出口都要做：**不做的那一边会退步** —— 页面翻了、API 原样返回编码串，
// 消费方看到的是 `mail.run.explain.waiting\x1f2026-01-02 15:04\x1fn1`。
//
// 旧数据不需要特殊分支：FormatRunText 对中文原文原样返回（判定不通过），
// 所以历史行在排障页上显示的还是当年那句话。

import (
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
)

// mailRunTexts 实例列表里的运行文案（原地改写）。
func mailRunTexts(tr mailTr, items []maildto.AutomationRunItem) {
	for i := range items {
		items[i].ErrorMessage = mailenums.FormatRunText(tr, items[i].ErrorMessage)
	}
}

// mailRunDetailTexts 排障详情：Explain / error_message / 时间线 detail（原地改写）。
func mailRunDetailTexts(tr mailTr, d *maildto.AutomationRunDetailResp) {
	if d == nil {
		return
	}
	d.Explain = mailenums.FormatRunText(tr, d.Explain)
	d.Run.ErrorMessage = mailenums.FormatRunText(tr, d.Run.ErrorMessage)
	for i := range d.Timeline {
		d.Timeline[i].Detail = mailenums.FormatRunText(tr, d.Timeline[i].Detail)
	}
}
