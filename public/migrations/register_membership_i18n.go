package migrations

// 462a — 会员等级与权益（BIZ-3）的词条：业务错误 / 成功回执 / 后台页面与菜单文案。
//
// 与 462（三张表 + orders 的会员折扣列）**同批**：表负责存、词条负责说，
// 两半一起落库才不会出现「表已上、页面上显示裸 key」的中间态（编号后缀是仓库既有的
// 「同批第二半」写法，见 register.go 的 compareVersion 注释）。
//
// 门槛判据**逐条枚举本批自己的 87 个 item_key**（上界封闭，87 × 2 = 174 行）：
//   - 不用 LIKE 前缀：别的批次已有同前缀行时计数虚高 → 本批被静默跳过（058 的真实故障）；
//   - 不用全库总量：将来新增同前缀 key 时永远追不平 → 每次启动重跑（076 的真实故障）。
//
// 放在**种子**台账（registerSeed 而不是 register）：本批只新增词条、不改任何既有 key，
// 词条属于 seed 语义（可重复写入的默认值）。
//
// 注册方式：本文件自带 init()（与 460a / 459 的既有写法同形），不在 register.go 的 init()
// 里再加一行 —— 那会让「谁负责注册」出现两个真源。
func init() {
	registerSeed(Seed{
		Version:   "462a-membership-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 174 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			"'admin.membership.assign.autoHint', 'admin.membership.assign.col.assignedAt', 'admin.membership.assign.col.source', " +
			"'admin.membership.assign.col.tier', 'admin.membership.assign.col.user', 'admin.membership.assign.empty', " +
			"'admin.membership.assign.emptyHeading', 'admin.membership.assign.field.tier', 'admin.membership.assign.field.user', " +
			"'admin.membership.assign.field.userHint', 'admin.membership.assign.field.userPh', 'admin.membership.assign.filter.user', " +
			"'admin.membership.assign.intro', 'admin.membership.assign.recalcUnavailable', 'admin.membership.assign.set', " +
			"'admin.membership.assign.setTitle', 'admin.membership.assign.source.auto', 'admin.membership.assign.source.manual', " +
			"'admin.membership.assign.title', 'admin.membership.assign.totalLead', 'admin.membership.assign.totalTail', " +
			"'admin.membership.assign.unlock', 'admin.membership.entitlement.freeShipping', 'admin.membership.entitlement.freeShippingOn', " +
			"'admin.membership.entitlement.hint', 'admin.membership.entitlement.kind.discount', 'admin.membership.entitlement.kind.freeShipping', " +
			"'admin.membership.entitlement.none', 'admin.membership.entitlement.percent', 'admin.membership.entitlement.percentHint', " +
			"'admin.membership.entitlement.percentHintShort', 'admin.membership.entitlement.save', 'admin.membership.entitlement.title', " +
			"'admin.membership.intro', 'admin.membership.lastError', 'admin.membership.link.assignments', " +
			"'admin.membership.link.tiers', 'admin.membership.loadFailed', 'admin.membership.menu.assignments', " +
			"'admin.membership.menu.tiers', 'admin.membership.tier.col.default', 'admin.membership.tier.col.entitlements', " +
			"'admin.membership.tier.col.name', 'admin.membership.tier.col.sort', 'admin.membership.tier.col.threshold', " +
			"'admin.membership.tier.create', 'admin.membership.tier.dangerHint', 'admin.membership.tier.dangerTitle', " +
			"'admin.membership.tier.defaultBadge', 'admin.membership.tier.editTitle', 'admin.membership.tier.empty', " +
			"'admin.membership.tier.emptyHeading', 'admin.membership.tier.field.isDefault', 'admin.membership.tier.field.isDefaultHint', " +
			"'admin.membership.tier.field.name', 'admin.membership.tier.field.remark', 'admin.membership.tier.field.sort', " +
			"'admin.membership.tier.field.sortHint', 'admin.membership.tier.field.threshold', 'admin.membership.tier.field.thresholdHint', " +
			"'admin.membership.tier.listLead', 'admin.membership.tier.listTail', 'admin.membership.tier.ph.name', " +
			"'admin.membership.tier.setDefault', 'admin.membership.title', 'membership.err.defaultTierExists', " +
			"'membership.err.defaultTierMissing', 'membership.err.defaultTierRequired', 'membership.err.entitlementKind', " +
			"'membership.err.entitlementValue', 'membership.err.internal', 'membership.err.invalidParam', " +
			"'membership.err.manualNotLocked', 'membership.err.notFound', 'membership.err.projectRequired', " +
			"'membership.err.recalcUnavailable', 'membership.err.thresholdInvalid', 'membership.err.thresholdTaken', " +
			"'membership.err.tierInUse', 'membership.err.tierNameTaken', 'membership.err.userRequired', " +
			"'membership.msg.assignSet', 'membership.msg.assignUnlocked', 'membership.msg.entitlementSaved', " +
			"'membership.msg.tierCreated', 'membership.msg.tierDeleted', 'membership.msg.tierUpdated'" +
			")",
		SQL: mustSQL("462a_membership_i18n.sql"),
	})
}
