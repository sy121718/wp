-- 235 · i18n 词条 seed（仪表盘降级提示）
--
-- 背景：仪表盘改为真实概览后，工程契约读取失败时不再整页 500，而是渲染页面壳 + 一条提示。
--       （整页 500 会把侧栏一起打掉，把"概览暂时没有数字"放大成"后台进不去"。）
-- 覆盖：1 个 key / zh-CN 1 行 / en-US 1 行。
-- 语义：ON CONFLICT DO NOTHING。幂等：ConditionSQL 取该 key 的 zh-CN 行数。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.loadError', 'en-US', 'Overview data is temporarily unavailable: ', 200, 'admin', 'admin/dashboard.html: 概览数据读取失败时的降级提示', 1, now(), now()),
('admin.dashboard.loadError', 'zh-CN', '概览数据暂时读不到：', 200, 'admin', 'admin/dashboard.html: 概览数据读取失败时的降级提示', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
