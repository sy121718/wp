package migrations

import "sync"

// register_bulk_notice_i18n.go — 批量操作结论文案 key 化的词条（迁移 283）。
//
// 为什么单独成一个主题文件：这一批横跨 admin / order / content / product / page / inventory
// 六个模块，但改动的是**同一件事**（handler 自己拼的批量回执从中文常量改成 i18n key），
// 混进 register_admin_i18n.go（那个文件的名字与注释都写着「admin 后台词条」）会让
// 「这批词条是谁的」在 review 时看不出来。
//
// 覆盖的六组（每组都遵循同一条判据：写侧与读侧共用同一个取词函数）：
//
//	· admin   —— admin.bulk.*（六领域列表页批量删除，模板 + 六个名词）、
//	             admin.i18nBulk.*（词条页批量删除的四个分支）；
//	· order   —— order.bulk.*（模板 + 七个动词 + 三个名词 + 一条参数级回执）；
//	· content —— content.bulk.*（文章列表页批量删除的四个分支）；
//	· product —— product.bulk.*（五个实体的批量删除 + 批量改价 + 变体清单保存）；
//	· page    —— page.bulk.*（页面列表批量删除 + 缺 id 提示 + 重定向批量删除）；
//	· inventory —— admin.inventory.bulk.sourcePartial / sourceDone（货源页，与仓库页的
//	             admin.inventory.bulk.partial / deleted 同族）。
func registerBulkNoticeI18n() {
	registerBulkNoticeI18nOnce.Do(registerBulkNoticeI18nSeed)
}

// registerBulkNoticeI18nOnce 让「自注册的 init()」与「register.go 里的显式一行」同时存在也安全
// （见下方 init 的注释）：重复调用只是空操作，不会重复 registerSeed。
var registerBulkNoticeI18nOnce sync.Once

// registerBulkNoticeI18nSeed 注册 283 的 seed（真正干活的那一半，被 Once 包一层）。
func registerBulkNoticeI18nSeed() {
	// 283：60 个 key × 中英 = 120 行。
	//
	// 判定枚举本批**全部 60 个 key**（>=60），不用全库行数：用全库行数会被同期其它批次的行
	// 满足而静默跳过（本仓库踩过，理由见 226/277）。
	// ConditionSQL 里没有占位符 —— 迁移器传进来的 ? 是表名，拿它当 key 会让判定恒为 0、
	// 每次启动重跑（178 踩过）。
	registerSeed(Seed{
		Version:   "283-bulk-notice-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 60 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang = 'zh-CN' AND item_key IN (" +
			"'admin.bulk.done', 'admin.bulk.noun.admin', 'admin.bulk.noun.datarule', " +
			"'admin.bulk.noun.dept', 'admin.bulk.noun.menu', 'admin.bulk.noun.permission', " +
			"'admin.bulk.noun.role', 'admin.bulk.partial', 'admin.i18nBulk.allDeleted', " +
			"'admin.i18nBulk.allSkipped', 'admin.i18nBulk.noneSelected', 'admin.i18nBulk.partial', " +
			"'admin.inventory.bulk.sourceDone', 'admin.inventory.bulk.sourcePartial', 'content.bulk.allDeleted', " +
			"'content.bulk.allSkipped', 'content.bulk.noneSelected', 'content.bulk.partial', " +
			"'order.bulk.allDone', 'order.bulk.allSkipped', 'order.bulk.couponTargetInvalid', " +
			"'order.bulk.noneSelected', 'order.bulk.noun.coupon', 'order.bulk.noun.order', " +
			"'order.bulk.noun.return', 'order.bulk.partial', 'order.bulk.verb.approved', " +
			"'order.bulk.verb.cancelled', 'order.bulk.verb.deleted', 'order.bulk.verb.disabled', " +
			"'order.bulk.verb.enabled', 'order.bulk.verb.flowed', 'order.bulk.verb.rejected', " +
			"'page.bulk.pageAllDeleted', 'page.bulk.pageAllSkipped', 'page.bulk.pageMissingID', " +
			"'page.bulk.pageNoneSelected', 'page.bulk.pagePartial', 'page.bulk.redirectAllDeleted', " +
			"'page.bulk.redirectAllSkipped', 'page.bulk.redirectNoneSelected', 'page.bulk.redirectPartial', " +
			"'product.bulk.attrDone', 'product.bulk.attrPartial', 'product.bulk.brandDone', " +
			"'product.bulk.brandPartial', 'product.bulk.categoryDone', 'product.bulk.categoryPartial', " +
			"'product.bulk.pricingAllSkip', 'product.bulk.pricingApplied', 'product.bulk.pricingNoChange', " +
			"'product.bulk.pricingNoneSelected', 'product.bulk.pricingPartial', 'product.bulk.productDone', " +
			"'product.bulk.productPartial', 'product.bulk.tagDone', 'product.bulk.tagPartial', " +
			"'product.bulk.variantSaveNoChange', 'product.bulk.variantSaveSaved', 'product.bulk.variantSaveSkipped')",
		SQL: mustSQL("283_bulk_notice_i18n.sql"),
	})
}

// 注册方式：由 register.go 的 init() 显式调用 registerBulkNoticeI18n()。
//
// 2026-09-19 第五批收口：本文件曾短暂带一个包内 init() 自注册（283 那批的子代理为了
// 不改共享的 register.go 而绕开约定）。已删除 —— 本仓库的约定是「注册顺序在 diff 里
// 可见」（见 register.go 的 init() 与 register_plugin_patrol_i18n.go 的注释），
// 自注册会让「谁在什么时候注册了哪一条」散落在文件尾，review 时看不见。
