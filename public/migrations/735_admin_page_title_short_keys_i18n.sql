-- 后台系统页标题与项目设置提示的短 key 整改：旧 key 含下划线或超过 15 字符，不合规。
-- 新 key 直接沿用旧译文（中英各一行），旧 key 在新迁移里精确清理。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('dataruleTitle', 'zh-CN', '数据权限', 200, 'ui', 'admin/system/datarules.html', 1, now(), now()),
('dataruleTitle', 'en-US', 'Data rules', 200, 'ui', 'admin/system/datarules.html', 1, now(), now()),
('deptTitle', 'zh-CN', '部门管理', 200, 'ui', 'admin/system/departments.html', 1, now(), now()),
('deptTitle', 'en-US', 'Departments', 200, 'ui', 'admin/system/departments.html', 1, now(), now()),
('adminTitle', 'zh-CN', '管理员', 200, 'ui', 'admin/system/administrators.html', 1, now(), now()),
('adminTitle', 'en-US', 'Administrators', 200, 'ui', 'admin/system/administrators.html', 1, now(), now()),
('permTitle', 'zh-CN', '权限资源', 200, 'ui', 'admin/system/permissions.html', 1, now(), now()),
('permTitle', 'en-US', 'Permissions', 200, 'ui', 'admin/system/permissions.html', 1, now(), now()),
('langUrlOffWarn', 'zh-CN', '当前多语言访问路径方案为 off：各语言映射到同一路径，语言切换器不会渲染；如需多语言独立 URL，请在下方「多语言访问路径方案」切换为 default_plain 或 all_prefix 并保存', 200, 'ui', 'admin/project/settings.html', 1, now(), now()),
('langUrlOffWarn', 'en-US', 'Language URL mode is off: all locales share the same path, so the language switcher will not render. Set i18n.site_lang_url_mode to default_plain or all_prefix for separate locale URLs.', 200, 'ui', 'admin/project/settings.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 旧 key 已无任何消费者（模板与测试均已改到新 key），精确清理。
DELETE FROM sys_i18n
WHERE (item_key, lang) IN (
  ('MsgDatarulesTitle', 'zh-CN'), ('MsgDatarulesTitle', 'en-US'),
  ('MsgDepartmentsTitle', 'zh-CN'), ('MsgDepartmentsTitle', 'en-US'),
  ('MsgAdministratorsTitle', 'zh-CN'), ('MsgAdministratorsTitle', 'en-US'),
  ('MsgPermissionsTitle', 'zh-CN'), ('MsgPermissionsTitle', 'en-US'),
  ('MsgSiteLangURLOffWarning', 'zh-CN'), ('MsgSiteLangURLOffWarning', 'en-US'));
