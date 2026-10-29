package migrations

func init() {
	registerSeed(Seed{
		Version: "693-add-to-cart-unavailable-i18n", TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*)=2 THEN 1 ELSE 0 END FROM sys_i18n WHERE item_key='site.component.addToCart.unavailable' AND lang IN ('zh-CN','en-US')`,
		SQL:          mustSQL("693_add_to_cart_unavailable_i18n.sql"),
	})
}
