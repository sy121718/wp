-- 524 · 自动化流程编辑页（步骤化重做）新增文案的双语词条。
--
-- 背景：编辑页从「7 列表格 + 裸标识/下一步/YES/NO 输入框」改成「基本信息 + 按顺序的步骤」，
-- 表单里的标签、下拉选项、校验提示全部走 i18n。这些 key 有两个来源：
--   · internal/templates/admin/mail/mail_automation_edit.html —— 模板直接写的 {{ .["t"]("key", "兜底") }}
--   · internal/module/mail/enums/mail_ui_labels.go       —— LabelPair{key, 中文} 经数据结构注入模板
-- 两条来源的 key 都必须有词条，否则英文界面回落到中文兜底。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
-- 本批 52 个 key（zh-CN + en-US 共 104 条）在写成时确实是「一个都没有」，
-- 由 scripts/check-i18n-keys-seeded.sh 判定（它只认 INSERT 的 VALUES 元组，
-- 把 key 写进注释或写进 register 的条件列表都不算已 seed）。
--
-- 文案契约：本文件的中文值必须与模板兜底 / LabelPair 第二字段**逐字一致**。
-- 若日后改 UI 措辞，两处要一起改；只改模板不改这里，页面就会出现
--「兜底写 A、DB 值写 B」的假翻译（本轮 P1 / P3 两条真缺陷就是这个形态）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    -- ── 模板直接引用的 25 条 ────────────────────────────────────────────────
    ('admin.mail.automation_edit.add_step', 'zh-CN', '＋ 添加一步', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.add_step', 'en-US', '＋ Add a step', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.actions', 'zh-CN', '操作', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.actions', 'en-US', 'Actions', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.condition', 'zh-CN', '判断条件', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.condition', 'en-US', 'Condition', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.no', 'zh-CN', '不满足时', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.no', 'en-US', 'If not met', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.no_suffix', 'zh-CN', '：不满足时', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.no_suffix', 'en-US', ': if not met', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.step', 'zh-CN', '步骤', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.step', 'en-US', 'Step', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.type_suffix', 'zh-CN', '：做什么', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.type_suffix', 'en-US', ': action', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.yes', 'zh-CN', '满足时', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.yes', 'en-US', 'If met', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.yes_suffix', 'zh-CN', '：满足时', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.col.yes_suffix', 'en-US', ': if met', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.fallback', 'zh-CN', '它用了表单表达不了的跳转，为避免改坏，这里只显示原样。请用画布修改。', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.fallback', 'en-US', 'This automation uses jumps the step form cannot express; to avoid breaking it, the original is shown as-is. Edit it on the canvas.', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.fallback_heading', 'zh-CN', '这条流程不能用步骤表单编辑', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.fallback_heading', 'en-US', 'This automation cannot be edited with the step form', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.guide.branch', 'zh-CN', '「条件分支」先选一个判断条件（打开过邮件 / 点击过链接 / 已经订阅 / 带着某个标签），再分别指定「满足」和「不满足」时跳到哪一步或结束。', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.guide.branch', 'en-US', 'A condition branch first picks a condition (opened the email / clicked a link / is subscribed / has a certain tag), then says which step the "met" and "not met" outcomes jump to — or ends the flow.', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.guide.enable', 'zh-CN', '流程保存后要「启用」才会接收新的人。', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.guide.enable', 'en-US', 'A saved automation only takes new contacts once it is enabled.', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.guide.order', 'zh-CN', '从上往下执行：一步做完做下一步。只有「条件分支」需要你指定跳到哪一步，其它步骤系统自动接着往下走。', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.guide.order', 'en-US', 'Steps run from top to bottom: each one leads into the next. Only a condition branch needs you to pick a jump target; every other step continues automatically.', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.move_down', 'zh-CN', '下移', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.move_down', 'en-US', 'Move down', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.move_up', 'zh-CN', '上移', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.move_up', 'en-US', 'Move up', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.blank', 'zh-CN', '选一个类型后填参数', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.blank', 'en-US', 'Pick a type, then fill in the parameters', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.delay_unit', 'zh-CN', '等待单位', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.delay_unit', 'en-US', 'Wait unit', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.delay_value', 'zh-CN', '等待时长', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.delay_value', 'en-US', 'Wait duration', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.email', 'zh-CN', '邮件模板 key', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.email', 'en-US', 'Mail template key', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.tag', 'zh-CN', '标签，多个用逗号分隔', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.tag', 'en-US', 'Tags, separated by commas', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.tag_name', 'zh-CN', '标签名', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.param.tag_name', 'en-US', 'Tag name', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.remove', 'zh-CN', '删除', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.remove', 'en-US', 'Delete', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.remove_step', 'zh-CN', '删除这一步？', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.remove_step', 'en-US', 'Delete this step?', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.switch_ctl', 'zh-CN', '换成这个类型', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    ('admin.mail.automation_edit.switch_ctl', 'en-US', 'Switch to this type', 200, 'admin', 'admin/mail/mail_automation_edit.html', 1, now(), now()),
    -- ── enums 经结构注入的 27 条 ───────────────────────────────────────────
    ('admin.mail.automation_edit.cond.clicked', 'zh-CN', '点击过链接', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.clicked', 'en-US', 'Clicked a link', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.has_tag', 'zh-CN', '带着某个标签', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.has_tag', 'en-US', 'Has a certain tag', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.none', 'zh-CN', '请选择判断条件', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.none', 'en-US', 'Select a condition', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.opened', 'zh-CN', '打开过邮件', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.opened', 'en-US', 'Opened the email', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.subscribed', 'zh-CN', '已经订阅', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.cond.subscribed', 'en-US', 'Is subscribed', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.assemble_failed', 'zh-CN', '流程保存失败，请检查各步的填写内容后重试', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.assemble_failed', 'en-US', 'Saving the automation failed — check each step and try again.', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.end_not_last', 'zh-CN', '第 %d 步：结束步骤必须是最后一步', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.end_not_last', 'en-US', 'Step %d: the end step must be the last step', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.name_required', 'zh-CN', '流程名称不能为空', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.name_required', 'en-US', 'Automation name is required', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_condition_required', 'zh-CN', '第 %d 步：请选择判断条件', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_condition_required', 'en-US', 'Step %d: select a condition', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_delay_unit', 'zh-CN', '第 %d 步：等待单位只能选分钟 / 小时 / 天', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_delay_unit', 'en-US', 'Step %d: the wait unit must be minutes, hours, or days', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_delay_value', 'zh-CN', '第 %d 步：等待时长要填大于 0 的整数', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_delay_value', 'en-US', 'Step %d: the wait time must be a whole number greater than 0', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_tag_required', 'zh-CN', '第 %d 步：请填写至少一个标签', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_tag_required', 'en-US', 'Step %d: enter at least one tag', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_target_backward', 'zh-CN', '第 %d 步：跳转目标只能选本步之后的步骤', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_target_backward', 'en-US', 'Step %d: the target must be a step after this one', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_target_invalid', 'zh-CN', '第 %d 步：跳转目标已不存在（可能已被删除），请重新选择', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_target_invalid', 'en-US', 'Step %d: the jump target no longer exists, choose another one', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_template_required', 'zh-CN', '第 %d 步：请选择要发送的邮件模板', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_template_required', 'en-US', 'Step %d: select the email template to send', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_type_required', 'zh-CN', '第 %d 步：请选择步骤类型', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.step_type_required', 'en-US', 'Step %d: select a step type', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.steps_required', 'zh-CN', '至少要排一个步骤', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.steps_required', 'en-US', 'Add at least one step', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.steps_too_many', 'zh-CN', '最多只能排 12 步，请拆分流程', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.err.steps_too_many', 'en-US', 'At most 12 steps — please split the automation', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.next_end', 'zh-CN', '下一步：结束', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.next_end', 'en-US', 'Next: end', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.next_step', 'zh-CN', '下一步：第 %d 步', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.next_step', 'en-US', 'Next: step %d', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.step_label', 'zh-CN', '第 %d 步', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.step_label', 'en-US', 'Step %d', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.step_none', 'zh-CN', '请选择步骤类型', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.step_none', 'en-US', 'Select a step type', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.target.end', 'zh-CN', '结束', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.target.end', 'en-US', 'End', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.target.next', 'zh-CN', '下一步', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.target.next', 'en-US', 'Next', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.unit.day', 'zh-CN', '天', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.unit.day', 'en-US', 'Days', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.unit.hour', 'zh-CN', '小时', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.unit.hour', 'en-US', 'Hours', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.unit.minute', 'zh-CN', '分钟', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now()),
    ('admin.mail.automation_edit.unit.minute', 'en-US', 'Minutes', 200, 'admin', 'enums/mail_ui_labels.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 回滚（仅在本迁移落库后需要撤销时手工执行）：
-- DELETE FROM sys_i18n WHERE remark IN ('admin/mail/mail_automation_edit.html', 'enums/mail_ui_labels.go')
--   AND item_key LIKE 'admin.mail.automation_edit.%'
--   AND item_key NOT IN (SELECT item_key FROM sys_i18n WHERE remark NOT IN ('admin/mail/mail_automation_edit.html', 'enums/mail_ui_labels.go'));
