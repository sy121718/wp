package migrations

import "sync"

// register_admin_orders_write_ui_i18n.go — 订单列表页「JSON 写表单」的界面文案（迁移 604）。
//
// 单独成一个主题文件而不是挤进相邻的 order/admin i18n 批次：本批的 10 个 key 是**列表页六个
// 写动作（状态流转 / 取消 / 退款 / 备注 + 批量流转 / 批量取消）从前端直调 JSON API** 带出来的
// （两套批量结果三句摘要 + 没勾选 + 刷新入口 + 流转确认 + 无 JS 提示），混进别的主题会让
// 「这批词条为什么存在」在 review 时看不出来 —— 与 591/592/593/595/598/600/603 同源。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerAdminOrdersWriteUII18n() {
	registerAdminOrdersWriteUII18nOnce.Do(registerAdminOrdersWriteUII18nSeed)
}

var registerAdminOrdersWriteUII18nOnce sync.Once

func registerAdminOrdersWriteUII18nSeed() {
	// 604：10 个 key × 中英 = 20 行。
	//
	// 判定逐条枚举本批**全部 10 个 key 的两种语言**（= 20 个 (item_key, lang) 组合），
	// 不用全库行数、也不用前缀 LIKE：
	// 前者会被同期其它批次的行满足而静默跳过，后者同理（本仓库踩过，见 226/277/283/592）。
	// 语言也不能只数一种：只数 zh-CN（>= 10）时，中文齐、英文缺一条的库上判据仍返回 1 →
	// RunSeeds 跳过 → 缺的英文永远补不上（定则见 AGENTS.md §数据库「宁可重跑，不可静默跳过」）。
	// 阈值取**精确组合数 20**：低于它缺词也能满足，高于它判据永不满足、每次启动重跑。
	// 用例：public/migrations/register_admin_orders_write_ui_i18n_test.go（本批自带，
	// recent_write_ui_seed_guard_test.go 钉的是 590/591/592/593/595 那批固定历史集，不追加）。
	registerSeed(Seed{
		Version:   "604-admin-orders-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 20 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.orders.bulk.noneSelected', 'admin.orders.bulk.refresh', " +
			"'admin.orders.bulkCancel.allSkipped', 'admin.orders.bulkCancel.allUpdated', " +
			"'admin.orders.bulkCancel.partial', 'admin.orders.bulkStatus.allSkipped', " +
			"'admin.orders.bulkStatus.allUpdated', 'admin.orders.bulkStatus.partial', " +
			"'admin.orders.bulkStatusConfirm', 'admin.orders.write.noJs')",
		SQL: mustSQL("604_admin_orders_write_ui_i18n.sql"),
	})
}
