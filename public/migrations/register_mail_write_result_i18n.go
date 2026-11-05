package migrations

func init() {
	registerSeed(Seed{
		Version:   "728-mail-write-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 64 THEN 1 ELSE 0 END FROM sys_i18n
WHERE (item_key, lang) IN (
  ('mailAccNoneSel', 'zh-CN'), ('mailAccNoneSel', 'en-US'),
  ('mailAccDelNone', 'zh-CN'), ('mailAccDelNone', 'en-US'),
  ('mailAccDelDone', 'zh-CN'), ('mailAccDelDone', 'en-US'),
  ('mailAccDelPart', 'zh-CN'), ('mailAccDelPart', 'en-US'),
  ('mailAccTestSent', 'zh-CN'), ('mailAccTestSent', 'en-US'),
  ('mailAutoNoneSel', 'zh-CN'), ('mailAutoNoneSel', 'en-US'),
  ('mailAutoDelNone', 'zh-CN'), ('mailAutoDelNone', 'en-US'),
  ('mailAutoDelDone', 'zh-CN'), ('mailAutoDelDone', 'en-US'),
  ('mailAutoDelPart', 'zh-CN'), ('mailAutoDelPart', 'en-US'),
  ('mailAutoStSet', 'zh-CN'), ('mailAutoStSet', 'en-US'),
  ('mailAutoTick', 'zh-CN'), ('mailAutoTick', 'en-US'),
  ('mailTplNoneSel', 'zh-CN'), ('mailTplNoneSel', 'en-US'),
  ('mailTplDelNone', 'zh-CN'), ('mailTplDelNone', 'en-US'),
  ('mailTplDelDone', 'zh-CN'), ('mailTplDelDone', 'en-US'),
  ('mailTplDelPart', 'zh-CN'), ('mailTplDelPart', 'en-US'),
  ('mailCmpNoneSel', 'zh-CN'), ('mailCmpNoneSel', 'en-US'),
  ('mailCmpDelNone', 'zh-CN'), ('mailCmpDelNone', 'en-US'),
  ('mailCmpDelDone', 'zh-CN'), ('mailCmpDelDone', 'en-US'),
  ('mailCmpDelPart', 'zh-CN'), ('mailCmpDelPart', 'en-US'),
  ('mailCmpStarted', 'zh-CN'), ('mailCmpStarted', 'en-US'),
  ('mailCtcNoneSel', 'zh-CN'), ('mailCtcNoneSel', 'en-US'),
  ('mailCtcDelNone', 'zh-CN'), ('mailCtcDelNone', 'en-US'),
  ('mailCtcDelDone', 'zh-CN'), ('mailCtcDelDone', 'en-US'),
  ('mailCtcDelPart', 'zh-CN'), ('mailCtcDelPart', 'en-US'),
  ('mailCtcTagNone', 'zh-CN'), ('mailCtcTagNone', 'en-US'),
  ('mailCtcTagDone', 'zh-CN'), ('mailCtcTagDone', 'en-US'),
  ('mailCtcTagPart', 'zh-CN'), ('mailCtcTagPart', 'en-US'),
  ('mailCtcStSaved', 'zh-CN'), ('mailCtcStSaved', 'en-US'),
  ('mailCtcStNone', 'zh-CN'), ('mailCtcStNone', 'en-US'),
  ('mailCtcStDone', 'zh-CN'), ('mailCtcStDone', 'en-US'),
  ('mailCtcStPart', 'zh-CN'), ('mailCtcStPart', 'en-US'),
  ('mailImpDone', 'zh-CN'), ('mailImpDone', 'en-US'))`,
		SQL: mustSQL("728_mail_write_result_i18n.sql"),
	})
}
