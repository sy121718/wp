-- ========================================
-- 222 · 访问统计页维度榜与保留期提示的后台模板文案词条（SEO-019 / SEO-021）
--
-- 来源：internal/templates/admin/analytics.html 新增区块的取词。覆盖 18 个 key，
-- zh-CN / en-US 各一条（成对，不伪造单语）：
--   标题   referrers.title / ua.title / langs.title
--   列标题 col.referrer / col.ua_class / col.lang（col.views / col.visitors 复用 193 已有的词条）
--   说明   referrers.hint / ua.hint / langs.hint / rank.hint_lead / rank.hint_tail
--   占位   referrers.unknown / ua.unknown / langs.unknown（空取值的展示形态）
--   空态   referrers.empty / ua.empty / langs.empty
--   提示   breakdown.retention_hint（明细已过保留期、维度分布不可用）
--
-- 为什么必须 seed：模板里的中文只是 key 命中失败时的兜底，不 seed 就等于英文界面下显示中文 ——
-- 「两端都在、没接线」，与 187–221 各批的处理方式一致。
-- rank.hint_lead / rank.hint_tail 按标签边界拆成两个 key：中间夹的是 <strong> 里的条数上限，
-- 标签本身留在模板里（与 193 对含标签长句的处理一致）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源
--（DO UPDATE 会让运营改过的词条在下次部署时静默回滚）。
-- 注意：VALUES 列表最后一项末尾不能带逗号（整段交给 db.Exec，不做语句切分）。
-- 注册：public/migrations/register_admin_i18n.go（Seed 222-i18n-seed-analytics-dimensions）。
-- 连带：public/test/pkg/i18n/i18n_seed_functional_test.go 的词条行数 ledger 同步 +18/+18。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.analytics.referrers.title', 'zh-CN', '来源域', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.referrers.title', 'en-US', 'Referrer hosts', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.referrers.hint', 'zh-CN', '打点只上报域名（服务端再去掉协议与路径），所以同一站点的不同页面算同一个来源。空值是没有来源的访问：从地址栏输入、书签打开，或客户端没有发送 referrer。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.referrers.hint', 'en-US', 'The tracker reports only the host (the server strips the scheme and path), so different pages on the same site count as one source. An empty value is a visit with no referrer: a typed-in URL, a bookmark, or a client that sent none.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.referrers.empty', 'zh-CN', '没有来源数据。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.referrers.empty', 'en-US', 'No referrer data.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.referrers.unknown', 'zh-CN', '（无来源）', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.referrers.unknown', 'en-US', '(no referrer)', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.title', 'zh-CN', '设备分类', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.title', 'en-US', 'Device classes', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.hint', 'zh-CN', '分类由服务端按请求头判定（desktop / tablet / mobile / bot），不采信上报里的字段 —— 客户端可以自称任何设备。空值是请求没有带 UA。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.hint', 'en-US', 'The class is decided by the server from the request headers (desktop / tablet / mobile / bot); the value reported by the client is never trusted, since a client can claim to be anything. An empty value means the request carried no UA.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.empty', 'zh-CN', '没有设备数据。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.empty', 'en-US', 'No device data.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.unknown', 'zh-CN', '（未识别）', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.ua.unknown', 'en-US', '(unrecognized)', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.title', 'zh-CN', '语言', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.title', 'en-US', 'Languages', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.hint', 'zh-CN', '语言由产物在构建期烘进打点配置，所以它反映的是页面自己声明的语言，不是访客的浏览器语言。空值是这一页没有上报语言。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.hint', 'en-US', 'The language is baked into the tracking config at build time, so it reflects the language the page declares, not the browser language of the visitor. An empty value means this page reported no language.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.empty', 'zh-CN', '没有语言数据。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.empty', 'en-US', 'No language data.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.unknown', 'zh-CN', '（未上报）', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.langs.unknown', 'en-US', '(not reported)', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.rank.hint_lead', 'zh-CN', '下面三个维度榜各取前', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.rank.hint_lead', 'en-US', 'The three rankings below show the top ', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.rank.hint_tail', 'zh-CN', '条，按浏览数降序、并列时按取值升序（顺序是确定的，刷新不会换名次）；它们恒定读访问明细，与上方的「取数来源」无关。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.rank.hint_tail', 'en-US', ' entries by views (descending; ties are ordered by value ascending, so the order is deterministic and a refresh never reshuffles them). They always read the visit detail table, regardless of the source shown above.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.col.referrer', 'zh-CN', '来源域', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.col.referrer', 'en-US', 'Referrer host', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.col.ua_class', 'zh-CN', '设备分类', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.col.ua_class', 'en-US', 'Device class', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.col.lang', 'zh-CN', '语言', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.col.lang', 'en-US', 'Language', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.breakdown.retention_hint', 'zh-CN', '该时间段的访问明细已过保留期并被清理，维度分布不可用；总数与路径榜来自按天汇总，仍然有效。', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW()),
('admin.analytics.breakdown.retention_hint', 'en-US', 'The visit detail for this range has passed the retention window and was purged, so the breakdowns are unavailable. Totals and the path ranking come from the daily rollup and are still valid.', 200, 'admin', 'internal/templates/admin/analytics.html', 1, NOW(), NOW())
ON CONFLICT (item_key, lang) DO NOTHING;
