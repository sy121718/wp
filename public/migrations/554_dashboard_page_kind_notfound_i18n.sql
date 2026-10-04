-- 554 · 补一条页面类型标签：notFound（admin/dashboard.html 的页面排行）
--
-- 背景：553 建了六种页面类型的文案，漏了 notFound。它由测试抓出来
-- （TestOverviewPageKindKeyCoversEveryKnownKind 拿 pageenums.PageKinds() 当输入逐条比对）——
-- 少了它的表现不是报错，而是 404 的那些路径在排行里**不显示类型标签**，
-- 而 404 恰恰是运营最该看见的一类流量（用户在找不存在的页）。
--
-- 覆盖：1 个新 key × 2 语言 = 2 条。
--
-- 为什么不直接补进 553：553 的 ConditionSQL 在已执行过的库上判为完成并跳过，
-- 往它里面加语句只会让新库与老库分叉（迁移 540 / 545 / 551 各记过一次这个坑）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.pageKind.notFound', 'en-US', 'Not found', 200, 'admin', 'admin/dashboard.html: 页面类型标签（404）', 1, now(), now()),
('admin.dashboard.pageKind.notFound', 'zh-CN', '未找到', 200, 'admin', 'admin/dashboard.html: 页面类型标签（404）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
