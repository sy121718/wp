-- 229 · i18n 词条 seed（后台页壳改造配套，补 13 条缺失）
--
-- 背景：后台页面设计评审（docs/02-H-admin-page-shell.md）的逐页改造引入了几个新的
--       固定文案位：页头的领域说明按钮（.help-btn 的 aria-label）、列表空状态
--       （标题 + 一句话）、操作列表头、筛选行的「重置」、页头的工程选择标签、
--       折叠区的说明与计数（.fold-note）。
--       模板里 t() 的兜底能显示中文，但**英文界面会回落中文** —— 词条必须成对 seed
--       （门禁：internal/templates/admin_group_f_i18n_test.go 的双向校验，
--        模板用到的 key 必须中英成对；未 seed 的 key 会让该测试直接失败）。
-- 覆盖：13 个 key / zh-CN 13 行 / en-US 13 行（人工编写，非机器伪造）。
-- 来源：internal/templates/admin/{i18n,returns,product_pricing,customers}.html（本批改造的 4 页）。
-- 注意：模板里的 fallback 与这里的 item_value 必须逐字一致 —— 词条命中时页面显示的是
--       本表的值，两者不同会让同一个页面在中英两种语言下读起来是两句话。
-- 语义：ON CONFLICT DO NOTHING —— seed 是**默认值来源**，不是真相来源；
--       运营在后台改过的词条不会被下一次部署静默回滚。
-- 幂等：注册见 register_i18n_layer.go，ConditionSQL 以这 13 个 key 的 zh-CN 行数为门槛。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.common.action.reset', 'en-US', 'Reset', 200, 'admin', '通用动作：筛选重置', 1, now(), now()),
('admin.common.action.reset', 'zh-CN', '重置', 200, 'admin', '通用动作：筛选重置', 1, now(), now()),
('admin.common.field.project', 'en-US', 'Site project', 200, 'admin', '通用字段：页头的站点工程选择标签', 1, now(), now()),
('admin.common.field.project', 'zh-CN', '站点工程', 200, 'admin', '通用字段：页头的站点工程选择标签', 1, now(), now()),
('admin.customers.help.label', 'en-US', 'Show help', 200, 'admin', 'admin/customers.html: 页面说明按钮的无障碍标签', 1, now(), now()),
('admin.customers.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/customers.html: 页面说明按钮的无障碍标签', 1, now(), now()),
('admin.customers.list.empty_heading', 'en-US', 'No matching customers', 200, 'admin', 'admin/customers.html: 客户列表空状态标题', 1, now(), now()),
('admin.customers.list.empty_heading', 'zh-CN', '没有符合条件的客户', 200, 'admin', 'admin/customers.html: 客户列表空状态标题', 1, now(), now()),
('admin.i18n.col.actions', 'en-US', 'Actions', 200, 'admin', 'admin/i18n.html: 操作列表头', 1, now(), now()),
('admin.i18n.col.actions', 'zh-CN', '操作', 200, 'admin', 'admin/i18n.html: 操作列表头', 1, now(), now()),
('admin.i18n.help.label', 'en-US', 'Show help', 200, 'admin', 'admin/i18n.html: 页面说明按钮的无障碍标签', 1, now(), now()),
('admin.i18n.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/i18n.html: 页面说明按钮的无障碍标签', 1, now(), now()),
('admin.i18n.list.empty_desc', 'en-US', 'Try another keyword, or relax the language / category filter. If the table really is empty, use the "New" button in the header to add the first entry.', 200, 'admin', 'admin/i18n.html: 词条列表空状态说明', 1, now(), now()),
('admin.i18n.list.empty_desc', 'zh-CN', '换一个关键词或放宽语言 / 分类筛选；确实一条都没有的话，用右上角「新建」补第一条。', 200, 'admin', 'admin/i18n.html: 词条列表空状态说明', 1, now(), now()),
('admin.i18n.list.empty_title', 'en-US', 'No matching entries', 200, 'admin', 'admin/i18n.html: 词条列表空状态标题', 1, now(), now()),
('admin.i18n.list.empty_title', 'zh-CN', '没有匹配的词条', 200, 'admin', 'admin/i18n.html: 词条列表空状态标题', 1, now(), now()),
('admin.product_pricing.help.label', 'en-US', 'Show help', 200, 'admin', 'admin/product_pricing.html: 页面说明按钮的无障碍标签', 1, now(), now()),
('admin.product_pricing.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/product_pricing.html: 页面说明按钮的无障碍标签', 1, now(), now()),
('admin.product_pricing.history.note', 'en-US', 'Total ', 200, 'admin', 'admin/product_pricing.html: 调价留痕折叠区计数前缀', 1, now(), now()),
('admin.product_pricing.history.note', 'zh-CN', '共 ', 200, 'admin', 'admin/product_pricing.html: 调价留痕折叠区计数前缀', 1, now(), now()),
('admin.product_pricing.history.noteTail', 'en-US', ' batches (per-variant old → new)', 200, 'admin', 'admin/product_pricing.html: 调价留痕折叠区计数后缀', 1, now(), now()),
('admin.product_pricing.history.noteTail', 'zh-CN', ' 批（逐变体「原价 → 新价」）', 200, 'admin', 'admin/product_pricing.html: 调价留痕折叠区计数后缀', 1, now(), now()),
('admin.product_pricing.rules.note', 'en-US', 'Four rules and the parameters each one takes (read-only)', 200, 'admin', 'admin/product_pricing.html: 内置定价规则折叠区说明', 1, now(), now()),
('admin.product_pricing.rules.note', 'zh-CN', '四种规则与各自要填的参数（只读）', 200, 'admin', 'admin/product_pricing.html: 内置定价规则折叠区说明', 1, now(), now()),
('admin.returns.help.label', 'en-US', 'Show help', 200, 'admin', 'admin/returns.html: 页面说明按钮的无障碍标签', 1, now(), now()),
('admin.returns.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/returns.html: 页面说明按钮的无障碍标签', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
