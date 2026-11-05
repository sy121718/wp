package migrations

import "sync"

// registerMediaErrorCodesI18n 对齐 media 错误词条的状态码并补一条缺失词条（715）。
//
// 两件事同批：① 把 ErrUploadFailed / ErrDownloadFailed 的 http_code 从 400 校正成 500
// （状态码事实源在 mediaenums.StatusOf，词库是可观测副本，不能长期分叉）；
// ② 补 ErrReplaceStaleNotifyFailed 的中英词条 —— 它在换图成功后通知失败的两条路径上
// 会真的返回给调用方，零词条时页面只剩归口文案。
func registerMediaErrorCodesI18n() {
	registerMediaErrorCodesI18nOnce.Do(registerMediaErrorCodesI18nSeed)
}

var registerMediaErrorCodesI18nOnce sync.Once

func registerMediaErrorCodesI18nSeed() {
	registerSeed(Seed{
		Version:   "715-media-error-codes-i18n",
		TableName: "sys_i18n",
		// 门槛逐条枚举本批的六个 (item_key, lang) 元组，用 LIKE / 全库计数都会让
		// 存量库永远满足，这条迁移就再也不会执行：
		//   · 两个 key 各两种语言 → 四条行的 http_code 必须已是 500；
		//   · 新补的 key 两种语言 → 两行必须存在。
		// 只数条数不够：UPDATE 没命中（比如运营把值改过）时行仍然在，但状态没对齐。
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM sys_i18n WHERE item_key IN ('ErrUploadFailed', 'ErrDownloadFailed') " +
			"AND lang IN ('zh-CN', 'en-US') AND http_code = 500) = 4 " +
			"AND (SELECT COUNT(*) FROM sys_i18n WHERE item_key = 'ErrReplaceStaleNotifyFailed' " +
			"AND lang IN ('zh-CN', 'en-US')) = 2 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("715_media_error_codes_i18n.sql"),
	})
}
