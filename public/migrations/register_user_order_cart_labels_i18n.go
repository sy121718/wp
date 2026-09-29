package migrations

func init() {
	registerSeed(Seed{
		Version:   "448-i18n-user-order-cart-labels",
		TableName: "sys_i18n",
		// 门槛判据**枚举本批自己的 key**（上界封闭，76 个 key × 2 语言 = 152 行）：
		// 用 LIKE 前缀会让「别批次已有同前缀行」把计数抬高、本批被静默跳过（058 的故障），
		// 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的故障）。
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 152 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN ('admin.orders.operator_type.admin', 'admin.orders.operator_type.system', " +
			"'admin.orders.operator_type.customer', 'admin.orders.created_via.checkout', " +
			"'admin.orders.created_via.admin', 'admin.orders.created_via.api', " +
			"'admin.orders.form.invalid_id', 'order.bulk.cancelReasonRequired', " +
			"'order.bulk.returnRejectReasonRequired', 'admin.returns.status.requested', " +
			"'admin.returns.status.approved', 'admin.returns.status.received', " +
			"'admin.returns.status.completed', 'admin.returns.status.rejected', " +
			"'admin.returns.status.cancelled', 'admin.returns.form.invalid_id', " +
			"'admin.returns.filter.pending_hint', 'admin.returns.form.default_warehouse', " +
			"'admin.coupons.status.enabled', 'admin.coupons.status.expired', " +
			"'admin.coupons.status.not_started', 'admin.coupons.status.exhausted', " +
			"'admin.coupons.form.invalid_id', 'admin.coupons.form.foreign_project', " +
			"'admin.coupons.type.percent', 'admin.coupons.type.fixed', " +
			"'admin.coupons.form.enabled', 'admin.coupons.form.disabled', " +
			"'admin.coupons.redemption.anonymous', 'admin.coupons.discount.percent', " +
			"'admin.coupons.discount.fixed', 'admin.coupons.discount.yuan', " +
			"'admin.customers.form.invalid_id', 'admin.customer_detail.orders.unavailable', " +
			"'admin.customers.bulk.none_selected', 'user.bulk.prefix.status', " +
			"'user.bulk.prefix.unlock', 'user.bulk.tail.none', 'user.bulk.joiner', 'user.bulk.period', " +
			"'user.bulk.status.done', 'user.bulk.status.skipped', 'user.bulk.unlock.done', " +
			"'user.bulk.unlock.noop', 'user.bulk.unlock.skipped', 'user.bulk.verb.enabled', " +
			"'user.bulk.verb.disabled', 'user.page.login', 'user.page.register', " +
			"'user.page.register_done', 'user.page.forgot', 'user.page.reset', 'user.page.message', " +
			"'user.page.account', 'user.message.activation_invalid.title', " +
			"'user.message.activation_failed.title', 'user.message.activated.body', " +
			"'user.message.resend_failed.title', 'user.message.resend_done.title', " +
			"'user.message.resend_done.body', 'user.message.logout_failed.title', " +
			"'user.message.reset_mail.title', 'user.message.link_invalid.title', " +
			"'user.message.password_reset.title', 'user.message.password_reset.body', " +
			"'user.message.account_open_failed.title', 'user.message.password_changed.title', " +
			"'user.message.password_changed.body', 'cart.msg.callbackReceived', " +
			"'cart.msg.callbackApplied', 'cart.msg.callbackUnpaid', 'cart.msg.callbackNeedsReview', " +
			"'cart.msg.callbackAlreadyPaid', 'site.fragment.stock.off_shelf', " +
			"'site.fragment.stock.insufficient', 'cart.payment.paypalMock') AND lang IN ('zh-CN', 'en-US')",
		SQL: mustSQL("448_i18n_user_order_cart_labels.sql"),
	})
}
