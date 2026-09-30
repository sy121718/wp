-- 475 · 「最近一次自动重建失败」的界面文案。
--
-- 背景：自动重建失败此前只进日志，页面上唯一的痕迹是 stale 仍为 true ——
--   「待重建影响面」显示非零时，读的人分不清「还没轮到」（等）与「重建失败了」（查日志）。
--   迁移 474 已把失败阶段与时刻落到 pages 行上，这批是它的展示文案。
--
-- 归属：internal/templates/admin/partials/stale_pages_drawer.html（/admin/articles 与
--   /admin/blocks 共用）、internal/templates/admin/page/pages.html 的折叠清单 ——
--   三处显示同一批页面，口径必须一致。
-- 阶段文案只给两级（与 service 侧的闭集常量一一对应）：再细的分类需要把错误映射成枚举，
--   而那层映射本身就会漂；两级已经足以决定「下一步查哪里」。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判定枚举本批自己的 3 个 key（×2 语言 = 6 行）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.pages.impact.rebuild_failed', 'zh-CN', '最近一次自动重建失败：', 200, 'admin', '待重建影响面：失败提示前缀', 1, now(), now()),
('admin.pages.impact.rebuild_failed', 'en-US', 'Last automatic rebuild failed: ', 200, 'admin', '待重建影响面：失败提示前缀', 1, now(), now()),
('admin.pages.impact.stage_plan', 'zh-CN', '计划阶段（站点语言清单或旧发布范围读不到）', 200, 'admin', '待重建影响面：失败阶段 plan', 1, now(), now()),
('admin.pages.impact.stage_plan', 'en-US', 'planning (site language list or previous publish scope unreadable)', 200, 'admin', '待重建影响面：失败阶段 plan', 1, now(), now()),
('admin.pages.impact.stage_build', 'zh-CN', '构建 / 发布阶段', 200, 'admin', '待重建影响面：失败阶段 build', 1, now(), now()),
('admin.pages.impact.stage_build', 'en-US', 'build / publish', 200, 'admin', '待重建影响面：失败阶段 build', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
