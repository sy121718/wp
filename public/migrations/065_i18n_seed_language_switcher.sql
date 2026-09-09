-- 065 · i18n 词条 seed（语言切换器容器无障碍标签，多语言 P3 前台切换器）
--
-- 覆盖：1 个 key / zh-CN 1 行 / en-US 1 行（人工编写，非机器伪造）。
-- 来源：internal/builder/components/languages/jet.go（core.languages 的 nav aria-label）
--       internal/templates/components/languages.jet
-- 命名：site.component.languages.label（docs/06-D §10.3；site.* 为访客面命名空间）。
-- 兜底：构建期缺词条时回退组件包内中文原文「语言」（core.RenderContext.Text），
--       绝不输出空串或裸 key（空 aria-label 属事故）。
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE（可重复执行）；
--       注册见 register.go，ConditionSQL 以 site.component.languages.* 的 zh-CN 行数 1 为门槛
--       （与 060 的 site.component.% 门槛互不干扰）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.component.languages.label', 'en-US', 'Language', 200, 'ui', 'internal/builder/components/languages/jet.go', 1, now(), now()),
('site.component.languages.label', 'zh-CN', '语言', 200, 'ui', 'internal/builder/components/languages/jet.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET
    item_value  = EXCLUDED.item_value,
    http_code   = EXCLUDED.http_code,
    category    = EXCLUDED.category,
    remark      = EXCLUDED.remark,
    status      = EXCLUDED.status,
    update_time = now();
