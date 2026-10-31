package migrations

import "sync"

func registerOrderNewI18n() {
	registerOrderNewI18nOnce.Do(registerOrderNewI18nSeed)
}

var registerOrderNewI18nOnce sync.Once

func registerOrderNewI18nSeed() {

	registerSeed(Seed{
		Version:   "433-i18n-order-new",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 76 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.order_new.heading','admin.order_new.help.label'," +
			"'admin.order_new.help.lead','admin.order_new.help.strong_snapshot'," +
			"'admin.order_new.help.mid','admin.order_new.help.strong_cent'," +
			"'admin.order_new.help.mid2','admin.order_new.help.strong_pending'," +
			"'admin.order_new.help.mid3','admin.order_new.help.strong_account'," +
			"'admin.order_new.help.tail','admin.order_new.err_prefix'," +
			"'admin.order_new.field_error_hint','admin.order_new.action.back'," +
			"'admin.order_new.action.submit','admin.order_new.section.customer'," +
			"'admin.order_new.field.email','admin.order_new.ph.email'," +
			"'admin.order_new.field.customer_name','admin.order_new.ph.customer_name'," +
			"'admin.order_new.field.customer_phone','admin.order_new.section.items'," +
			"'admin.order_new.col.variant','admin.order_new.col.quantity'," +
			"'admin.order_new.option.choose','admin.order_new.option.available'," +
			"'admin.order_new.items.no_candidate','admin.order_new.items.add_row'," +
			"'admin.order_new.items.row_limit_lead','admin.order_new.items.row_limit_tail'," +
			"'admin.order_new.candidate.heading','admin.order_new.candidate.keyword'," +
			"'admin.order_new.candidate.ph','admin.order_new.candidate.submit'," +
			"'admin.order_new.candidate.count_lead','admin.order_new.candidate.count_tail'," +
			"'admin.order_new.candidate.hint.lead','admin.order_new.candidate.hint.strong_match'," +
			"'admin.order_new.candidate.hint.mid','admin.order_new.candidate.hint.strong_reload'," +
			"'admin.order_new.candidate.hint.tail','admin.order_new.section.shipping'," +
			"'admin.order_new.field.ship_name','admin.order_new.field.ship_phone'," +
			"'admin.order_new.field.province','admin.order_new.field.city'," +
			"'admin.order_new.field.district','admin.order_new.field.address'," +
			"'admin.order_new.field.zip','admin.order_new.section.billing'," +
			"'admin.order_new.field.same_billing','admin.order_new.field.bill_name'," +
			"'admin.order_new.field.bill_phone','admin.order_new.section.amount'," +
			"'admin.order_new.field.shipping_total','admin.order_new.ph.shipping_total'," +
			"'admin.order_new.field.coupon','admin.order_new.ph.coupon'," +
			"'admin.order_new.amount.hint.lead','admin.order_new.amount.hint.strong_coupon'," +
			"'admin.order_new.amount.hint.mid','admin.order_new.amount.hint.strong_discount'," +
			"'admin.order_new.amount.hint.tail','admin.order_new.section.payment'," +
			"'admin.order_new.field.pay_method','admin.order_new.ph.pay_method'," +
			"'admin.order_new.field.pay_title','admin.order_new.ph.pay_title'," +
			"'admin.order_new.field.remark','admin.order_new.ph.remark'," +
			"'admin.order_new.field.admin_note','admin.order_new.ph.admin_note'," +
			"'admin.order_new.section.account','admin.order_new.field.provision'," +
			"'admin.order_new.provision.off_hint','admin.orders.action.create')",
		SQL: mustSQL("433_i18n_order_new.sql"),
	})
}

func init() { registerOrderNewI18n() }
