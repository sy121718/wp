-- 660 · 商品表单「仓库明细」面板（B8）的文案词条。
--
--   商品编辑页的每个变体下多了一块按仓分组的仓库明细：组头是仓库名 + 仓码，组内每行
--   一条仓库货行（缩略图 / 名称 / 编码 / 在库数量目标值 / 移除），组外一个「选货 → 添加」
--   入口。本批只 seed 这一块新增的 10 个 key（中英各一行，共 20 行）。
--
--   数量口径写在面板模板头注释里：填的是**目标值（校准）**，不是增量 —— 编辑态显示当前
--   在库，改了就是校准成它，不动就是什么都不写。所以 qtyLabel 明确标出「目标值」，
--   避免运营按「加多少」理解。
--
--   文案已经解释了三个会被服务端拒绝的输入（同仓同 SKU 重复、负数、不跟踪行带数量），
--   所以界面上的报错只需要说「哪里不对」，不必再重复规则。
--
-- 幂等：全部是新增 key，走 DO NOTHING 语义 —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.stocks.toggle', 'zh-CN', '仓库明细', 200, 'product', 'internal/templates/admin/product/product_edit.html', 1, now(), now()),
('admin.products.stocks.toggle', 'en-US', 'Warehouse details', 200, 'product', 'internal/templates/admin/product/product_edit.html', 1, now(), now()),
('admin.products.stocks.qtyLabel', 'zh-CN', '该仓在库数量（目标值）', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.qtyLabel', 'en-US', 'Stock in this warehouse (target)', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.untrackedHint', 'zh-CN', '这一行不跟踪库存（无限）。要填具体数量，请先到库存页把这一行切成跟踪。', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.untrackedHint', 'en-US', 'This row does not track stock (unlimited). To set a specific quantity, switch this row to tracked on the inventory page first.', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.remove', 'zh-CN', '移除这条货', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.remove', 'en-US', 'Remove this stock item', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.duplicated', 'zh-CN', '这个仓里的这条货已经挂上了：直接改它的数量即可，不用再加一行。', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.duplicated', 'en-US', 'This stock item is already attached: change its quantity instead of adding another row.', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.empty', 'zh-CN', '这个变体还没有挂任何仓库货行。', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.empty', 'en-US', 'This variant has no warehouse stock items yet.', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.add', 'zh-CN', '添加', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.add', 'en-US', 'Add', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.addLabel', 'zh-CN', '选择要挂到本变体的仓库货品', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.addLabel', 'en-US', 'Pick a warehouse stock item to attach to this variant', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.addPlaceholder', 'zh-CN', '选一条仓库里的货…', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.addPlaceholder', 'en-US', 'Pick a stock item…', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.unlimited', 'zh-CN', '无限', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('admin.products.stocks.unlimited', 'en-US', 'Unlimited', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
