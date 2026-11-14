package migrations

// 746a 修复「退役 key 与已存在新 key 同时存在」的重复行。
//
// 背景：747 的冲突守卫按设计拒绝「旧行与新行元数据不同」，绝不静默二选一。但升级库里
// 会真实出现这种形态 —— 早期短键迁移复制出一行，之后某个域迁移又把退役 key 用另一套文案
// 重新播种，于是两边都在。这不是运营编辑，而是历史迁移自身留下的重复；747 不该为它放宽，
// 需要一条**前置条件全部满足才动手**的修复。
//
// 本迁移只处理「文案不同、恰好一行等于规范默认值，且待删的非规范副本无编辑痕迹」；
// 其余记录保持不动，由 747 按原有严格语义处理冲突，不在此替运营二选一。
//
// 执行顺序由版本号保证：746a 排在 746 之后、747 之前，先清重复行，747 再按原逻辑
// 处理「只剩退役行」的语言（或直接跳过）。
func init() {
	register(Migration{
		Version:   "746a-shared-feedback-legacy-duplicate-repair",
		TableName: "sys_i18n",
		// 只在真正存在重复行时才执行；没有重复行时返回 1 → 跳过。
		CheckSQL: `SELECT CASE WHEN EXISTS (
    SELECT 1
    FROM (VALUES
        ('MsgInternalError', 'uiInternalErr'),
        ('ErrInvalidParams', 'sysErrBadParam'),
        ('ErrPermissionDenied', 'sysErrNoPerm'),
        ('ErrRequestEntityTooLarge', 'sysErrTooLarge'),
        ('msg_operation_success', 'uiOpSuccess'),
        ('msg_delete_confirm', 'uiDelConfirm')
    ) AS p(old_key, new_key)
    JOIN sys_i18n o ON o.item_key = p.old_key
    JOIN sys_i18n n ON n.item_key = p.new_key AND n.lang = o.lang
) THEN 0 ELSE 1 END`,
		SQL: mustSQL("746a_shared_feedback_legacy_duplicate_repair.sql"),
	})
}
