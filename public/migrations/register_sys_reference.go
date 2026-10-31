package migrations

import "sync"

func registerSysReference() {
	registerSysReferenceOnce.Do(registerSysReferenceSQL)
}

var registerSysReferenceOnce sync.Once

func registerSysReferenceSQL() {
	register(Migration{
		Version: "484-sys-config",
		SQL:     mustSQL("484_sys_config.sql"),
	})
	register(Migration{
		Version: "485-sys-area",
		SQL:     mustSQL("485_sys_area.sql"),
	})
	register(Migration{
		Version: "486-sys-dict",
		SQL:     mustSQL("486_sys_dict.sql"),
	})

	registerSeed(Seed{
		Version:   "487-seed-sys-area",
		TableName: "sys_area",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT count(*) FROM sys_area WHERE source = 'un-m49') >= 28 AND " +
			"(SELECT count(*) FROM sys_area WHERE source = 'iso3166') >= 247 AND " +
			"(SELECT count(*) FROM sys_area WHERE source = 'modood-cn') >= 367 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("487_seed_sys_area.sql"),
	})

	registerSeed(Seed{
		Version:   "488-seed-sys-dict",
		TableName: "sys_dict",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT count(*) FROM sys_dict WHERE type = 'language') >= 100 AND " +
			"(SELECT count(*) FROM sys_dict WHERE type = 'currency') >= 178 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("488_seed_sys_dict.sql"),
	})

	registerSeed(Seed{
		Version:   "489-seed-sys-config",
		TableName: "sys_config",
		ConditionSQL: "SELECT CASE WHEN (" +
			"SELECT count(*) FROM sys_config WHERE group_key = 'i18n' " +
			"AND jsonb_exists(config_data, 'default_lang') " +
			"AND jsonb_exists(config_data, 'site_lang_url_mode') " +
			") >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("489_seed_sys_config.sql"),
	})

	registerSeed(Seed{
		Version:   "490-seed-sys-config-lang-url-codes",
		TableName: "sys_config",
		ConditionSQL: "SELECT CASE WHEN (" +
			"SELECT count(*) FROM sys_config WHERE group_key = 'i18n' " +
			"AND jsonb_exists(config_data, 'lang_url_codes') " +
			") >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("490_sys_config_lang_url_codes.sql"),
	})
}
