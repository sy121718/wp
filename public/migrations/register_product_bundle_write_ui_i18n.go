package migrations

import "sync"

// register_product_bundle_write_ui_i18n.go — 商品捆绑页「写失败 + 重读也失败」档的三条界面文案（迁移 599）。
//
// 单独成一个主题文件而不是挤进相邻的 admin i18n 批次：这三个 key 是**写失败重渲染的
// 保输入路径**带出来的（重新载入入口 / 禁用的保存按钮 / 「输入已留下」的说明），
// 混进别的主题会让「这批词条为什么存在」在 review 时看不出来 —— 与 591/592/593/595 同源。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerProductBundleWriteUII18n() {
	registerProductBundleWriteUII18nOnce.Do(registerProductBundleWriteUII18nSeed)
}

var registerProductBundleWriteUII18nOnce sync.Once

func registerProductBundleWriteUII18nSeed() {
	// 599：3 个 key × 中英 = 6 行。
	//
	// 判定枚举本批**全部 3 个 key 的两种语言**（= 6 个 (item_key, lang) 组合）：
	//   · 不用全库行数、也不用前缀 LIKE —— 前者会让本批每次启动重跑，后者会被同期其它
	//     批次的行满足而静默跳过（见 226/277/283/592）；
	//   · **只数 lang='zh-CN' 的 3 行同样会静默跳过** —— 只要运营库里任意一条 en-US
	//     缺失（历史库缺行、有人手工删过英文、或本批 SQL 曾半途失败），中文计数就已经满足，
	//     判据返回 1，RunSeeds 跳过 SQL，缺的英文**永远补不上**，英文界面回退到组件包内的
	//     中文兜底（错得安静：没有异常、没有日志）。review-sol 在 task-33 对 598 抓到的
	//     就是这条 P2，本批同源。
	// 定则来自 AGENTS.md §数据库：「判据的偏差方向要刻意选**宁可重跑，不可静默跳过**」。
	// 4 项齐全时判据为 1（跳过）；缺任意一 (key, lang) 即为 0（重跑）—— 而 SQL 是
	// ON CONFLICT (item_key, lang) DO NOTHING，重跑只补缺行、不覆盖运营改过的值，
	// 所以这个偏差方向没有副作用。
	//
	// 语言标识写死这两种：它们就是本批 SQL 里出现的全部语言（用例
	// register_product_bundle_write_ui_i18n_test.go 会核对判据与 SQL 的组合集合一致）。
	registerSeed(Seed{
		Version:   "599-product-bundle-write-ui-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 4 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE lang IN ('zh-CN', 'en-US') AND item_key IN (" +
			"'admin.product_bundle.reload', " +
			"'admin.product_bundle.cfg.saveBlocked')",
		SQL: mustSQL("599_product_bundle_write_ui_i18n.sql"),
	})
}
