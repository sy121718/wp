-- 426 · 插件管理页「产物对账巡检」折叠卡的标题与不一致计数词条（2 个 key × 中英 = 4 行）。
--
-- 背景（审计 02-L P1-17）：/admin/plugins 上四类产物对账结果（孤儿 schema / 缺 schema 的注册行 /
-- 孤儿存储目录 / 目录缺失的注册行）此前是四张独立的卡片，一旦出现就把页尾的「安装插件」推到第 5 屏。
-- 本批把它们收进**一张** <details class="section-fold card">：折叠省版面，但 summary 常显并带
-- 不一致**类数**与告警色（孤儿 schema 里可能有真实数据，是安全信号，不能折起来就看不见）。
--
-- 本批新增 2 个 key：
--   admin.plugins.patrol.title       —— 折叠卡的标题（summary 里的 .fold-title）
--   admin.plugins.patrol.badge_kinds —— 不一致计数的量词（模板输出「N」+ 空格 + 本词条）
--
-- badge_kinds 的值**不带前后空白**：数字与词条之间的空格留在模板里，运营在词条后台编辑时
-- 不会因为顺手删掉一个看不见的空格而让 badge 粘成「4类不一致」。
--
-- 与 279 的关系：279 已 seed 的 12 个 admin.plugins.patrol.* 词条**一个都没改**（四个小节标题
-- 与说明文案原样保留），本批只补折叠卡外壳需要的两个新 key。
--
-- http_code 用 200、category 用 admin：这些是页面标签，不是错误文案（与 279 / 278 同口径）。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.plugins.patrol.title', 'zh-CN', '产物对账巡检', 200, 'admin', 'internal/templates/admin/plugins.html: 巡检折叠卡 summary 标题', 1, now(), now()),
    ('admin.plugins.patrol.title', 'en-US', 'Artifact reconciliation', 200, 'admin', 'internal/templates/admin/plugins.html: 巡检折叠卡 summary 标题', 1, now(), now()),
    ('admin.plugins.patrol.badge_kinds', 'zh-CN', '类不一致', 200, 'admin', 'internal/templates/admin/plugins.html: 不一致类数量词（模板在其前面输出数字与空格）', 1, now(), now()),
    ('admin.plugins.patrol.badge_kinds', 'en-US', 'mismatched kinds', 200, 'admin', 'internal/templates/admin/plugins.html: 不一致类数量词（模板在其前面输出数字与空格）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
