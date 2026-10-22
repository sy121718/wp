package productenums

// product_translation_fallbacks.go — 译文工作台提示词条的 key 与「词条缺失时的中文原文」。
//
// 为什么住 enums 而不是控制器：key 的归属在本目录（见 internal/module/CLAUDE.md §enums），
// 兜底原文与 key 是一对，分两处住时改一句话要动控制器，而「加一门语言」这件事就变成了
// 控制器里的活。page 模块的 pageenums.ErrDetailFallbacks 是同一形态的样板。
//
// 性质说明：这张表**不是可透出性判据**（那是 pkg/response.IsBusinessError 与各模块
// 的归口文案），只是「i18n 未初始化 / 该 key 没有词条」时的展示原文 —— 与 seed 里的
// zh-CN 词条保持一致，避免「同一句话在测试与线上不一样」。

// 译文工作台提示的遗留命名 key（`MsgXxx` 形态，早期约定；新代码用点分 key）。
const (
	MsgTranslationSaveFailed  = "MsgTranslationSaveFailed"
	MsgTranslationInvalid     = "MsgTranslationInvalid"
	MsgTranslationStale       = "MsgTranslationStale"
	MsgTranslationLangInvalid = "MsgTranslationLangInvalid"
)

// TranslationMsgFallbacks 商品翻译页提示的中文兜底（dashboard enums 常量是
// sys_i18n 的 key，缺词条时用这里的原文；只列本页真正会产生的那几条）。
var TranslationMsgFallbacks = map[string]string{
	MsgTranslationSaveFailed:  "译文保存失败，请稍后重试",
	MsgTranslationInvalid:     "提交数据不完整，请刷新页面后重试",
	MsgTranslationStale:       "原文已变更，请刷新页面后重新翻译",
	MsgTranslationLangInvalid: "目标语言未启用，请先在站点设置里启用",
	// 行级校验结论（validateProductTarget 的返回值，见 product_translations_handle.go 的 validateProductTarget）：
	// 它们是页面上最常出现的几句话，同样必须能按语言取词。
	ProductTranslationsErrContextInvalid: "语境非法：不是商品域的可翻译字段",
	ProductTranslationsErrSourceSkipped:  "原文不参与翻译（空串、纯数字或纯符号）",
	ProductTranslationsErrTargetEmpty:    "译文不能为空",
	ProductTranslationsErrShapeMismatch:  "译文形态与原文不一致：原文含 HTML 标签时译文也必须含标签",
}
