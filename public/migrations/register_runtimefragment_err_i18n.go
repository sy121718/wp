package migrations

import "sync"

func registerRuntimeFragmentErrI18n() {
	registerRuntimeFragmentErrI18nOnce.Do(registerRuntimeFragmentErrI18nSeed)
}

var registerRuntimeFragmentErrI18nOnce sync.Once

func registerRuntimeFragmentErrI18nSeed() {

	registerSeed(Seed{
		Version:   "409-runtimefragment-err-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(DISTINCT item_key) >= 5 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'site.fragment.err.form_parse', 'site.fragment.err.too_many_params', " +
			"'site.fragment.err.param_invalid', 'site.fragment.err.context', " +
			"'site.fragment.err.internal')",
		SQL: mustSQL("409_i18n_runtimefragment_err.sql"),
	})
}

func init() { registerRuntimeFragmentErrI18n() }
