-- 当前词条回补源；历史原文 620_translation_stale_mark_failed_i18n.sql 保持归档，不再执行。
-- 620 · 译文保存成功但标记待重建失败时的回执词条。
--
-- 背景：翻译工作台保存译文时，写入成功后还要「标记页面待重建」；那一步失败原先只写日志，
-- 用户看到「已保存 N 条译文；译文变更已标记待重建」——而实际上**没标记**，
-- 前台仍是旧译文，也没有重试入口（下次保存同样的值会走「无变化」短路，永远补不上）。
--
-- 现在 service 通过独立的 StaleMarkFailed 布尔把这件事交给出口，回执换成本条词条。
-- 幂等：ON CONFLICT DO NOTHING（同一 key + lang 只写一次）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('trSavedMarkFail', 'zh-CN',
     '已保存 {n} 条译文，但未能标记页面待重建 —— 请手动重新发布一次，否则前台仍是旧译文。',
     200, 'admin', 'product translation saved but stale mark failed', 1, now(), now()),
    ('trSavedMarkFail', 'en-US',
     'Saved {n} translation(s), but failed to mark pages for rebuild — please republish manually, otherwise the site still shows the old text.',
     200, 'admin', 'product translation saved but stale mark failed', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('trSavedMarkFail', 'admin.product_translations.savedStaleMarkFailed')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
