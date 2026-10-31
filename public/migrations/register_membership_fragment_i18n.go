package migrations

func init() {
	registerSeed(Seed{
		Version:   "464-membership-consume-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 40 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'site.fragment.membership.title', 'site.fragment.membership.guest', " +
			"'site.fragment.membership.unavailable', 'site.fragment.membership.project_missing', " +
			"'site.fragment.membership.failed', 'site.fragment.membership.default_hint', " +
			"'site.fragment.membership.free_shipping', 'site.fragment.membership.discount', " +
			"'site.fragment.membership.none', " +
			"'admin.customer_detail.membership.heading', 'admin.customer_detail.membership.hint', " +
			"'admin.customer_detail.membership.tier', 'admin.customer_detail.membership.default_tier', " +
			"'admin.customer_detail.membership.benefits', 'admin.customer_detail.membership.free_shipping', " +
			"'admin.customer_detail.membership.discount', 'admin.customer_detail.membership.no_benefit', " +
			"'admin.customer_detail.membership.unavailable', 'admin.customer_detail.membership.no_project', " +
			"'admin.customer_detail.membership.failed'" +
			")",
		SQL: mustSQL("464_membership_consume_i18n.sql"),
	})
}
