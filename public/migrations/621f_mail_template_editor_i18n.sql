-- 621f — 模板可视化编辑器（T6）的界面词条（中英成对）。
--
-- 两组：template_editor.*（四栏编辑器自己的文案）与 templates.action.visual
--（列表页进入编辑器的按钮）。能力位在模板里取词，与既有 admin.mail.* 同一命名空间。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_mail_template_editor_i18n.go 里**逐条枚举本批 21 个 item_key**（42 行）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.mail.template_editor.back', 'zh-CN', '返回模板列表', 200, 'admin', 'admin.mail.template_editor.back', 1, now(), now()),
('admin.mail.template_editor.back', 'en-US', 'Back to templates', 200, 'admin', 'admin.mail.template_editor.back', 1, now(), now()),
('admin.mail.template_editor.canvas.aria', 'zh-CN', '选中块的子块', 200, 'admin', 'admin.mail.template_editor.canvas.aria', 1, now(), now()),
('admin.mail.template_editor.canvas.aria', 'en-US', 'Blocks inside the selected block', 200, 'admin', 'admin.mail.template_editor.canvas.aria', 1, now(), now()),
('admin.mail.template_editor.field.key', 'zh-CN', '标识', 200, 'admin', 'admin.mail.template_editor.field.key', 1, now(), now()),
('admin.mail.template_editor.field.key', 'en-US', 'Key', 200, 'admin', 'admin.mail.template_editor.field.key', 1, now(), now()),
('admin.mail.template_editor.field.locale', 'zh-CN', '语言', 200, 'admin', 'admin.mail.template_editor.field.locale', 1, now(), now()),
('admin.mail.template_editor.field.locale', 'en-US', 'Locale', 200, 'admin', 'admin.mail.template_editor.field.locale', 1, now(), now()),
('admin.mail.template_editor.field.name', 'zh-CN', '名称', 200, 'admin', 'admin.mail.template_editor.field.name', 1, now(), now()),
('admin.mail.template_editor.field.name', 'en-US', 'Name', 200, 'admin', 'admin.mail.template_editor.field.name', 1, now(), now()),
('admin.mail.template_editor.field.subject', 'zh-CN', '主题', 200, 'admin', 'admin.mail.template_editor.field.subject', 1, now(), now()),
('admin.mail.template_editor.field.subject', 'en-US', 'Subject', 200, 'admin', 'admin.mail.template_editor.field.subject', 1, now(), now()),
('admin.mail.template_editor.field.vars', 'zh-CN', '变量', 200, 'admin', 'admin.mail.template_editor.field.vars', 1, now(), now()),
('admin.mail.template_editor.field.vars', 'en-US', 'Variables', 200, 'admin', 'admin.mail.template_editor.field.vars', 1, now(), now()),
('admin.mail.template_editor.heading', 'zh-CN', '模板编辑器', 200, 'admin', 'admin.mail.template_editor.heading', 1, now(), now()),
('admin.mail.template_editor.heading', 'en-US', 'Template editor', 200, 'admin', 'admin.mail.template_editor.heading', 1, now(), now()),
('admin.mail.template_editor.pane.attrs', 'zh-CN', '属性', 200, 'admin', 'admin.mail.template_editor.pane.attrs', 1, now(), now()),
('admin.mail.template_editor.pane.attrs', 'en-US', 'Properties', 200, 'admin', 'admin.mail.template_editor.pane.attrs', 1, now(), now()),
('admin.mail.template_editor.pane.canvas', 'zh-CN', '画布', 200, 'admin', 'admin.mail.template_editor.pane.canvas', 1, now(), now()),
('admin.mail.template_editor.pane.canvas', 'en-US', 'Canvas', 200, 'admin', 'admin.mail.template_editor.pane.canvas', 1, now(), now()),
('admin.mail.template_editor.pane.preview', 'zh-CN', '预览', 200, 'admin', 'admin.mail.template_editor.pane.preview', 1, now(), now()),
('admin.mail.template_editor.pane.preview', 'en-US', 'Preview', 200, 'admin', 'admin.mail.template_editor.pane.preview', 1, now(), now()),
('admin.mail.template_editor.pane.tree', 'zh-CN', '结构', 200, 'admin', 'admin.mail.template_editor.pane.tree', 1, now(), now()),
('admin.mail.template_editor.pane.tree', 'en-US', 'Structure', 200, 'admin', 'admin.mail.template_editor.pane.tree', 1, now(), now()),
('admin.mail.template_editor.ph.locale', 'zh-CN', '留空 = 通用', 200, 'admin', 'admin.mail.template_editor.ph.locale', 1, now(), now()),
('admin.mail.template_editor.ph.locale', 'en-US', 'Empty = the shared one', 200, 'admin', 'admin.mail.template_editor.ph.locale', 1, now(), now()),
('admin.mail.template_editor.preview.text', 'zh-CN', '纯文本正文', 200, 'admin', 'admin.mail.template_editor.preview.text', 1, now(), now()),
('admin.mail.template_editor.preview.text', 'en-US', 'Plain-text body', 200, 'admin', 'admin.mail.template_editor.preview.text', 1, now(), now()),
('admin.mail.template_editor.preview.title', 'zh-CN', '邮件预览', 200, 'admin', 'admin.mail.template_editor.preview.title', 1, now(), now()),
('admin.mail.template_editor.preview.title', 'en-US', 'Email preview', 200, 'admin', 'admin.mail.template_editor.preview.title', 1, now(), now()),
('admin.mail.template_editor.redo', 'zh-CN', '重做（Ctrl+Shift+Z）', 200, 'admin', 'admin.mail.template_editor.redo', 1, now(), now()),
('admin.mail.template_editor.redo', 'en-US', 'Redo (Ctrl+Shift+Z)', 200, 'admin', 'admin.mail.template_editor.redo', 1, now(), now()),
('admin.mail.template_editor.save', 'zh-CN', '保存并更新预览', 200, 'admin', 'admin.mail.template_editor.save', 1, now(), now()),
('admin.mail.template_editor.save', 'en-US', 'Save and refresh preview', 200, 'admin', 'admin.mail.template_editor.save', 1, now(), now()),
('admin.mail.template_editor.title', 'zh-CN', '模板编辑器', 200, 'admin', 'admin.mail.template_editor.title', 1, now(), now()),
('admin.mail.template_editor.title', 'en-US', 'Template editor', 200, 'admin', 'admin.mail.template_editor.title', 1, now(), now()),
('admin.mail.template_editor.tree.aria', 'zh-CN', '模板块树', 200, 'admin', 'admin.mail.template_editor.tree.aria', 1, now(), now()),
('admin.mail.template_editor.tree.aria', 'en-US', 'Block tree', 200, 'admin', 'admin.mail.template_editor.tree.aria', 1, now(), now()),
('admin.mail.template_editor.undo', 'zh-CN', '撤销（Ctrl+Z）', 200, 'admin', 'admin.mail.template_editor.undo', 1, now(), now()),
('admin.mail.template_editor.undo', 'en-US', 'Undo (Ctrl+Z)', 200, 'admin', 'admin.mail.template_editor.undo', 1, now(), now()),
('admin.mail.templates.action.visual', 'zh-CN', '可视化', 200, 'admin', 'admin.mail.templates.action.visual', 1, now(), now()),
('admin.mail.templates.action.visual', 'en-US', 'Visual editor', 200, 'admin', 'admin.mail.templates.action.visual', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
