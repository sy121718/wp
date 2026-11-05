-- 706 · admin 批量删除返工新增的两条界面词条。
--
-- 这两条此前根本没有词条：
--   · admin.bulk.emptySelection —— 一个 id 都没选中就提交批量删除。旧实现直接返回空结果，
--     前端什么也没说；现在它是「整批被拒」的一种，主消息要能按语言说清原因。
--   · admin.bulk.invalidSelection —— 提交里带了不能当主键解析的原始值。旧实现把这类值
--     静默丢掉（`id == 0` 就 continue），用户勾了三行只删掉两行也不报错；现在整批打回，
--     明细走 data.items，主消息由这条词条给。
--
-- http_code 400 / category error：它们都走 JSON 出口的错误取词链，状态码是请求参数问题。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.bulk.emptySelection', 'zh-CN', '没有选中任何要删除的记录', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.bulk.emptySelection', 'en-US', 'Nothing was selected for deletion.', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.bulk.invalidSelection', 'zh-CN', '提交的记录标识无法识别，整批未执行', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
    ('admin.bulk.invalidSelection', 'en-US', 'Some submitted record identifiers could not be recognised; nothing was executed.', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
