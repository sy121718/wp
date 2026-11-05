-- 商品批量拒绝与变体库存重复两条主错误的词条（此前只有 key，没有 seed）。
--
-- 背景：错误声明已改成 key + 状态码，文案统一由词库在 JSON 出口取词。这两个 key
-- 在商品相关迁移里从未 seed，缺词时出口只能退化成共享内部提示，运营看不到
-- 「这批操作被拒」或「哪一行库存重复」。这里补 zh-CN / en-US 两侧。
--
-- 只补词条，不动状态码与调用点：状态仍由 enums.StatusFor 决定
-- （ErrBulkRejected = 409；ErrVariantStockDuplicated 未登记 → 出口按 400）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— 运营在后台改过的文案保留。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('ErrBulkRejected', 'zh-CN', '批量操作被拒绝，请按列出的原因逐条处理后重试', 409, 'product', 'product blocked errors', 1, now(), now()),
    ('ErrBulkRejected', 'en-US', 'The batch operation was rejected. Fix the listed items and try again.', 409, 'product', 'product blocked errors', 1, now(), now()),
    ('ErrVariantStockDuplicated', 'zh-CN', '库存清单里同一仓库同一 SKU 出现了多行，请合并后再保存', 400, 'product', 'product blocked errors', 1, now(), now()),
    ('ErrVariantStockDuplicated', 'en-US', 'The same warehouse and SKU appear more than once in the stock list. Merge those rows and save again.', 400, 'product', 'product blocked errors', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
