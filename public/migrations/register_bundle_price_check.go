package migrations

// register_bundle_price_check.go — 迁移 619：捆绑容器的容器价必须 > 0。
//
// 与 service 侧的 ErrBundlePriceRequired 是同一条不变量的两层实现：
// service 给出可读的业务错误，数据库挡住任何绕过 service 的写入口（迁移脚本 / 修复脚本 /
// 将来的第二个端点）。审查指出 Update 原先没有这条校验，而 081 的列只有 numeric 无 CHECK。
func registerBundlePriceCheck() {
	register(Migration{
		Version:   "619-bundle-price-check",
		TableName: "products",
		// 判据是「约束已存在」而不是「表存在」：表一直都在，用 TableName 判会让这条永远跳过。
		CheckSQL: "SELECT CASE WHEN to_regclass(?) IS NOT NULL AND " +
			"EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'ck_products_bundle_price_positive') " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("619_bundle_price_check.sql"),
	})
}
