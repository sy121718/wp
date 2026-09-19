-- 258 · ErrAuditFailed（publication 模块）词条 —— 中英各一行
--
-- 背景：SEO 体检接口（POST /api/publication/seo-audit）此前把 service 错误的原文
-- 直接交给 response.ErrorWithMessage —— 基础设施错误会带产物路径甚至 SQL 片段，
-- 违反审计 CQ-009（后台不直出内部错误）。改为固定归口 key 之后必须有词条，否则
-- pkg/response.translate 查不到就原样返回 key。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；注册侧 ConditionSQL 判本 key 是否已在。
-- category 用 error（与 058 的 enums 词条同口径），备注写来源文件。
INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
    ('ErrAuditFailed', 'zh-CN', 'SEO 体检失败，请查看服务端日志', 'error', 'internal/module/publication/enums/publication_enums.go', 1, 400, now(), now()),
    ('ErrAuditFailed', 'en-US', 'The SEO audit failed; check the server logs', 'error', 'internal/module/publication/enums/publication_enums.go', 1, 400, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
