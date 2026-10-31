package migrations

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
