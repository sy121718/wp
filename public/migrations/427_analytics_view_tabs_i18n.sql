-- 427 · 访问统计页：5 张维度卡改页签，新增 tablist 的可访问名
--
-- 背景（审计 02-L §2 P1-6）：admin/analytics.html 的 5 张数据卡（按天 / 按路径 / 来源域 /
--   设备分类 / 语言）列头结构完全相同（维度名 + 浏览数 + 独立访客）、只读、页面上没有主行动 ——
--   并列渲染等于把 5 屏塞进一页，而用户每次只关心其中一种看法。改成 .tabs 后：
--     · 5 个面板仍由服务端**全部**渲染（数据一次取齐，切换是纯前端行为，零请求）；
--     · URL 的 ?view= 参数只决定**初始选中**，标签点击不改 URL（与 masterdata_changes.html 同形）。
--
-- 为什么要 seed 这条词条：.tab-list[role="tablist"] 的可访问名（aria-label）是 WAI-ARIA 的
--   硬要求 —— 没有它的屏幕阅读器只会读出「标签页」而说不出这组标签是关于什么的。
--   5 个标签的文字全部复用既有词条（admin.analytics.{daily,paths,referrers,ua,langs}.title），
--   三个维度的口径说明也复用既有 hint（referrers.hint / ua.hint / langs.hint），不重复登记。
--
-- 本批新增 1 个 key × 2 语言 = 2 行：
--   admin.analytics.view.label —— admin/analytics.html 的 .tab-list[role=tablist] 的 aria-label
--
-- 幂等：新增走 INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（重复执行不报错、不重复）。
-- 判定：register 侧的 ConditionSQL 由迁移器 db.Raw 直接执行、**没有任何参数替换** ——
--   判定要用的 key 只能写成 SQL 字面量（写成 ? 会被换成表名，判定恒为 0、每次启动重跑，178 踩过）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.analytics.view.label', 'zh-CN', '统计维度', 200, 'admin', 'admin/analytics.html: .tab-list[role=tablist] 的 aria-label（5 张维度卡改页签）', 1, now(), now()),
('admin.analytics.view.label', 'en-US', 'Dimensions', 200, 'admin', 'admin/analytics.html: aria-label of the .tab-list[role=tablist] (five dimension cards became tabs)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
