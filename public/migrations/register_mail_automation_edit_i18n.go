package migrations

// 524 · 自动化流程编辑页（步骤化重做）新增文案的词条补齐。
//
// 判定写成本批自己的自洽门槛，而不是全库计数：只有当这 52 个 key 的双语
// (item_key, lang) 元组一个不缺时才算完成。这样中途失败（比如只插了一半）
// 不会被误判成已完成而永不重跑，也不会因为别的批次往 sys_i18n 里加了行
// 就被判为「已完成」（存量库永远满足的计数条件等于永久跳过）。
//
// 52 个 key 里 25 个来自模板 mail_automation_edit.html、27 个来自
// enums/mail_ui_labels.go；不做「已 seed 键数」的模糊判定，是因为
// scripts/check-i18n-keys-seeded.sh 只认 INSERT 的 VALUES 元组 ——
// 把 key 列在这里（注释或 IN 列表）都不算已 seed。
//
// ConditionSQL 由 db.Raw 直接执行、没有参数替换，所以整段写成 SQL 字面量。
func init() {
	registerSeed(Seed{
		Version:   "524-mail-automation-edit-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN (
    SELECT count(*) = 104
       AND count(DISTINCT item_key) = 52
       AND count(DISTINCT lang) = 2
    FROM sys_i18n
    WHERE item_key IN (
        'admin.mail.automation_edit.add_step',
        'admin.mail.automation_edit.col.actions',
        'admin.mail.automation_edit.col.condition',
        'admin.mail.automation_edit.col.no',
        'admin.mail.automation_edit.col.no_suffix',
        'admin.mail.automation_edit.col.step',
        'admin.mail.automation_edit.col.type_suffix',
        'admin.mail.automation_edit.col.yes',
        'admin.mail.automation_edit.col.yes_suffix',
        'admin.mail.automation_edit.cond.clicked',
        'admin.mail.automation_edit.cond.has_tag',
        'admin.mail.automation_edit.cond.none',
        'admin.mail.automation_edit.cond.opened',
        'admin.mail.automation_edit.cond.subscribed',
        'admin.mail.automation_edit.err.assemble_failed',
        'admin.mail.automation_edit.err.end_not_last',
        'admin.mail.automation_edit.err.name_required',
        'admin.mail.automation_edit.err.step_condition_required',
        'admin.mail.automation_edit.err.step_delay_unit',
        'admin.mail.automation_edit.err.step_delay_value',
        'admin.mail.automation_edit.err.step_tag_required',
        'admin.mail.automation_edit.err.step_target_backward',
        'admin.mail.automation_edit.err.step_target_invalid',
        'admin.mail.automation_edit.err.step_template_required',
        'admin.mail.automation_edit.err.step_type_required',
        'admin.mail.automation_edit.err.steps_required',
        'admin.mail.automation_edit.err.steps_too_many',
        'admin.mail.automation_edit.fallback',
        'admin.mail.automation_edit.fallback_heading',
        'admin.mail.automation_edit.guide.branch',
        'admin.mail.automation_edit.guide.enable',
        'admin.mail.automation_edit.guide.order',
        'admin.mail.automation_edit.move_down',
        'admin.mail.automation_edit.move_up',
        'admin.mail.automation_edit.next_end',
        'admin.mail.automation_edit.next_step',
        'admin.mail.automation_edit.param.blank',
        'admin.mail.automation_edit.param.delay_unit',
        'admin.mail.automation_edit.param.delay_value',
        'admin.mail.automation_edit.param.email',
        'admin.mail.automation_edit.param.tag',
        'admin.mail.automation_edit.param.tag_name',
        'admin.mail.automation_edit.remove',
        'admin.mail.automation_edit.remove_step',
        'admin.mail.automation_edit.step_label',
        'admin.mail.automation_edit.step_none',
        'admin.mail.automation_edit.switch_ctl',
        'admin.mail.automation_edit.target.end',
        'admin.mail.automation_edit.target.next',
        'admin.mail.automation_edit.unit.day',
        'admin.mail.automation_edit.unit.hour',
        'admin.mail.automation_edit.unit.minute'
    )
) THEN 1 ELSE 0 END`,
		SQL: mustSQL("524_mail_automation_edit_i18n.sql"),
	})
}
