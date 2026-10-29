package productenums

// 商品仍被后端事实声明或跨模块装配引用的词条 key。
// 纯界面文案留在模板；响应文案由统一出口取词。
const (
	ProductsActionDone                      = "admin.products.actionDone"
	ProductTranslationsEntityTypeProduct    = "admin.product_translations.entityType.product"
	ProductTranslationsSavedNote            = "admin.product_translations.savedNote"
	ProductTranslationsSavedStaleMarkFailed = "admin.product_translations.savedStaleMarkFailed"
	ProductTranslationsSavedNone            = "admin.product_translations.savedNone"
	ProductTranslationsErrorsTitle          = "admin.product_translations.errorsTitle"
	ProductTranslationsErrContextInvalid    = "admin.product_translations.err.contextInvalid"
	ProductTranslationsErrSourceSkipped     = "admin.product_translations.err.sourceSkipped"
	ProductTranslationsErrTargetEmpty       = "admin.product_translations.err.targetEmpty"
	ProductTranslationsErrShapeMismatch     = "admin.product_translations.err.shapeMismatch"
)
