package migrations

import "sync"

// register_admin_customer_filter_i18n.go — 客户列表筛选归口文案 + 商品评分参数级提示（迁移 612）。
//
// 单独成一个主题文件：这 6 个 key 不是界面装饰，而是「取数整维失败时仍然说清哪一维没生效」
// 那条判据的用户可见面（见 SQL 文件头）。它们此前只以事实形态存在于 Go 侧、从未 seed，
// 于是中文环境完全看不出问题、切到英文才露出中文句子。
//
// 目前 init() 里的注册函数按主题手工列出（见 register.go），新增主题要在那里加一行调用。
func registerAdminCustomerFilterI18n() {
	registerAdminCustomerFilterI18nOnce.Do(registerAdminCustomerFilterI18nSeed)
}

var registerAdminCustomerFilterI18nOnce sync.Once

func registerAdminCustomerFilterI18nSeed() {
	// 612：6 个 key × 中英 = 12 行。
	//
	// 判定枚举本批**全部 6 个 key 的两种语言**（= 12 个 (item_key, lang) 组合）：
	//   · 不用全库行数、也不用前缀 LIKE —— 前者会让本批每次启动重跑，后者会被同期其它
	//     批次的行满足而静默跳过（见 226/277/283/592）；
	//   · **只数 lang='zh-CN' 的 6 行同样会静默跳过** —— 只要运营库里任意一条 en-US
	//     缺失（历史库缺行、有人手工删过英文），中文计数就已经满足、判据返回 1，
	//     RunSeeds 跳过 SQL，缺的英文永远补不上（错得安静：没有异常、没有日志）。
	// 定则来自 AGENTS.md §数据库：「判据的偏差方向要刻意选宁可重跑，不可静默跳过」。
	// 12 项齐全时判据为 1（跳过）；缺任意一 (key, lang) 即为 0（重跑）—— 而 SQL 是
	// ON CONFLICT (item_key, lang) DO NOTHING，重跑只补缺行、不覆盖运营改过的值。
	//
	// 语言标识写死这两种：它们就是本批 SQL 里出现的全部语言。判据与 SQL 的组合集合
	// 必须逐条对齐 —— 少枚举一个组合会让那条词条永远补不上（判据已满足、SQL 被跳过）。
	// 样本：register_page_translations_saved_i18n_test.go（613 的对账用例，
	// 同时核对组合集合、句值、ON CONFLICT 尾句与 guard 原文）。
	registerSeed(Seed{
		Version:   "612-admin-customer-filter-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) >= 12 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE (item_key, lang) IN (" +
			"('admin.customer.filter.rfm_unavailable', 'zh-CN'), ('admin.customer.filter.rfm_unavailable', 'en-US'), " +
			"('admin.customer.filter.segment_unavailable', 'zh-CN'), ('admin.customer.filter.segment_unavailable', 'en-US'), " +
			"('admin.customer.filter.tier_truncated', 'zh-CN'), ('admin.customer.filter.tier_truncated', 'en-US'), " +
			"('admin.customer.filter.tier_unavailable', 'zh-CN'), ('admin.customer.filter.tier_unavailable', 'en-US'), " +
			"('admin.customer.rfm.unavailable', 'zh-CN'), ('admin.customer.rfm.unavailable', 'en-US'), " +
			"('admin.products.rating.invalid', 'zh-CN'), ('admin.products.rating.invalid', 'en-US'))",
		SQL: mustSQL("612_admin_customer_filter_labels_i18n.sql"),
	})
}
