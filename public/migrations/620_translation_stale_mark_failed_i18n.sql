-- 620 · 译文保存成功但标记待重建失败时的回执词条。
--
-- 背景：翻译工作台保存译文时，写入成功后还要「标记页面待重建」；那一步失败原先只写日志，
-- 用户看到「已保存 N 条译文；译文变更已标记待重建」——而实际上**没标记**，
-- 前台仍是旧译文，也没有重试入口（下次保存同样的值会走「无变化」短路，永远补不上）。
--
-- 现在 service 通过独立的 StaleMarkFailed 布尔把这件事交给出口，回执换成本条词条。
-- 幂等：ON CONFLICT DO NOTHING（同一 key + lang 只写一次）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.product_translations.savedStaleMarkFailed', 'zh-CN',
     '已保存 {n} 条译文，但未能标记页面待重建 —— 请手动重新发布一次，否则前台仍是旧译文。',
     200, 'admin', 'product translation saved but stale mark failed', 1, now(), now()),
    ('admin.product_translations.savedStaleMarkFailed', 'en-US',
     'Saved {n} translation(s), but failed to mark pages for rebuild — please republish manually, otherwise the site still shows the old text.',
     200, 'admin', 'product translation saved but stale mark failed', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
