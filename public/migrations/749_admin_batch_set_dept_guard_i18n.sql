-- 749 · 批量改归属的两条守卫词条。
--
-- 这两条此前没有词条：
--   · admin.err.adminSelectionEmpty —— 批量改归属一个人都没选。旧实现回 nil 当成功，
--     前端什么都不说；现在这是「整批被拒」的一种，状态 400。
--   · admin.err.adminBatchTooMany —— 去重后超过共享 ID 上限。整批拒绝，状态 400；
--     条数属于明细，不拼进这句。
--
-- category error：两条都走错误取词链。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.err.adminSelectionEmpty', 'zh-CN', '没有选中任何成员', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.err.adminSelectionEmpty', 'en-US', 'No members were selected.', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.err.adminBatchTooMany', 'zh-CN', '一次最多操作 200 名成员，请分批进行', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.err.adminBatchTooMany', 'en-US', 'At most 200 members can be operated at once; please split into batches.', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
