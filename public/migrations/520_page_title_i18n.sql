-- 520 · 后台整页标题（layout 的 <title>）词条。
--
-- 背景：浏览器遍历全部菜单路径时，/admin/system 返回 500 —— layout.html 用点号取字段
--   写死 `<title>{{.title}} …`，而 shell.Prepare 不注入 title（title 必须由各页 handler
--   自己放进 gin.H）。缺 title 时 Jet 抛「there is no field or method 'title' in gin.H」，
--   经 jet_render.go 的 renderError 变成整页 500。
--   本批修好三处（sysconfig 系统设置页 + AI 的供应商页 / 会话页），并加静态门禁
--   scripts/check-page-title.sh 防复发；layout.html 另加 isset 兜底（止损，不替代本修）。
--
-- 词条范围：只有 sysconfig 的标题是新 key（admin.system.title）。
--   AI 的两处标题复用模板 h1 同源的既有 key —— admin.ai.title（513）、
--   admin.ai.session.title（516），已 seed，故不重复插入。
--   判据见 scripts/check-i18n-keys-seeded.sh（只认 INSERT 元组，注释与 ConditionSQL 不算）。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
-- 门槛：ConditionSQL 只枚举本批自己的代表 key admin.system.title 的中英两条
--   （register_page_title_i18n.go），不用 LIKE 前缀 / 全库计数
--   （存量库永远满足 → 补词条的那条迁移永远不会执行，迁移 494 记过这个坑）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.system.title', 'zh-CN', '系统设置', 200, 'admin', 'admin/system/settings.html: 页面标题（layout 的 <title>）', 1, now(), now()),
('admin.system.title', 'en-US', 'System settings', 200, 'admin', 'admin/system/settings.html: page title (layout <title>)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
