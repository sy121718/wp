-- 447 · 补齐两类缺失词条（共 17 行）：
--   第一类（13 行，只补 en-US）：058 写下的 Msg* 词条只有 zh-CN 行，
--     英文界面因「指定语言缺失 → 默认语言」的降级链**回落成中文**。
--   第二类（4 行，中英成对）：模板已在取词、但 sys_i18n 与全部迁移里都不存在的 key，
--     取词只能拿到模板里的中文兜底 —— 英文界面同样是中文。
--
-- 第一类只写 en-US 行，**不重写 zh-CN 行**：zh-CN 行由 058 落地（category='ui'、http_code=200）。
--   同一 key 的两语言行取值必须一致（先例 405 / 406 / 430），故本批的 en-US 行沿用该口径：
--   http_code=200、category='ui'。
--
--   remark **不复用 058 的路径** —— 那是 `internal/module/dashboard/enums/dashboard_enums.go`，
--   而 dashboard 模块早已随页面回迁删除（`internal/shell/errors.go` 有注明）。照抄旧路径
--   会把后来人指向不存在的文件，故本批逐个核实了每个 key 的真实定义位置（常量值即 key 名）：
--     · MsgAdministratorsTitle / MsgRolesTitle / MsgMenusTitle / MsgPermissionsTitle /
--       MsgDepartmentsTitle / MsgDatarulesTitle → internal/module/admin/inbound/http/admin_pages_handle.go:39-45
--     · MsgBlocksTitle        → internal/module/block/inbound/http/block_page_handle.go:29
--     · MsgNavigationsTitle   → internal/module/navigation/inbound/http/navigation_page_handle.go:33
--     · MsgThemesTitle        → internal/module/project/inbound/http/theme_admin_pages.go:33
--     · MsgSiteSettingsTitle  → internal/module/project/inbound/http/site_settings_admin_pages.go:36
--     · MsgThemeSettingsTitle → internal/module/project/inbound/http/theme_settings_admin_pages.go:32
--     · MsgAdminGenericFailed / MsgSiteSettingsSaved → Go 侧无任何常量定义（全仓 rg -F 只命中迁移
--       与注释），按「058 遗留词条，Go 侧无引用」登记，不编位置。
--   行号是核实当时的实测值（rg -n 逐个确认），文件改动后会漂移 —— 它是线索不是契约。
--
--   英文按后台页面标题惯例用 Title Case（Administrators / Roles / Permissions / Menus /
--   Departments / Data rules / Global blocks / Navigations / Themes / Theme settings /
--   Site settings / Media library）。
--   MsgAdminGenericFailed 是后台通用失败提示，刻意与同表 ErrInternal 的既有英文
--   （'Operation failed, please try again later'）区分：中文原文多了「请检查输入或联系管理员」。
--   MsgSiteSettingsSaved 沿用既有 "... saved." 回执口径（对照 admin.settings.locales.saved）。
--
-- 第二类两个 key 的 zh-CN 值**逐字照抄模板兜底**（库值存在时覆盖兜底，两边不一致
-- 等于顺手改了文案）：
--   · admin.media.heading           internal/templates/admin/media/media.html:7
--   · admin.article.edit.unavailable  internal/templates/admin/content/article_edit.html:83
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（sys_i18n 主键 (item_key, lang)）；
--   本批只新增行，不修改任何既有词条的 item_value。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('MsgAdminGenericFailed', 'en-US', 'Operation failed, check your input or contact an administrator.', 200, 'ui', '058 遗留词条，Go 侧无引用（本批补 en-US）', 1, now(), now()),
('MsgAdministratorsTitle', 'en-US', 'Administrators', 200, 'ui', 'internal/module/admin/inbound/http/admin_pages_handle.go:39: 管理员页面标题（本批补 en-US）', 1, now(), now()),
('MsgBlocksTitle', 'en-US', 'Global blocks', 200, 'ui', 'internal/module/block/inbound/http/block_page_handle.go:29: 全局块页面标题（本批补 en-US）', 1, now(), now()),
('MsgDatarulesTitle', 'en-US', 'Data rules', 200, 'ui', 'internal/module/admin/inbound/http/admin_pages_handle.go:45: 数据权限页面标题（本批补 en-US）', 1, now(), now()),
('MsgDepartmentsTitle', 'en-US', 'Departments', 200, 'ui', 'internal/module/admin/inbound/http/admin_pages_handle.go:44: 部门管理页面标题（本批补 en-US）', 1, now(), now()),
('MsgMenusTitle', 'en-US', 'Menus', 200, 'ui', 'internal/module/admin/inbound/http/admin_pages_handle.go:42: 菜单管理页面标题（本批补 en-US）', 1, now(), now()),
('MsgNavigationsTitle', 'en-US', 'Navigations', 200, 'ui', 'internal/module/navigation/inbound/http/navigation_page_handle.go:33: 导航菜单页面标题（本批补 en-US）', 1, now(), now()),
('MsgPermissionsTitle', 'en-US', 'Permissions', 200, 'ui', 'internal/module/admin/inbound/http/admin_pages_handle.go:43: 权限资源页面标题（本批补 en-US）', 1, now(), now()),
('MsgRolesTitle', 'en-US', 'Roles', 200, 'ui', 'internal/module/admin/inbound/http/admin_pages_handle.go:40: 角色管理页面标题（本批补 en-US）', 1, now(), now()),
('MsgSiteSettingsSaved', 'en-US', 'Site settings saved.', 200, 'ui', '058 遗留词条，Go 侧无引用（本批补 en-US）', 1, now(), now()),
('MsgSiteSettingsTitle', 'en-US', 'Site settings', 200, 'ui', 'internal/module/project/inbound/http/site_settings_admin_pages.go:36: 站点设置页面标题（本批补 en-US）', 1, now(), now()),
('MsgThemeSettingsTitle', 'en-US', 'Theme settings', 200, 'ui', 'internal/module/project/inbound/http/theme_settings_admin_pages.go:32: 主题设置页面标题（本批补 en-US）', 1, now(), now()),
('MsgThemesTitle', 'en-US', 'Themes', 200, 'ui', 'internal/module/project/inbound/http/theme_admin_pages.go:33: 主题管理页面标题（本批补 en-US）', 1, now(), now()),
('admin.media.heading', 'zh-CN', '媒体库', 200, 'admin', 'admin/media/media.html: 页面标题（模板兜底逐字照抄）', 1, now(), now()),
('admin.media.heading', 'en-US', 'Media library', 200, 'admin', 'admin/media/media.html: 页面标题（模板兜底逐字照抄）', 1, now(), now()),
('admin.article.edit.unavailable', 'zh-CN', '文章信息暂时不可用；以下是本次提交内容，请保留后返回列表重新打开文章。', 200, 'admin', 'admin/content/article_edit.html: 文章信息暂时不可用的提示（模板兜底逐字照抄）', 1, now(), now()),
('admin.article.edit.unavailable', 'en-US', 'Article details are temporarily unavailable; what follows is the content you submitted. Keep it, then go back to the list and reopen the article.', 200, 'admin', 'admin/content/article_edit.html: 文章信息暂时不可用的提示（模板兜底逐字照抄）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
