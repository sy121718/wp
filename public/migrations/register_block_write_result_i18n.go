package migrations

func init() {
	registerSeed(Seed{
		Version:   "730-block-write-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 14 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('blkCreated', 'zh-CN'), ('blkCreated', 'en-US'),
  ('blkOpenEdit', 'zh-CN'), ('blkOpenEdit', 'en-US'),
  ('blkNoneSel', 'zh-CN'), ('blkNoneSel', 'en-US'),
  ('blkDelNone', 'zh-CN'), ('blkDelNone', 'en-US'),
  ('blkDelDone', 'zh-CN'), ('blkDelDone', 'en-US'),
  ('blkDelPart', 'zh-CN'), ('blkDelPart', 'en-US'),
  ('blkSaved', 'zh-CN'), ('blkSaved', 'en-US'))`,
		SQL: mustSQL("730_block_write_result_i18n.sql"),
	})
}
