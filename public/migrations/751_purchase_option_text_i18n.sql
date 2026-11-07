-- 751 · 采购选项的固定 UI 片段词条（中英成对）。
--
-- 背景：采购单与生产入库的选项标签原先由 handler 拼成品句（成本说明、默认仓后缀），
-- 按「显示归模板」迁到模板后，这几个固定片段需要在词库里有一行。
-- 业务名（货源名、SKU、仓库名）仍是业务事实，原样传递，不在本批。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING，不覆盖运营改过的译文。
-- 守门：register_purchase_option_text_i18n.go 逐条核对本批 6 对 (item_key, lang)。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory_purchases.option.cost', 'zh-CN', '（成本', 200, 'purchase', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.option.cost', 'en-US', ' (cost', 200, 'purchase', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.option.costTail', 'zh-CN', '）', 200, 'purchase', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.option.costTail', 'en-US', ')', 200, 'purchase', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.option.warehouseDefaultSuffix', 'zh-CN', '· 默认仓', 200, 'purchase', 'internal/templates/admin/purchase/purchases.html', 1, now(), now()),
('admin.inventory_purchases.option.warehouseDefaultSuffix', 'en-US', '· default warehouse', 200, 'purchase', 'internal/templates/admin/purchase/purchases.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
