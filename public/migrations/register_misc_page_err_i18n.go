package migrations

import "sync"

func registerMiscPageErrI18n() {
	registerMiscPageErrI18nOnce.Do(registerMiscPageErrI18nSeed)
}

var registerMiscPageErrI18nOnce sync.Once

func registerMiscPageErrI18nSeed() {

	registerSeed(Seed{
		Version:   "408-i18n-misc-page-err",
		TableName: "sys_i18n",
		// 门槛：本批 7 条 en-US 词条齐全才算种过。analytics 的「装载失败空态」词条
		// 已随页面失败态一起清退（页面失败改走 JSON 早退），故从本批门槛里去掉，
		// 条数 8 → 7。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 7 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.masterdata.loadFailed.lead','admin.masterdata.loadFailed.title'," +
			"'admin.masterdata.loadFailed.desc'," +
			"'admin.content.templates.loadFailed.title','admin.content.templates.loadFailed'," +
			"'admin.content.templates.impact.loadFailed','ErrAnalyticsInternal')",
		SQL: mustSQL("408_i18n_misc_page_err.sql"),
	})
}

func init() { registerMiscPageErrI18n() }
