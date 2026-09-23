package migrations

import "sync"

// register_bulk_confirm_i18n.go — 批量操作条的按钮与确认文案（434）。
//
// 见 434_i18n_bulk_confirm.sql 的头部：模板的取词写法是文件顶部 {{tr := .["t"]}} 声明一次、
// range 内复用 {{tr("key", "兜底")}}。此前只按 {{ .["t"]("key", …) }} 直调形式扫描 key 时，
// 这批 key 整批漏掉 —— 它们既不在 sys_i18n 里、也没进过任何一批 seed，于是英文界面
// 在批量条上回落模板里的中文兜底（确认弹窗、按钮文字全露中文）。
//
// 注册方式：本文件自带 init()（与 register_customers_status_help_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
// init 的注册顺序不影响执行顺序：迁移按 compareVersion 排序、种子按版本号排序。
func registerBulkConfirmI18n() {
	registerBulkConfirmI18nOnce.Do(registerBulkConfirmI18nSeed)
}

// registerBulkConfirmI18nOnce 让重复调用成为空操作。
var registerBulkConfirmI18nOnce sync.Once

// registerBulkConfirmI18nSeed 注册 434（真正干活的那一半）。
func registerBulkConfirmI18nSeed() {
	// 门槛 = 本批 36 个新 key 的 **en-US** 行都在（= 36 行）。挑 en-US 而不是 zh-CN：
	// 中文行与模板里的 fallback 同形，容易被别处顺手加上，按 zh-CN 计数会让门槛在
	// 「本批还没跑」时就成立、整批词条被静默跳过（416 / 431 的同一理由）。
	//
	// 放在**种子**台账：本批只新增词条、不改任何既有 key，与其它 seed 无先后约束，
	// 但词条属于 seed 语义（可重复写入的默认值），与 416 / 431 保持一致。
	//
	// ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换** —— 判定用的 key 只能写成
	// SQL 字面量；写成 item_key = ? 会被换成表名，判定恒为 0、每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "434-i18n-bulk-confirm",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 36 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'en-US' AND item_key IN (" +
			"'admin.article.list.bulkDeleteConfirm','admin.blocks.bulk_delete_confirm'," +
			"'admin.common.bulk.column','admin.content.templates.bulkDeleteConfirm'," +
			"'admin.coupons.bulk.delete','admin.coupons.bulkDeleteConfirm'," +
			"'admin.coupons.bulk.target','admin.coupons.bulk.toggle'," +
			"'admin.customers.bulk.disable','admin.customers.bulk.disableConfirm'," +
			"'admin.customers.bulk.enable','admin.customers.bulk.enableConfirm'," +
			"'admin.customers.bulk.unlock','admin.customers.bulk.unlockConfirm'," +
			"'admin.inventory_sources.bulkDeleteConfirm','admin.inventory.warehouses.bulkDeleteConfirm'," +
			"'admin.mail.accounts.bulk_delete_confirm','admin.mail.marketing.bulk_status_confirm'," +
			"'admin.mail.marketing.bulk_status_submit','admin.mail.marketing.campaigns.bulk_delete_confirm'," +
			"'admin.mail.templates.bulk_delete_confirm','admin.orders.bulk.cancel'," +
			"'admin.orders.bulkCancelConfirm','admin.orders.bulk.status'," +
			"'admin.orders.bulk.target','admin.orders.ph.bulk_remark'," +
			"'admin.pages.bulk_delete_confirm','admin.product_attributes.bulkDeleteConfirm'," +
			"'admin.product_brands.bulkDeleteConfirm','admin.products.bulkDeleteConfirm'," +
			"'admin.product_tags.bulkDeleteConfirm','admin.redirect.bulk_delete_confirm'," +
			"'admin.returns.bulk.approve','admin.returns.bulk.reject'," +
			"'admin.returns.bulkRejectConfirm','admin.returns.ph.bulk_remark')",
		SQL: mustSQL("434_i18n_bulk_confirm.sql"),
	})
}

func init() { registerBulkConfirmI18n() }
