package migrations

// 738 的旧 SQL 先 DO NOTHING 插入再删旧行，遇到运营自定义值或额外语言会静默丢失。
// 现行 SQL 换成共享安全搬迁（747_shared_feedback_short_keys_i18n.sql）：
// 先逐语言检查旧/新冲突（不同则整批回滚并定位 old -> new/lang），再原位改名、
// 仅对缺失目标补默认；738 与正式迁移 747 复用同一份 SQL，避免两套映射分叉。
//
// 守门逐条核对本批 14 个 (item_key, lang) 对，并要求六组旧 key 全部不存在：
// 旧行还在说明搬迁未完成（或又被别的 seed 种回），此时必须重跑，
// 不能因为目标行齐全就跳过、把旧数据留在库里。
func init() {
	registerSeed(Seed{
		Version:   "738-shared-error-short-keys",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN
    NOT EXISTS (SELECT 1 FROM (VALUES
        ('uiInternalErr', 'zh-CN'), ('uiInternalErr', 'en-US'),
        ('sysErrBadParam', 'zh-CN'), ('sysErrBadParam', 'en-US'),
        ('sysErrNoPerm', 'zh-CN'), ('sysErrNoPerm', 'en-US'),
        ('sysErrTooLarge', 'zh-CN'), ('sysErrTooLarge', 'en-US'),
        ('uiOpSuccess', 'zh-CN'), ('uiOpSuccess', 'en-US'),
        ('uiDelConfirm', 'zh-CN'), ('uiDelConfirm', 'en-US'),
        ('brandNoneSel', 'zh-CN'), ('brandNoneSel', 'en-US')
    ) AS want(item_key, lang)
    WHERE NOT EXISTS (
        SELECT 1 FROM sys_i18n t WHERE t.item_key = want.item_key AND t.lang = want.lang))
    AND NOT EXISTS (SELECT 1 FROM sys_i18n WHERE item_key IN (
        'MsgInternalError', 'ErrInvalidParams', 'ErrPermissionDenied',
        'ErrRequestEntityTooLarge', 'msg_operation_success', 'msg_delete_confirm'
    ))
THEN 1 ELSE 0 END`,
		SQL: mustSQL("747_shared_feedback_short_keys_i18n.sql"),
	})
}
