-- 304 · i18n 词条 seed（商品「相关商品」引用校验，1 key × 2 语言 = 2 行）
--
-- 背景：审计 DB-03 §1.2 / PROD-01 —— products.related_ids 此前是商品域**唯一**没有服务层
--       校验的引用面（分类 / 标签 / 属性都有「存在 + 同工程」）。本次补上「存在 + 同工程 +
--       不能指向自己」，错误用新常量 ErrRelatedInvalid：文案说明规则，具体的非法 id、数量与
--       归属工程在错误的 tail 里（productErrText 取 key 译文后把 tail 拼在后面）。
-- 为什么必须 seed：与 239 等所有 product enums 常量同一口径 —— 常量值就是 sys_i18n 的 item_key；
--       缺词条不会报任何错，但英文界面会回落 productErrFallbacks 的中文兜底。
-- 幂等：ON CONFLICT DO NOTHING —— seed 是默认值来源，后台是真相来源。
-- 注册：register_product_related_i18n.go（registerSeed），幂等判定的 key 写进 SQL 字面量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrRelatedInvalid', 'zh-CN', '相关商品引用不合法：引用的商品必须存在、属于本工程，且不能指向自己', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrRelatedInvalid', 'en-US', 'Invalid related product reference: every related product must exist, belong to this project, and must not be the product itself', 400, 'product', 'internal/module/product/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
