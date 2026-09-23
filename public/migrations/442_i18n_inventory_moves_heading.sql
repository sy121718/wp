-- The empty inventory movement list uses a count-free heading in both languages.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.moves.title', 'zh-CN', '库存流水', 200, 'admin', 'admin/inventory/inventory.html: empty movement list heading', 1, now(), now()),
('admin.inventory.moves.title', 'en-US', 'Stock ledger', 200, 'admin', 'admin/inventory/inventory.html: empty movement list heading', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
