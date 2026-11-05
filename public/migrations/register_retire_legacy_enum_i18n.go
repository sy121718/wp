package migrations

// registerRetireLegacyEnumI18n 清理已退役的枚举 i18n key。
//
// 条件：这些行里任意一条还存在才执行；全部清干净后直接跳过，不重复写库。
func init() {
	register(Migration{
		Version:   "745-retire-legacy-enum-i18n",
		TableName: "sys_i18n",
		CheckSQL: `SELECT CASE WHEN EXISTS (
    SELECT 1 FROM sys_i18n WHERE (item_key, lang) IN (
      ('MsgInternalError', 'zh-CN'), ('MsgInternalError', 'en-US'),
      ('ErrInvalidParams', 'zh-CN'), ('ErrInvalidParams', 'en-US'),
      ('ErrPermissionDenied', 'zh-CN'), ('ErrPermissionDenied', 'en-US'),
      ('ErrRequestEntityTooLarge', 'zh-CN'), ('ErrRequestEntityTooLarge', 'en-US'),
      ('msg_operation_success', 'zh-CN'), ('msg_operation_success', 'en-US'),
      ('msg_delete_confirm', 'zh-CN'), ('msg_delete_confirm', 'en-US'))
) THEN 0 ELSE 1 END`,
		SQL: mustSQL("745_retire_legacy_enum_i18n.sql"),
	})
}
