package migrations

// register_customer_catalog.go — 客户目录（555）。
//
// 为什么是 Seed 而不是 Migration：本批改的是**数据行**（sys_menus 的分组与菜单项），
// 不是结构。Migration 的默认存在性检查看 `TableName` 那张表在不在 —— sys_menus 早就在了，
// 那条检查对本批毫无判别力（会直接判「已完成」而跳过）。Seed 的 ConditionSQL 才问得出
// 「这四行在不在」。
//
// 判据按本批自己的对象逐条枚举（上界封闭，AGENTS.md「seed 门槛判据」那一条）：
//   - 一级分组「订单」与「客户」都在（前者是 224 那个「交易」改名后的名字）；
//   - 「客户列表」这一项已经挂进「客户」组（**带 title 判定** —— 只看 path 的话，
//     改名那一步失败不会被发现，而侧栏会一直显示旧标题）；
//   - 「客户概览」那一项存在。
//
// 四个对象齐了才认完成。少一个就返回 0 → 重跑补齐（SQL 全部幂等）。
func registerCustomerCatalog() {
	registerSeed(customerCatalogSeed())
}

// customerCatalogSeed 导出给测试用（判据里的对象清单要被单独钉住）。
func customerCatalogSeed() Seed {
	return Seed{
		Version:   "555-customer-nav-directory",
		TableName: "sys_menus",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 4 THEN 1 ELSE 0 END FROM sys_menus " +
			"WHERE deleted_at IS NULL AND (" +
			"(type = 1 AND title = '订单') " +
			"OR (type = 1 AND title = '客户') " +
			"OR (type = 2 AND path = '/admin/customers' AND title = '客户列表') " +
			"OR (type = 2 AND path = '/admin/customers/overview')" +
			")",
		SQL: mustSQL("555_customer_nav_directory.sql"),
	}
}
