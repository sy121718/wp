package migrations

// 747 正式承接原 745 的危险 DELETE：不再直接删除旧行，而是把六组共享反馈词条
// （MsgInternalError→uiInternalErr、ErrInvalidParams→sysErrBadParam、
// ErrPermissionDenied→sysErrNoPerm、ErrRequestEntityTooLarge→sysErrTooLarge、
// msg_operation_success→uiOpSuccess、msg_delete_confirm→uiDelConfirm）
// 的全部语言、运营译文、禁用状态与时间等元数据原位搬到新 key。
//
// 迁移先于全部 seed 执行：升级时先搬旧值，后续 720 等 seed 重播不会再拿旧 key
// 当默认值；RunSeeds 被单独调用时由同源 seed 738 先走同一段安全搬迁。
//
// 条件：库里仍有任一条旧 key 才执行；没有旧数据时跳过，默认值由 058/738 负责。
func init() {
	register(Migration{
		Version:   "747-shared-feedback-short-keys-i18n",
		TableName: "sys_i18n",
		CheckSQL: `SELECT CASE WHEN EXISTS (
    SELECT 1 FROM sys_i18n WHERE item_key IN (
      'MsgInternalError', 'ErrInvalidParams', 'ErrPermissionDenied',
      'ErrRequestEntityTooLarge', 'msg_operation_success', 'msg_delete_confirm')
) THEN 0 ELSE 1 END`,
		SQL: mustSQL("747_shared_feedback_short_keys_i18n.sql"),
	})
}
