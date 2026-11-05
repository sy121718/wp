-- 当前词条回补源；历史原文 660_product_variant_stock_panel_i18n.sql 保持归档，不再执行。
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

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pStocksToggle', 'zh-CN', '仓库明细', 200, 'product', 'internal/templates/admin/product/product_edit.html', 1, now(), now()),
('pStocksToggle', 'en-US', 'Warehouse details', 200, 'product', 'internal/templates/admin/product/product_edit.html', 1, now(), now()),
('pStocksQty', 'zh-CN', '该仓在库数量（目标值）', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksQty', 'en-US', 'Stock in this warehouse (target)', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStockTrackHint', 'zh-CN', '这一行不跟踪库存（无限）。要填具体数量，请先到库存页把这一行切成跟踪。', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStockTrackHint', 'en-US', 'This row does not track stock (unlimited). To set a specific quantity, switch this row to tracked on the inventory page first.', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksRemove', 'zh-CN', '移除这条货', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksRemove', 'en-US', 'Remove this stock item', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksDup', 'zh-CN', '这个仓里的这条货已经挂上了：直接改它的数量即可，不用再加一行。', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksDup', 'en-US', 'This stock item is already attached: change its quantity instead of adding another row.', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksEmpty', 'zh-CN', '这个变体还没有挂任何仓库货行。', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksEmpty', 'en-US', 'This variant has no warehouse stock items yet.', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStockAdd', 'zh-CN', '添加', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStockAdd', 'en-US', 'Add', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStockAddLabel', 'zh-CN', '选择要挂到本变体的仓库货品', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStockAddLabel', 'en-US', 'Pick a warehouse stock item to attach to this variant', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksAddPh', 'zh-CN', '选一条仓库里的货…', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksAddPh', 'en-US', 'Pick a stock item…', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksUnlimit', 'zh-CN', '无限', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now()),
('pStocksUnlimit', 'en-US', 'Unlimited', 200, 'product', 'internal/templates/admin/product/variant_stock_panel.html', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pStockAdd', 'admin.products.stocks.add'), ('pStockAddLabel', 'admin.products.stocks.addLabel'), ('pStockTrackHint', 'admin.products.stocks.untrackedHint'), ('pStocksAddPh', 'admin.products.stocks.addPlaceholder'), ('pStocksDup', 'admin.products.stocks.duplicated'), ('pStocksEmpty', 'admin.products.stocks.empty'), ('pStocksQty', 'admin.products.stocks.qtyLabel'), ('pStocksRemove', 'admin.products.stocks.remove'), ('pStocksToggle', 'admin.products.stocks.toggle'), ('pStocksUnlimit', 'admin.products.stocks.unlimited')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
