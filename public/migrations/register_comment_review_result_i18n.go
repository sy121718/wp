package migrations

func init() {
	registerSeed(Seed{
		Version:   "724-comment-review-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN NOT EXISTS (
  SELECT 1 FROM (VALUES ('cmtDoneApprove'), ('cmtDoneReject')) AS keys(item_key)
  CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
  WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = keys.item_key AND t.lang = langs.lang)
) AND NOT EXISTS (SELECT 1 FROM sys_i18n WHERE item_key IN ('admin.comment.done.approved', 'admin.comment.done.rejected'))
THEN 1 ELSE 0 END`,
		SQL: mustSQL("724_comment_review_result_i18n.sql"),
	})
}
