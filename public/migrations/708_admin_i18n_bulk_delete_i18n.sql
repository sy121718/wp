-- 708 · 词库批量删除返工新增的两条错误词条。
--
-- 这两条此前没有词条：
--   · admin.err.i18nEntryMissing —— 批量删除里有 (item_key, lang) 已经不存在的词条。
--     旧实现逐条删、不存在的悄悄跳过、回执里带 Skipped 计数；现在整批打回，
--     不存在的明细走 data.items，主消息由这条词条给。状态 404：语义是「你选的东西没了」，
--     用户下一步是刷新重选，不是改参数。
--   · admin.err.i18nSelectionEmpty —— 一个词条都没选中就提交批量删除。旧实现回空结果、
--     前端什么都不说；现在这是「整批被拒」的一种，状态 400，属于请求参数问题。
--
-- category error：两条都走 JSON 出口的错误取词链，由 response.ErrorStatusAuto 取词。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.err.i18nEntryMissing', 'zh-CN', '选中的词条已不存在，请刷新后重选', 404, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.err.i18nEntryMissing', 'en-US', 'Some selected entries no longer exist. Refresh and pick again.', 404, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.err.i18nSelectionEmpty', 'zh-CN', '没有选中任何词条', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.err.i18nSelectionEmpty', 'en-US', 'No entries were selected.', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
