-- 291 · 结构模板后台改造的词条（内容模板列表页的生效 / 引用列 · 主题设置的结构模板下拉）
--
-- 背景：结构模板（页眉 / 页脚）从「只能 API 配」升格为可在后台管理 —— 模板列表页新增
-- 「类型 / 状态 / 引用」三列与「设为生效」按钮，主题设置页把三个隐藏域换成结构模板下拉。
-- 模板侧同批改成 `{{ .["t"]("key", "中文兜底") }}`，本迁移补齐这 20 个 key 的中英词条。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，后台是真相来源）。
-- 判定枚举本批**全部 19 个 key**（>=19）：用全库行数会被同期其它批次满足而静默跳过。
--
-- 注册：public/migrations/register_content_template_structure_i18n.go（由 register.go 的 init 调用）。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
	('admin.content.templates.introStructure', 'zh-CN', '结构模板（页眉 / 页脚）不是内容实体，也不接受字段绑定；同一工程内每类只允许一套生效，「设为生效」后引用它的页面与实例会重新构建。', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.introStructure', 'en-US', 'Structure templates (header / footer) are not content entities and accept no field bindings. Only one takes effect per type in a project — activating one rebuilds the pages and instances that reference it.', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.colType', 'zh-CN', '类型', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.colType', 'en-US', 'Type', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.colStatus', 'zh-CN', '状态', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.colStatus', 'en-US', 'Status', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.colRefs', 'zh-CN', '引用', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.colRefs', 'en-US', 'References', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.activeBadge', 'zh-CN', '当前生效', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.activeBadge', 'en-US', 'Active', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.activate', 'zh-CN', '设为生效', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.activate', 'en-US', 'Activate', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refPages', 'zh-CN', '页面', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refPages', 'en-US', 'Pages', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refInstances', 'zh-CN', '实例', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refInstances', 'en-US', 'Instances', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refNone', 'zh-CN', '无引用', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refNone', 'en-US', 'No references', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refUnknown', 'zh-CN', '引用未知', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.refUnknown', 'en-US', 'References unknown', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.entityHeader', 'zh-CN', '页眉（结构模板）', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.entityHeader', 'en-US', 'Header (structure template)', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.entityFooter', 'zh-CN', '页脚（结构模板）', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.entityFooter', 'en-US', 'Footer (structure template)', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.roleLabel', 'zh-CN', '角色', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.roleLabel', 'en-US', 'Role', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.noVisualEdit', 'zh-CN', '无可视化编辑入口', 'admin', '', 1, 200, now(), now()),
	('admin.content.templates.noVisualEdit', 'en-US', 'No visual editor', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_templates', 'zh-CN', '结构模板（页眉 / 页脚）', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_templates', 'en-US', 'Structure templates (header / footer)', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.header_template', 'zh-CN', '页眉模板', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.header_template', 'en-US', 'Header template', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.footer_template', 'zh-CN', '页脚模板', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.footer_template', 'en-US', 'Footer template', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_none', 'zh-CN', '（不绑定，用上面的页眉块）', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_none', 'en-US', '(Not bound — use the header block above)', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_none_footer', 'zh-CN', '（不绑定，用上面的页脚块）', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_none_footer', 'en-US', '(Not bound — use the footer block above)', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_hint', 'zh-CN', '结构模板在构建时优先于上面的块；模板未绑定或解析失败时回退到块，页眉不会因此消失。同一类型只允许一套生效，「设为生效」在内容模板列表页。', 'admin', '', 1, 200, now(), now()),
	('admin.theme_settings.structure_hint', 'en-US', 'Structure templates take precedence over the blocks above at build time; when a template is unbound or fails to parse the build falls back to the block, so the header never disappears. Only one template per type takes effect — use "Activate" on the content templates page.', 'admin', '', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
