package migrations

// Explicit pairs keep a missing language eligible for backfill.
func init() {
	registerSeed(Seed{
		Version:      "606-membership-write-ui-i18n",
		TableName:    "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key IN ('admin.membership.write.noJs') AND lang IN ('zh-CN', 'en-US')",
		SQL:          mustSQL("606_membership_write_ui_i18n.sql"),
	})
}
