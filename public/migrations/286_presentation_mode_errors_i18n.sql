-- 286 · 商品页双轨（迁移 282）的 4 条 presentation 业务错误文案（中英各一行）。
--
-- 背景：这四条是双轨写动作的可行动差异 ——「会放弃模板同步，需确认」「回滚目标不在位」
-- 「回滚失败，线上未变」「这个版本不属于该商品」。它们经 ?err= 回带商品详情页，
-- 商品侧的 productErrText 已经按 tr(key, productErrFallbacks[key]) 取词（product_page_handle.go），
-- 所以**常量值改成 i18n key + seed 词条即接通读写两侧**；不 seed 时页面上原样显示裸 key。
--
-- 值带模块前缀是刻意的：sys_i18n 主键是 (item_key, lang)，而 page 模块已有一个同名的
-- ErrRollbackTargetMiss 哨兵 —— 裸 key 会让两条不同来源的错误互相顶掉词条。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('presentation.err.detachConfirmRequired', 'zh-CN', '该操作会放弃模板同步（商品页转为独立文档），需要先确认', 409, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now()),
    ('presentation.err.detachConfirmRequired', 'en-US', 'This will detach the page from its template (the product gets its own document); confirmation is required.', 409, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now()),
    ('presentation.err.rollbackTargetMiss', 'zh-CN', '回滚目标不存在：该版本不属于这个商品，或产物文件已缺失', 400, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now()),
    ('presentation.err.rollbackTargetMiss', 'en-US', 'Rollback target not found: that version does not belong to this product, or its artifact file is missing.', 400, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now()),
    ('presentation.err.rollbackFailed', 'zh-CN', '回滚失败：线上版本保持不变', 400, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now()),
    ('presentation.err.rollbackFailed', 'en-US', 'Rollback failed; the live version was left unchanged.', 400, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now()),
    ('presentation.err.snapshotMismatch', 'zh-CN', '该版本不属于这个商品，不能用来回滚', 400, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now()),
    ('presentation.err.snapshotMismatch', 'en-US', 'That version does not belong to this product and cannot be used to roll back.', 400, 'error', 'internal/module/presentation/enums/presentation_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
