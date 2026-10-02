-- 497 · 系统设置页：新增 trade 配置组（全局默认国家 / 货币）。
--
-- 背景：sys_config 此前只有 i18n 组（迁移 490），而后台没有任何编辑入口 —— 改全局默认值
--       只能改库。本批补上「系统设置」页（/admin/system），它要编辑两组，其中 trade 组
--       此前不存在。
--
-- 为什么组必须由迁移建立：SetGroup 是**只为已存在的组**做整组覆盖（0 行时归因为
--   「分组不存在 / 版本冲突」），后台保存不凭空建组 —— 组的存在与否是部署资产，
--   由迁移决定，不由某次点击决定。
--
-- 默认值取 CN / CNY：两者都在 sys_area（kind='country'）与 sys_dict（type='currency'）
--   的启用项里。它们的作用是**全局默认 + 兜底**（还没有读取方），改成别的值随时可以。
--
-- 幂等：WHERE / ON CONFLICT DO NOTHING；本批只新增这一组，不改 i18n 组。
INSERT INTO sys_config (group_key, group_name, config_data, remark, status, version, create_by, create_time, update_by, update_time)
VALUES ('trade', '交易默认值', '{"default_country":"CN","default_currency":"CNY"}'::jsonb,
        '全局默认国家 / 货币（兜底；工程级覆盖走 projects.settings）', 1, 1, 0, now(), 0, now())
ON CONFLICT (group_key) DO NOTHING;

-- 本页文案词条（中英各 12 条）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.system.title', 'zh-CN', '系统设置', 200, 'admin', '系统设置页标题', 1, now(), now()),
('admin.system.title', 'en-US', 'System settings', 200, 'admin', 'system settings page title', 1, now(), now()),
('admin.system.help.label', 'zh-CN', '查看说明', 200, 'admin', '帮助按钮无障碍标签', 1, now(), now()),
('admin.system.help.label', 'en-US', 'Show help', 200, 'admin', 'help button accessible label', 1, now(), now()),
('admin.system.help.title', 'zh-CN', '这一页管什么', 200, 'admin', '帮助浮层标题', 1, now(), now()),
('admin.system.help.title', 'en-US', 'What this page controls', 200, 'admin', 'help popover title', 1, now(), now()),
('admin.system.help.global', 'zh-CN', '这里改的是**全局默认值**：站点没有自己的工程级设置时才会用到。', 200, 'admin', '帮助：全局默认口径', 1, now(), now()),
('admin.system.help.global', 'en-US', 'This page edits **global defaults**: they apply when a site has no project-level setting of its own.', 200, 'admin', 'help: global default semantics', 1, now(), now()),
('admin.system.help.trade', 'zh-CN', '默认国家与默认货币目前只是全局默认 + 兜底，还没有读取方 —— 设了不会立刻改变某个表单的初始值（工程级覆盖仍走站点设置页）。', 200, 'admin', '帮助：trade 组暂无读取方', 1, now(), now()),
('admin.system.help.trade', 'en-US', 'Default country and currency are global defaults + fallback only; nothing reads them yet, so setting them does not immediately change any form (project-level overrides still live on the site settings page).', 200, 'admin', 'help: trade group has no reader yet', 1, now(), now()),
('admin.system.help.shortcode', 'zh-CN', '语言短码覆盖表（lang_url_codes）属于专家项，不在本页编辑，保存时原样保留。', 200, 'admin', '帮助：未暴露键保存时保留', 1, now(), now()),
('admin.system.help.shortcode', 'en-US', 'The language short-code override table (lang_url_codes) is an expert setting: it is not editable here and is preserved as-is when you save.', 200, 'admin', 'help: unexposed keys are preserved on save', 1, now(), now()),
('admin.system.section.locale', 'zh-CN', '语言', 200, 'admin', '分组标题：语言', 1, now(), now()),
('admin.system.section.locale', 'en-US', 'Language', 200, 'admin', 'section: language', 1, now(), now()),
('admin.system.section.trade', 'zh-CN', '交易默认值', 200, 'admin', '分组标题：交易默认值', 1, now(), now()),
('admin.system.section.trade', 'en-US', 'Trade defaults', 200, 'admin', 'section: trade defaults', 1, now(), now()),
('admin.system.field.default_lang', 'zh-CN', '默认语言', 200, 'admin', '字段：默认语言', 1, now(), now()),
('admin.system.field.default_lang', 'en-US', 'Default language', 200, 'admin', 'field: default language', 1, now(), now()),
('admin.system.field.site_lang_url_mode', 'zh-CN', '语言 URL 方案', 200, 'admin', '字段：语言 URL 方案', 1, now(), now()),
('admin.system.field.site_lang_url_mode', 'en-US', 'Language URL scheme', 200, 'admin', 'field: language URL scheme', 1, now(), now()),
('admin.system.field.default_country', 'zh-CN', '默认国家', 200, 'admin', '字段：默认国家', 1, now(), now()),
('admin.system.field.default_country', 'en-US', 'Default country', 200, 'admin', 'field: default country', 1, now(), now()),
('admin.system.field.default_currency', 'zh-CN', '默认货币', 200, 'admin', '字段：默认货币', 1, now(), now()),
('admin.system.field.default_currency', 'en-US', 'Default currency', 200, 'admin', 'field: default currency', 1, now(), now()),
('admin.system.hint.default_lang', 'zh-CN', '站点默认语言：进产物字节（<html lang>、hreflang 的 x-default）。改后需重建页面才反映到访问面。', 200, 'admin', '提示：默认语言', 1, now(), now()),
('admin.system.hint.default_lang', 'en-US', 'Site default language: it goes into the artifact bytes (<html lang>, hreflang x-default). Rebuild pages for it to reach the live site.', 200, 'admin', 'hint: default language', 1, now(), now()),
('admin.system.hint.site_lang_url_mode', 'zh-CN', 'default_plain：默认语言不带前缀；all_prefix：所有语言都带前缀；off：不做语言 URL 分离。工程可在站点设置里覆盖。', 200, 'admin', '提示：语言 URL 方案', 1, now(), now()),
('admin.system.hint.site_lang_url_mode', 'en-US', 'default_plain: the default language has no prefix; all_prefix: every language has one; off: no language URL separation. A project can override this in site settings.', 200, 'admin', 'hint: language URL scheme', 1, now(), now()),
('admin.system.hint.trade', 'zh-CN', '默认国家与默认货币是全局默认 + 兜底：目前还没有读取方（工程级覆盖仍走站点设置页），设置后不会立刻改变某个表单的初始值。', 200, 'admin', '提示：trade 组语义', 1, now(), now()),
('admin.system.hint.trade', 'en-US', 'Default country and currency are global defaults + fallback: nothing reads them yet (project-level overrides still live on the site settings page), so saving them does not immediately change any form.', 200, 'admin', 'hint: trade group semantics', 1, now(), now()),
('admin.system.submit', 'zh-CN', '保存', 200, 'admin', '保存按钮', 1, now(), now()),
('admin.system.submit', 'en-US', 'Save', 200, 'admin', 'save button', 1, now(), now()),
('admin.system.saved', 'zh-CN', '系统设置已保存（全局默认值立即对读取方生效）', 200, 'admin', '保存成功回执', 1, now(), now()),
('admin.system.saved', 'en-US', 'System settings saved (global defaults take effect for readers immediately)', 200, 'admin', 'save success receipt', 1, now(), now()),
('admin.system.group_missing', 'zh-CN', '配置分组缺失，请先执行数据库迁移（make migrate）后再打开本页', 200, 'admin', '分组缺失提示', 1, now(), now()),
('admin.system.group_missing', 'en-US', 'A config group is missing. Run the database migrations (make migrate) and reopen this page.', 200, 'admin', 'missing group hint', 1, now(), now()),
('admin.system.err.lang_unknown', 'zh-CN', '选择的默认语言不在字典里（可能已被停用），请重新选择', 200, 'admin', '语言不在字典', 1, now(), now()),
('admin.system.err.lang_unknown', 'en-US', 'The selected default language is not in the dictionary (it may be disabled). Pick another one.', 200, 'admin', 'language not in dictionary', 1, now(), now()),
('admin.system.err.currency_unknown', 'zh-CN', '选择的默认货币不在字典里（可能已被停用），请重新选择', 200, 'admin', '货币不在字典', 1, now(), now()),
('admin.system.err.currency_unknown', 'en-US', 'The selected default currency is not in the dictionary (it may be disabled). Pick another one.', 200, 'admin', 'currency not in dictionary', 1, now(), now()),
('admin.system.err.country_unknown', 'zh-CN', '选择的默认国家不在字典里（可能已被停用），请重新选择', 200, 'admin', '国家不在字典', 1, now(), now()),
('admin.system.err.country_unknown', 'en-US', 'The selected default country is not in the dictionary (it may be disabled). Pick another one.', 200, 'admin', 'country not in dictionary', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
