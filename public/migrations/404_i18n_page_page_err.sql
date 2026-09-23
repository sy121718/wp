-- ========================================
-- 404 — page 域后台页面失败出口收口的新增词条
--
-- 背景：page 域的页面 handler 有三处失败出口脱掉页壳（AGENTS.md 形态 ①）：
--   · pages_handle.go   c.String(400, "项目名称不能为空")           —— 硬编码中文
--   · pages_handle.go   c.String(400, "项目与页面路径不能为空")     —— 硬编码中文 + 合成句
--   · site_slot_handle.go c.String(500, siteSlotInternalText(c))   —— 文案受控、出口仍脱页壳
-- 本批按调用方读什么分别改成：原生表单 → 303 回列表页 + ?err=<当前语言文案>；
-- GET 装载失败 → 降级渲染（空列表 + 提示，HTTP 200，页壳保留）。
--
-- 页面提示是**直接渲染的文本**（模板 .Err 不经过 pkg/response 的翻译层），
-- 所以文案必须走 i18n：没有词条就只能硬编码中文，英文界面上永远显示中文。
--
-- 本批新增 2 个 key × 2 语言 = 4 行：
--   page.form.projectNameRequired —— 新建站点工程时名称为空
--   page.form.pathRequired        —— 新建页面时路径为空
--
-- 缺工程 id 那一条**不在此列**：它复用既有的 ErrProjectRequired
--（403 已登记中英词条，语义就是「没有工程作用域」）。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批不修改任何既有词条的值。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('page.form.projectNameRequired', 'zh-CN', '站点工程名称不能为空，未创建。', 400, 'ui', 'internal/module/page/inbound/http/pages_handle.go: 新建站点工程缺名称（?err= 回执）', 1, now(), now()),
('page.form.projectNameRequired', 'en-US', 'Project name is required; nothing was created.', 400, 'ui', 'internal/module/page/inbound/http/pages_handle.go: 新建站点工程缺名称（?err= 回执）', 1, now(), now()),
('page.form.pathRequired', 'zh-CN', '页面路径不能为空，未创建。', 400, 'ui', 'internal/module/page/inbound/http/pages_handle.go: 新建页面缺草稿路径（?err= 回执）', 1, now(), now()),
('page.form.pathRequired', 'en-US', 'Page path is required; nothing was created.', 400, 'ui', 'internal/module/page/inbound/http/pages_handle.go: 新建页面缺草稿路径（?err= 回执）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
