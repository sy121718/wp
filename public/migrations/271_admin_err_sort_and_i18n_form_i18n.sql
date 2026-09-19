-- 271 · admin 错误文案第二组：列表排序参数 + 文案词条表单缺项（5 个 key × 中英 = 10 行）。
--
-- 与本项目所有 enums 常量一致：常量值就是 i18n key，真文案在这张表里。缺词条的后果是
-- 接口原样返回 key、页面原样显示 key（既不中文也不是话），所以新增常量必须同批 seed；
-- 同批还要扩 adminenums.AdminFacingMessages 白名单，否则这些错误会被归口成通用提示。
--
-- 两组背景：
--   · admin.err.sortFieldInvalid / admin.err.sortDirectionInvalid —— 此前是 service 里的中文
--     原文（errors.New("无效的排序字段")）：既不进白名单（页面与接口一律被归口成「操作失败，
--     请稍后重试」）、也无法翻译。参数错误是**客户端输入问题**，必须说得具体 —— 与白名单里
--     其它业务文案同一条判据，只是来源从 service 的业务判定换成了入参校验。
--   · admin.err.i18nKeyEmpty / i18nLangEmpty / i18nValueEmpty —— 空 key / 空语言 / 空内容
--     此前共用 MsgFieldRequired（「必填字段不能为空」）：运营点保存后只知道「有个字段没填」，
--     而这张表单有三行，只能逐个试。拆成三条，判据相同、结论具体。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.err.sortFieldInvalid', 'zh-CN', '无效的排序字段：请改用列表支持的排序字段', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.sortFieldInvalid', 'en-US', 'Invalid sort field: use one of the sortable fields supported by this list', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.sortDirectionInvalid', 'zh-CN', '无效的排序方向：只支持 asc / desc', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.sortDirectionInvalid', 'en-US', 'Invalid sort direction: only asc / desc are supported', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.i18nKeyEmpty', 'zh-CN', '词条 key 不能为空', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.i18nKeyEmpty', 'en-US', 'Entry key is required', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.i18nLangEmpty', 'zh-CN', '词条语言不能为空', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.i18nLangEmpty', 'en-US', 'Entry language is required', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.i18nValueEmpty', 'zh-CN', '词条内容不能为空', 400, 'admin', 'internal/module/admin/enums', 1, now(), now()),
    ('admin.err.i18nValueEmpty', 'en-US', 'Entry text is required', 400, 'admin', 'internal/module/admin/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
