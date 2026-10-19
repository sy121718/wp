package migrations

// 617 — 清掉退货页批量「部分成功 / 全部跳过」的存量词条。
//
// 这两个批量动作现在只有「整批成功」与「整批失败」两种回执（见 order 模块的批量事务口径），
// 605 里对应的六个 key 已无消费者：605 的 seed SQL 已删掉这 12 行，本迁移清已有库的存量。
//
// 注册走迁移（不是 seed）：它是一次性清理，没有「缺词要补」的门槛语义。
func init() {
	register(Migration{
		Version: "617-returns-bulk-receipt-cleanup",
		SQL:     mustSQL("617_returns_bulk_receipt_cleanup.sql"),
	})
}
