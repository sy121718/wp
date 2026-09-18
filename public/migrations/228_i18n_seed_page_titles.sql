-- 228 · i18n 词条 seed（后台页面标题 Msg*Title，补 4 条缺失）
--
-- 背景：shell.Prepare 对渲染数据里的 title 做 t(title, title) —— 词条命中显示译文，
--       未命中**回退字面量**。于是当 handler 传的是 i18n key（Msg*Title）而词条缺失时，
--       顶栏与 <title> 会把 key 原样显示出来（形如 "MsgMasterDataChangesTitle — go_wp 管理后台"）。
--       2026-09 后台页面设计评审（39 个页面全量实测）发现 4 个页面命中该缺陷：
--         /admin/content-templates、/admin/inventory/sources、
--         /admin/inventory/purchases、/admin/masterdata/changes
-- 覆盖：4 个 key / zh-CN 4 行 / en-US 4 行（人工编写，非机器伪造）。
-- 来源：internal/module/{contenttemplate,product/inventory,masterdata}/inbound/http/*.go 的 "title"。
-- 命名：沿用既有 Msg*Title 族（与 MsgDashboardTitle / MsgProductsTitle 同形）。
-- 语义：ON CONFLICT DO NOTHING —— seed 是**默认值来源**，不是真相来源；
--       运营在后台改过的词条不会被下一次部署静默回滚。
-- 幂等：注册见 register_i18n_layer.go，ConditionSQL 以这 4 个 key 的 zh-CN 行数为门槛。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('MsgContentTemplatesTitle', 'en-US', 'Content Templates', 200, 'ui', 'contenttemplate: /admin/content-templates 页标题', 1, now(), now()),
('MsgContentTemplatesTitle', 'zh-CN', '内容模板', 200, 'ui', 'contenttemplate: /admin/content-templates 页标题', 1, now(), now()),
('MsgInventorySourcesTitle', 'en-US', 'Sources', 200, 'ui', 'product/inventory: /admin/inventory/sources 页标题', 1, now(), now()),
('MsgInventorySourcesTitle', 'zh-CN', '货源管理', 200, 'ui', 'product/inventory: /admin/inventory/sources 页标题', 1, now(), now()),
('MsgInventoryPurchasesTitle', 'en-US', 'Purchase Receiving', 200, 'ui', 'product/inventory: /admin/inventory/purchases 页标题', 1, now(), now()),
('MsgInventoryPurchasesTitle', 'zh-CN', '采购入库', 200, 'ui', 'product/inventory: /admin/inventory/purchases 页标题', 1, now(), now()),
('MsgMasterDataChangesTitle', 'en-US', 'Change Log', 200, 'ui', 'masterdata: /admin/masterdata/changes 页标题', 1, now(), now()),
('MsgMasterDataChangesTitle', 'zh-CN', '变更记录', 200, 'ui', 'masterdata: /admin/masterdata/changes 页标题', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
