package migrations

import "sync"

// register_admin_returns_write_ui_i18n.go — 退货入库页「JSON 写表单」的界面文案（迁移 605）。
//
// 单独成一个主题文件而不是挤进相邻的 order i18n 批次：本批的 3 个 key 是**退货页五个写动作
// （同意 / 拒绝 / 确认收货 / 批量同意 / 批量拒绝）从页面 POST 改为前端直调 JSON API** 带出来的
// （批量结论 + 没勾选 + 刷新入口 + 无 JS 提示），混进别的主题会让「这批词条为什么
// 存在」在 review 时看不出来 —— 与 591/592/593/595/598/600/603/604 同源。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerAdminReturnsWriteUII18n() {
	registerAdminReturnsWriteUII18nOnce.Do(registerAdminReturnsWriteUII18nSeed)
}

var registerAdminReturnsWriteUII18nOnce sync.Once

func registerAdminReturnsWriteUII18nSeed() {
	// 605：3 个 key × 中英 = 6 行（批量部分成功/全跳过的摘要随整批语义删除）。
	//
	// 判定逐条枚举本批**全部 3 个 key 的两种语言**（= 6 个 (item_key, lang) 组合），
	// 不用全库行数、也不用前缀 LIKE：
	// 前者会被同期其它批次的行满足而静默跳过，后者同理（本仓库踩过，见 226/277/283/592）。
	// 语言也不能只数一种：只数 zh-CN（>= 3）时，中文齐、英文缺一条的库上判据仍返回 1 →
	// RunSeeds 跳过 → 缺的英文永远补不上（定则见 AGENTS.md §数据库「宁可重跑，不可静默跳过」）。
	// 阈值取**精确组合数 6**：低于它缺词也能满足，高于它判据永不满足、每次启动重跑。
	// 用例：public/migrations/register_admin_returns_write_ui_i18n_test.go（本批自带，
	// recent_write_ui_seed_guard_test.go 钉的是 590/591/592/593/595 那批固定历史集，不追加）。
	registerSeed(Seed{
		Version:   "605-admin-returns-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 6 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.returns.bulk.noneSelected', 'admin.returns.bulk.refresh', " +
			"'admin.returns.write.noJs')",
		SQL: mustSQL("605_admin_returns_write_ui_i18n.sql"),
	})
}
