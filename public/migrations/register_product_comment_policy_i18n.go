package migrations

// 468 — 「商品评论必须买过」的拒绝文案词条（BIZ-5 差异化规则）。
//
// 为什么单独一条而不是并进 466a：466a 是 comment 模块自己那批词条（片段列表与表单的
// 固定文案），本批是**消费方**（product）为它实现的差异化规则所带来的词条 ——
// 「谁负责哪批词条」的边界跟着「谁声明这个 key」走，硬并进去会让那条边界消失。
//
// 门槛判据**逐条枚举本批自己的 1 个 item_key**（上界封闭，1 × 2 = 2 行）：
//   - 不用 LIKE 前缀：别的批次已有同前缀行时计数虚高 → 本批被静默跳过（058 的真实故障）；
//   - 不用全库总量：将来新增同前缀 key 时永远追不平 → 每次启动重跑（076 的真实故障）。
//
// 放在**种子**台账（registerSeed 而不是 register）：只新增词条、不改任何既有 key，
// 属 seed 语义（可重复写入的默认值）。
//
// 注册方式：本文件自带 init()（与 461 的 register_site_scripts_access_i18n.go 同形），
// 不在 register.go 的 init() 里再加一行 —— 那会让「谁负责注册」出现两个真源。
func init() {
	registerSeed(Seed{
		Version:   "468-product-comment-policy-i18n",
		TableName: "sys_i18n",
		ConditionSQL: "SELECT CASE WHEN COUNT(*) = 2 THEN 1 ELSE 0 END FROM sys_i18n " +
			"WHERE item_key IN (" +
			// 真源 internal/module/product/enums/product_enums.go 的 ErrCommentPurchaseRequired。
			"'product.err.commentPurchaseRequired'" +
			")",
		SQL: mustSQL("468_product_comment_policy_i18n.sql"),
	})
}
