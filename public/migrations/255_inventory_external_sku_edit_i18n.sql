-- 255 · 库存页「外部编码」行内编辑（可编辑入口）的模板文案位。
--
-- 背景：迁移 251 给 inventory_stocks 加了 external_sku，但库存页当时只有**只读列** ——
-- 早于 251 建的老商品事后没有地方登记外码。本批给那一列加上行内编辑（原生表单 POST +
-- PRG 回列表），随之产生 3 个用户可见文案位。业务错误的词条（ErrExternalSKUInvalid /
-- ErrExternalSKUProductConflict）在 252 已经 seed，本批不重复。
--
-- 为什么必须 seed：中文站点靠模板兜底看起来正常，英文站点会退回中文 —— 真源是这张表。
-- 缺词条的另一种后果是页面上出现 admin.inventory.sku.externalSku.save 这种裸 key。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.sku.externalSku.ph', 'zh-CN', '留空 = 该仓用我们的 SKU', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.sku.externalSku.ph', 'en-US', 'Empty = use our own SKU in this warehouse', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.sku.externalSku.save', 'zh-CN', '保存', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.sku.externalSku.save', 'en-US', 'Save', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.sku.externalSku.actionHint', 'zh-CN', '登记这条货在该仓的外部编码；留空保存即撤销映射（改用我们自己的 SKU）', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.sku.externalSku.actionHint', 'en-US', 'Register the external code of this stock row in this warehouse; saving it empty removes the mapping (back to our own SKU)', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
