package migrations

import "sync"

// registerFileCategoryI18n 登记文件分类与媒体列表游标这七条错误的中英文案（716）。
//
// 这七条 key 此前在 service 里是 errors.New("中文串")，既不进词库也不带状态码；
// 现在改成 enums 的业务错误声明，文案落在这里。前缀是 FileCategory 而不是
// Category：product 域已有一套 ErrCategory*（商品分类），词库的 (item_key, lang)
// 全局唯一，共用同名 key 就等于两个模块共享一句文案的所有权。
func registerFileCategoryI18n() {
	registerFileCategoryI18nOnce.Do(registerFileCategoryI18nSeed)
}

var registerFileCategoryI18nOnce sync.Once

func registerFileCategoryI18nSeed() {
	registerSeed(Seed{
		Version:   "716-file-category-i18n",
		TableName: "sys_i18n",
		// 门槛逐条枚举本批的十四个 (item_key, lang) 元组：七个 key 的两种语言全齐才算完成。
		// 用全库计数或 LIKE 前缀会让存量库永远满足，补词条的那条迁移就再也不会执行；
		// 只数其中几个 key 也不行 —— 缺的那个 key 的页面会一直显示归口文案。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 14 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('ErrFileCategoryNameRequired', 'zh-CN'), ('ErrFileCategoryNameRequired', 'en-US'), " +
			"('ErrFileCategoryNotFound', 'zh-CN'), ('ErrFileCategoryNotFound', 'en-US'), " +
			"('ErrFileCategoryParentNotFound', 'zh-CN'), ('ErrFileCategoryParentNotFound', 'en-US'), " +
			"('ErrFileCategoryNameDuplicated', 'zh-CN'), ('ErrFileCategoryNameDuplicated', 'en-US'), " +
			"('ErrFileCategoryCycle', 'zh-CN'), ('ErrFileCategoryCycle', 'en-US'), " +
			"('ErrFileCategoryHasChildren', 'zh-CN'), ('ErrFileCategoryHasChildren', 'en-US'), " +
			"('ErrMediaCursorInvalid', 'zh-CN'), ('ErrMediaCursorInvalid', 'en-US'))",
		SQL: mustSQL("716_file_category_i18n.sql"),
	})
}
