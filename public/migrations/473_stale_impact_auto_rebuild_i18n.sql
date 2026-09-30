-- 473 · 「待重建影响面」的说明文案改为与自动重建一致。
--
-- 背景：块/主题变更、主题设置保存、组件版本更新这三条 stale 来源此前**只标记不重建**，
--   而人工入口并不存在（page.RebuildStale 只有契约方法、没有 HTTP 路由）——
--   表现是「后台显示 N 个页面待重建，线上一直不变」。代码侧已把三条来源全部接上自动重建
--   （BlockStalePropagator / refreshThemePages / 启动期组件版本比对，统一走 rebuildStaleAsync）。
--
--   文案必须跟着改：原文「重新构建这些页面后新内容才会出现在访问面」是在教用户「去重建」，
--   而现在是系统自己重建 —— 留着它会让人去找一个不存在的按钮。
--
-- 归属：admin.blocks.impact.help 被三处共用（/admin/articles 与 /admin/blocks 的抽屉片段、
--   /admin/pages 的折叠清单说明）。改这一条，三处一起对齐。
--
-- 幂等：UPDATE 的结果与执行次数无关；判定枚举本批自己的 1 个 key、且要求两种语言的新值都已落库。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.blocks.impact.help', 'zh-CN', '块、内容、主题或导航改动过，产物的字节还停在旧版本。系统会在改动后自动重建这些页面；若长时间停在这里，说明重建失败或构建队列积压（原因已记入服务日志）。', 200, 'admin', '待重建影响面说明（三处共用）', 1, now(), now()),
('admin.blocks.impact.help', 'en-US', 'Blocks, content, themes or navigation changed, but the published bytes still carry the old version. These pages are rebuilt automatically; if they stay here for long, the rebuild failed or the build queue is backed up (see the service log).', 200, 'admin', '待重建影响面说明（三处共用）', 1, now(), now())
ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value, update_time = now();
