package migrations

import "sync"

func registerSourcesFilterTitleRetire() {
	registerSourcesFilterTitleRetireOnce.Do(registerSourcesFilterTitleRetireSeed)
}

var registerSourcesFilterTitleRetireOnce sync.Once

func registerSourcesFilterTitleRetireSeed() {

	registerSeed(Seed{
		Version:   "436-i18n-retire-sources-filter-title",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key = " +
			"'admin.inventory_sources.filter.title'",
		SQL: mustSQL("436_retire_sources_filter_title_i18n.sql"),
	})
}

func init() { registerSourcesFilterTitleRetire() }
