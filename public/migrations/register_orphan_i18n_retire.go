package migrations

import "sync"

func registerOrphanI18nRetire() {
	registerOrphanI18nRetireOnce.Do(registerOrphanI18nRetireSeed)
}

var registerOrphanI18nRetireOnce sync.Once

func registerOrphanI18nRetireSeed() {

	registerSeed(Seed{
		Version:   "419-i18n-retire-orphans",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 0 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN (" +
			"'admin.product_categories.seo.unset','admin.product_brands.seo.unset'," +
			"'admin.inventory.pagination.info','admin.inventory.pagination.more','admin.inventory.pagination.end'," +
			"'admin.mail.campaign.page_prefix','admin.mail.campaign.page_suffix')",
		SQL: mustSQL("419_retire_orphan_i18n.sql"),
	})
}

func init() { registerOrphanI18nRetire() }
