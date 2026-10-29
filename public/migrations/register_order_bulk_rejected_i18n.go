package migrations

func init() {
	registerSeed(Seed{
		Version: "694-order-bulk-rejected-i18n", TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key='order.err.bulkRejected' AND lang IN ('zh-CN','en-US')`,
		SQL:          mustSQL("694_order_bulk_rejected_i18n.sql"),
	})
}
