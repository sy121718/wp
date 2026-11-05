-- 当前词条回补源；历史原文 468_product_comment_policy_i18n.sql 保持归档，不再执行。
-- 468 · 「商品评论必须买过」的拒绝文案词条（BIZ-5 差异化规则）。
--
-- 背景：comment 模块的 EntityPolicy 允许消费方（拥有该实体的模块）自带拒绝理由，
-- 经 commentcontract.PolicyDenial 的 Message（i18n key）+ Fallback（中文原文）传出。
-- 本批 product 实现了该端口（internal/module/product/service/product_comment_policy.go），
-- 它给出的 key 必须在本仓 sys_i18n 里存在 —— 否则英文界面下会回落中文兜底：
-- 界面不报错，但等于这条规则在英文站只有一半。
--
-- 取值来源（真源，不是这里现编的字面量）：
--   · `product.err.commentPurchaseRequired` —— 真源是
--     internal/module/product/enums/product_enums.go 的 ErrCommentPurchaseRequired
--    （常量值即 item_key），中文兜底原文在 product_comment_policy.go 的 PolicyDenial.Fallback。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING。门槛判据在
-- register_product_comment_policy_i18n.go 里**逐条枚举本批 1 个 item_key**（上界封闭，2 行）：
-- 不用 LIKE 前缀（别的批次已有同前缀行时计数虚高 → 本批被静默跳过，058 的真实故障），
-- 也不用全库总量（将来新增同前缀 key 时永远追不平 → 每次启动重跑，076 的真实故障）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pReviewBuyReq', 'zh-CN', '购买过该商品才能发表评论', 400, 'error', 'internal/module/product/service/product_comment_policy.go', 1, now(), now()),
('pReviewBuyReq', 'en-US', 'Only customers who have purchased this product can post a review.', 400, 'error', 'internal/module/product/service/product_comment_policy.go', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pReviewBuyReq', 'product.err.commentPurchaseRequired')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
