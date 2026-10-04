-- 546 · 「MCP 与外部访问」页新增的一处文案（请求头写法）
--
-- 为什么单独一条而不是并进 545：545 的 ConditionSQL 在已执行过的库上会判为完成并**跳过**，
--   把新键写进 545 对本库（以及任何已跑过 545 的库）无效。
--   这正是 AGENTS.md「迁移」一节记的坑：幂等条件让「跳过」看起来像「通过」。新键一律新迁移。
--
-- 键的来源：mcp.html 原先硬编码了一行 `Authorization: Bearer <令牌>`，
--   check-i18n-coverage.sh 抓住了它（新增硬编码文案 0 → 1）。
--   改成 tr("admin.ai.mcp.endpoint.auth", …) 之后必须同批 seed 中英词条 ——
--   否则新库 / 重建库上英文站会显示中文兜底（不报错，只是串味）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.mcp.endpoint.auth', 'zh-CN', '请求头：Authorization: Bearer 你的令牌', 200, 'admin', 'admin/ai/mcp.html: 请求头写法', 1, now(), now()),
('admin.ai.mcp.endpoint.auth', 'en-US', 'Header: Authorization: Bearer <your token>', 200, 'admin', 'admin/ai/mcp.html: 请求头写法', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
