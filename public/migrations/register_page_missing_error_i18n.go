package migrations

import "sync"

// registerPageMissingErrorI18n 登记 page 域八条业务错误的中英文案（717）。
//
// 这八个 key 在 page_enums.go 里早已声明，但此前在迁移里零覆盖，出口查不到词条就
// 退回归口文案「系统内部错误，请稍后重试」—— 用户看不到「该语言已被本页排除，
// 无需重复操作」这类可行动提示。裸常量名当 key 是全仓既有约定（20+ 个域一致），
// 这里沿用同一形状。
func registerPageMissingErrorI18n() {
	registerPageMissingErrorI18nOnce.Do(registerPageMissingErrorI18nSeed)
}

var registerPageMissingErrorI18nOnce sync.Once

func registerPageMissingErrorI18nSeed() {
	registerSeed(Seed{
		Version:   "717-page-missing-error-i18n",
		TableName: "sys_i18n",
		// 门槛逐条枚举本批的十六个 (item_key, lang) 元组：八个 key 的两种语言全齐才算完成。
		// 用全库计数或 LIKE 前缀会让存量库永远满足，补词条的那条迁移就再也不会执行；
		// 只数其中几个也不行 —— 缺的那个 key 的页面会一直显示归口文案。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 16 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('ErrPageLangExcluded', 'zh-CN'), ('ErrPageLangExcluded', 'en-US'), " +
			"('ErrCannotExcludeDefaultLang', 'zh-CN'), ('ErrCannotExcludeDefaultLang', 'en-US'), " +
			"('ErrLangAlreadyExcluded', 'zh-CN'), ('ErrLangAlreadyExcluded', 'en-US'), " +
			"('ErrLangNotExcluded', 'zh-CN'), ('ErrLangNotExcluded', 'en-US'), " +
			"('ErrRevisionNotFound', 'zh-CN'), ('ErrRevisionNotFound', 'en-US'), " +
			"('ErrInputMoved', 'zh-CN'), ('ErrInputMoved', 'en-US'), " +
			"('ErrRebuildOverloaded', 'zh-CN'), ('ErrRebuildOverloaded', 'en-US'), " +
			"('ErrRedirectConflict', 'zh-CN'), ('ErrRedirectConflict', 'en-US'))",
		SQL: mustSQL("717_page_missing_error_i18n.sql"),
	})
}
