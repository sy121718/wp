-- 312 · i18n 词条 seed（商品标签**跨工程**引用拒绝，1 key × 2 语言 = 2 行）
--
-- 背景：审计 DB-03 §2.4 / §5.1 第 2 条（PROD-02）—— 标签删除与分类 / 品牌 / 属性不同：
--       本工程内的引用是主动解绑（补偿式清理），而**别的工程**仍引用着它时，旧实现会删除成功
--       并在那些商品上留下永久悬空 tag id。现在跨工程引用一律打回给人，错误用新常量
--       ErrTagCrossProject：文案说明规则，具体的引用面、涉及工程与商品 id 在错误的 tail 里
--       （productErrText 取 key 译文后把 tail 拼在后面）。
-- 为什么必须 seed：与 239 / 298 / 304 等所有 product enums 常量同一口径 —— 常量值就是
--       sys_i18n 的 item_key；缺词条不会报任何错，但英文界面会回落 productErrFallbacks 的中文兜底。
--       文案与 productErrFallbacks[ErrTagCrossProject] **逐字一致**（兜底与 seed 不许分叉）。
-- 幂等：ON CONFLICT DO NOTHING —— seed 是默认值来源，后台是真相来源。
-- 注册：register_product_tag_cross_project_i18n.go（registerSeed），幂等判定的 key 写进 SQL 字面量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrTagCrossProject', 'zh-CN', '标签仍被其它工程的商品引用，不能删除', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrTagCrossProject', 'en-US', 'The tag is still referenced by products in other projects and cannot be deleted', 400, 'product', 'internal/module/product/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
