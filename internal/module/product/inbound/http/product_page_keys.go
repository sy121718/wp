package producthttp

// product_page_keys.go — 商品域后台页面的 i18n key 常量。
//
// i18n key 是字符串协议：这些 key 原登记在 dashboard/enums，页面回迁后随域归入本模块，
// 字面值与词条表（迁移 163 起）保持一致 —— 改 key 必须同步 sys_i18n 词条。
const (
	MsgProductsTitle          = "MsgProductsTitle"
	MsgTranslationSaveFailed  = "MsgTranslationSaveFailed"
	MsgTranslationInvalid     = "MsgTranslationInvalid"
	MsgTranslationStale       = "MsgTranslationStale"
	MsgTranslationLangInvalid = "MsgTranslationLangInvalid"
)
