package migrations

// 755 播种内容模板列表页的提示词条（引用面状态与样例实体选取原因）。
// 守门逐条核对本批 7 个 key 的中英行，不按全库总量。
func init() {
	registerSeed(Seed{
		Version:   "755-content-template-hint-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 14 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
    ('ctImpUnavail','zh-CN'), ('ctImpUnavail','en-US'),
    ('ctImpUnpars','zh-CN'), ('ctImpUnpars','en-US'),
    ('ctHintProdMod','zh-CN'), ('ctHintProdMod','en-US'),
    ('ctHintCntMod','zh-CN'), ('ctHintCntMod','en-US'),
    ('ctHintNoProd','zh-CN'), ('ctHintNoProd','en-US'),
    ('ctHintNoArt','zh-CN'), ('ctHintNoArt','en-US'),
    ('ctHintEntMiss','zh-CN'), ('ctHintEntMiss','en-US'))`,
		SQL: mustSQL("755_content_template_hint_i18n.sql"),
	})
}
