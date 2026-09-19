-- 264 · 商品侧第一批（仓库侧裸码 / 多仓与认领复用 / 无限库存 / 主体 SKU 唯一性预检）的词条。
--
-- 本批新增的 enums 常量值就是 i18n key，真文案在这张表里；缺词条的后果是页面上原样显示
-- ErrContainerSKUTaken 这种裸 key（既不中文也不是话），所以新增常量必须同批 seed。
--
-- 两类：
--   · 一条业务错误（ErrContainerSKUTaken）—— 主体 SKU 在工程内被别的商品占用（迁移 246 的
--     偏唯一索引）。预检与唯一索引兜底都映射到它，错误里绝不含索引名 / SQLSTATE（CQ-009）；
--   · 商品列表与新建抽屉的新文案位 —— 库存三态（∞ / 数字 / 未入库 + 展开分仓）、
--     SKU 输入框的「认领 / 新建」两种预览、多仓勾选与数量（不填 = 无限）的说明。
--     中文站点有模板兜底看起来正常，英文站点会退回中文 —— 真源是本表，所以一并补上。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
-- 注：admin.products.col.stock 已由迁移 191 seed（本次复用，不重复登记）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrContainerSKUTaken', 'zh-CN', '该主体 SKU 在本工程已被其它商品占用（主体编码在工程内唯一）', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrContainerSKUTaken', 'en-US', 'This container SKU is already used by another product in this project (the container code is unique per project)', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('admin.products.stock.infinite', 'zh-CN', '∞ 无限', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.stock.infinite', 'en-US', '∞ Unlimited', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.stock.infiniteShort', 'zh-CN', '∞', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.stock.infiniteShort', 'en-US', '∞', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.stock.none', 'zh-CN', '未入库', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.stock.none', 'en-US', 'Not stocked yet', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.stock.expand', 'zh-CN', '展开分仓库存', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.stock.expand', 'en-US', 'Expand per-warehouse stock', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.reuseLabel', 'zh-CN', '该仓已有这条编码，将复用那一行（不新建）：', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.reuseLabel', 'en-US', 'This code already exists in the warehouse; that row will be reused (nothing new created): ', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.newLabel', 'zh-CN', '新编码，将在勾选的每个仓各建一行：', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.sku.newLabel', 'en-US', 'New code; one row per selected warehouse: ', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.warehouses.label', 'zh-CN', '建在哪些仓（可多选，不勾即默认仓）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.warehouses.label', 'en-US', 'Warehouses to stock (multi-select; none = default warehouse)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.warehouses.hint', 'zh-CN', '按本表顺序第一个勾中的仓是认领仓（决定主体 SKU 的仓码前缀）；仓库里已有同编码的行会复用、不新建。', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.warehouses.hint', 'en-US', 'The first checked warehouse in this list is the claiming one (it sets the container SKU prefix); an existing row with the same code is reused, not duplicated.', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.quantity.track', 'zh-CN', '跟踪数量（不勾 = 无限）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.quantity.track', 'en-US', 'Track quantity (unchecked = unlimited)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.quantity', 'zh-CN', '不跟踪时留空（无限）；跟踪时填数量，0 = 没货', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.quantity', 'en-US', 'Leave empty when untracked (unlimited); enter a number when tracked, 0 = out of stock', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.quantity.hint', 'zh-CN', '不填数量 = 不跟踪 = 无限；显式填数字 = 跟踪并写入该数量（0 是合法的明确值）。', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.quantity.hint', 'en-US', 'No quantity means untracked (unlimited); an explicit number means tracked and written as-is (0 is a valid, explicit value).', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
