package migrations

// register_membership_fragment_i18n.go — 迁移 464 的 seed 注册（BIZ-3 消费侧接入的文案词条）。
//
// 门槛判据**枚举本批自己的 20 个 item_key**（上界封闭，20 × 2 语言 = 40 行）：
// 用 LIKE 前缀会让「别批次已有同前缀行」把计数抬高、本批被静默跳过（058 的故障），
// 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的故障）。
// （`site.fragment.membership.*` 与 `admin.customer_detail.membership.*` 两个前缀下，
// 本批是唯一的写入者；一旦将来有人续写同前缀，重跑也会被 ON CONFLICT 挡住。）
//
// 注册方式：本文件自带 func init()（与 455 / 462a 一致），不在 register.go 的 init() 里再加一行
// —— 那会让「谁负责注册」出现两个真源。
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
