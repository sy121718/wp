package migrations

import "sync"

// register_order_new_i18n.go — 代客建单整页的词条（433）。
//
// 见 433_i18n_order_new.sql 的头部：order_new.html 是新增的整页表单（docs/02-W-admin-order-create.md），
// 页面与词条 seed 没有同批落地 —— 76 个 key 一条都不在 sys_i18n 里，英文界面下整页回落
// 模板里的中文兜底（页头说明、字段标签、候选 SKU 检索区、金额提示、开号提示）。
//
// 其中分段拼接词条（help.* / candidate.hint.* / amount.hint.* / *_lead / *_tail）
// 的段边界空格在 SQL 注释里逐条说明 —— 改词条前先读那段，尤其英文侧不能照中文的标点分隔去写。
//
// 注册方式：本文件自带 init()（与 register_customers_status_help_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerOrderNewI18n() {
	registerOrderNewI18nOnce.Do(registerOrderNewI18nSeed)
}

// registerOrderNewI18nOnce 让重复调用成为空操作。
var registerOrderNewI18nOnce sync.Once

// registerOrderNewI18nSeed 注册 433（真正干活的那一半）。
func registerOrderNewI18nSeed() {
	// 门槛 = 本批 76 个新 key 的 **en-US** 行都在（= 76 行）。挑 en-US 而不是 zh-CN：
	// 中文行与模板里的 fallback 同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立、整批词条被静默跳过（416 / 431 的同一理由）。
	//
	// 放在**种子**台账：本批只新增词条、不改任何既有 key，与其它 seed 无先后约束，
	// 但词条属于 seed 语义（可重复写入的默认值），与 416 / 431 保持一致。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换** —— 判定用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会被换成表名，判定恒为 0、每次启动重跑（178 踩过）。
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
