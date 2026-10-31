package migrations

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
