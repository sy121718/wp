package productenums

const (
	MsgTranslationSaveFailed  = "MsgTranslationSaveFailed"
	MsgTranslationInvalid     = "MsgTranslationInvalid"
	MsgTranslationStale       = "MsgTranslationStale"
	MsgTranslationLangInvalid = "MsgTranslationLangInvalid"
)

var TranslationMsgFallbacks = map[string]string{
	MsgTranslationSaveFailed:  "译文保存失败，请稍后重试",
	MsgTranslationInvalid:     "提交数据不完整，请刷新页面后重试",
	MsgTranslationStale:       "原文已变更，请刷新页面后重新翻译",
	MsgTranslationLangInvalid: "目标语言未启用，请先在站点设置里启用",

	ProductTranslationsErrContextInvalid: "语境非法：不是商品域的可翻译字段",
	ProductTranslationsErrSourceSkipped:  "原文不参与翻译（空串、纯数字或纯符号）",
	ProductTranslationsErrTargetEmpty:    "译文不能为空",
	ProductTranslationsErrShapeMismatch:  "译文形态与原文不一致：原文含 HTML 标签时译文也必须含标签",
}
