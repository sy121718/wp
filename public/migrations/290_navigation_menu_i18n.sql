-- 290 · 导航菜单页与工作台检查器的补漏词条（中英各一行）。
--
-- 三组，都是「模板里已有 t(key, 中文兜底)、但 sys_i18n 里没有对应行」的文案：
--
--   1) 导航位置名（移动端两个）与悬浮面板区（超级菜单，迁移 285 落地时只加了模板与列，
--      忘记同批 seed）：不 seed 时英文界面上原样显示中文兜底，中文界面看着「正常」。
--   2) 乐观锁冲突（本批新增）：service 返回白名单 key + 「：<菜单项标题>」的定位信息
--      （navigation/service 的 staleVersionMessage）。页面上显示的是词条原文 + 定位 ——
--      缺词条时运营看到的是裸 key「ErrStaleVersion：产品」，比通用提示更糟。
--   3) 工作台检查器的「就地新建菜单项」（本批新增的 /workbench/navigation/create）：
--      检查器片段由服务端渲染（fragments/inspector_panel.html），文案走同一套取词。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
-- 判据（register 侧）按本批自己的 key 计数，不用全库行数：用总量会被同期其它批次的行
-- 满足而静默跳过（060 / 221 / 226 / 269 都记过这个坑）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    -- 1) 导航位置（移动端）与悬浮面板区
    ('admin.navigations.kind.headerMobile', 'zh-CN', '页眉导航（移动端）', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.kind.headerMobile', 'en-US', 'Header navigation (mobile)', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.kind.footerMobile', 'zh-CN', '页脚导航（移动端）', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.kind.footerMobile', 'en-US', 'Footer navigation (mobile)', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.label', 'zh-CN', '悬浮面板（超级菜单）', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.label', 'en-US', 'Hover panel (mega menu)', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.none', 'zh-CN', '（无面板）', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.none', 'en-US', '(no panel)', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.width', 'zh-CN', '面板宽度', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.width', 'en-US', 'Panel width', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.widthAuto', 'zh-CN', '宽度：跟随内容', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.widthAuto', 'en-US', 'Width: fit content', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.widthFull', 'zh-CN', '宽度：通栏', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.widthFull', 'en-US', 'Width: full bleed', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.save', 'zh-CN', '保存面板', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.save', 'en-US', 'Save panel', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.create', 'zh-CN', '新建面板块并编辑', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.create', 'en-US', 'Create a panel block and edit it', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.preview', 'zh-CN', '预览面板', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.preview', 'en-US', 'Preview panel', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.previewClose', 'zh-CN', '关闭预览', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.panel.previewClose', 'en-US', 'Close preview', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.bulk_delete_confirm', 'zh-CN', '删除选中的菜单项？它们的子菜单项会一并删除，其余照常删除。', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.bulk_delete_confirm', 'en-US', 'Delete the selected menu items? Their child items are deleted as well; the rest are processed normally.', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.last_error', 'zh-CN', '上一次操作未完成：', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),
    ('admin.navigations.last_error', 'en-US', 'The previous operation did not complete: ', 200, 'admin', 'internal/templates/admin/navigations.html', 1, now(), now()),

    -- 2) 乐观锁冲突（业务错误：值就是 enums 常量，形态与其它 Err* 一致）
    ('ErrStaleVersion', 'zh-CN', '该菜单项已被其他窗口修改，请刷新后重试', 409, 'error', 'internal/module/navigation/enums/navigation_enums.go', 1, now(), now()),
    ('ErrStaleVersion', 'en-US', 'This menu item was changed in another window; refresh and try again.', 409, 'error', 'internal/module/navigation/enums/navigation_enums.go', 1, now(), now()),

    -- 3) 工作台检查器：就地新建菜单项
    ('workbench.nav.new', 'zh-CN', '新建菜单项', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.new', 'en-US', 'New menu item', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.title', 'zh-CN', '菜单文字，如：关于我们', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.title', 'en-US', 'Menu label, e.g. About us', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.path', 'zh-CN', '链接，如 /about', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.path', 'en-US', 'Link, e.g. /about', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.kind', 'zh-CN', '导航位置', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.kind', 'en-US', 'Navigation position', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.create', 'zh-CN', '创建并选用', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now()),
    ('workbench.nav.create', 'en-US', 'Create and use', 200, 'admin', 'internal/templates/fragments/inspector_panel.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
