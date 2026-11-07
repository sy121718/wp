-- 750 · 采购货源选项与导航面板块的展示词条补种（中英成对）。
--
-- 背景：这两处模板按现行规则直接用完整字面量 key + 中文兜底取词
-- （货源状态 / 关联方类型下拉、面板块创建后的结论片段），但对应词条从未落库。
-- 缺词时界面会退到模板兜底，非中文请求就看不到译文，因此补种默认值。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING，不覆盖运营改过的译文。
-- 守门：register_purchase_navigation_short_keys_i18n.go 逐条核对本批 10 对
--       (item_key, lang)，不用全库计数 —— 存量库永远满足，条件一旦写成计数就永不再补。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory_sources.status.active', 'zh-CN', '启用中', 200, 'purchase', 'internal/templates/admin/purchase/sources.html', 1, now(), now()),
('admin.inventory_sources.status.active', 'en-US', 'Active', 200, 'purchase', 'internal/templates/admin/purchase/sources.html', 1, now(), now()),
('admin.inventory_sources.status.disabled', 'zh-CN', '已停用', 200, 'purchase', 'internal/templates/admin/purchase/sources.html', 1, now(), now()),
('admin.inventory_sources.status.disabled', 'en-US', 'Disabled', 200, 'purchase', 'internal/templates/admin/purchase/sources.html', 1, now(), now()),
('admin.inventory_sources.related.typeDefault', 'zh-CN', '按类型默认 / 不改', 200, 'purchase', 'internal/templates/admin/purchase/sources.html', 1, now(), now()),
('admin.inventory_sources.related.typeDefault', 'en-US', 'Use the type default / leave unchanged', 200, 'purchase', 'internal/templates/admin/purchase/sources.html', 1, now(), now()),
('admin.navigations.panel.created', 'zh-CN', '面板块已创建，正在打开编辑器…', 200, 'navigation', 'internal/templates/admin/navigation/panel_created.html', 1, now(), now()),
('admin.navigations.panel.created', 'en-US', 'The panel block was created. Opening the editor…', 200, 'navigation', 'internal/templates/admin/navigation/panel_created.html', 1, now(), now()),
('admin.navigations.panel.open', 'zh-CN', '打开编辑器', 200, 'navigation', 'internal/templates/admin/navigation/panel_created.html', 1, now(), now()),
('admin.navigations.panel.open', 'en-US', 'Open the editor', 200, 'navigation', 'internal/templates/admin/navigation/panel_created.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
