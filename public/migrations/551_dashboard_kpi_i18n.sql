-- 551 · 概览页新增两格 KPI 的词条（admin/dashboard.html）
--
-- 背景：KPI 按 docs/17 §P6 的口径补齐 —— 新增「商品销售总量」（区间内售出件数，
--       与热销榜同筛选条件），并把「文章浏览」换成「页面浏览」（全站 PV，
--       小字注明其中文章页那部分）。
--
-- 覆盖：4 个新 key × 2 语言 = 8 条（ON CONFLICT DO NOTHING）。
--
-- 为什么「文章浏览」用新 key 而不是改写旧 key 的文案：旧 key
-- （kpi.articleViews / kpi.articleViewsNote）说的是「只看文章页路径」这一个数，
-- 而新的那一格是「全站总量 + 其中文章页」两个数。同一把钥匙换语义，
-- 下一次有人改动时没法从名字看出它曾经是什么。
--
-- 旧 key 不删：540 的 seed 是 ON CONFLICT DO NOTHING，删了它下次 540 重跑会插回来
-- （判据只看代表 key，不覆盖它）；留着只是两条无引用的历史词条。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.dashboard.kpi.items', 'en-US', 'Items sold', 200, 'admin', 'admin/dashboard.html: 区间商品销售总量 KPI（件数）', 1, now(), now()),
('admin.dashboard.kpi.items', 'zh-CN', '商品销售总量', 200, 'admin', 'admin/dashboard.html: 区间商品销售总量 KPI（件数）', 1, now(), now()),
('admin.dashboard.kpi.itemsNote', 'en-US', 'Units; paid orders only', 200, 'admin', 'admin/dashboard.html: 商品销售总量的口径说明', 1, now(), now()),
('admin.dashboard.kpi.itemsNote', 'zh-CN', '件数，只算计入消费的订单', 200, 'admin', 'admin/dashboard.html: 商品销售总量的口径说明', 1, now(), now()),
('admin.dashboard.kpi.pageViews', 'en-US', 'Page views', 200, 'admin', 'admin/dashboard.html: 区间全站页面浏览总量 KPI（PV）', 1, now(), now()),
('admin.dashboard.kpi.pageViews', 'zh-CN', '页面浏览', 200, 'admin', 'admin/dashboard.html: 区间全站页面浏览总量 KPI（PV）', 1, now(), now()),
('admin.dashboard.kpi.pageViewsNote', 'en-US', 'All paths; articles: ', 200, 'admin', 'admin/dashboard.html: 页面浏览口径说明（拼在数值前面）', 1, now(), now()),
('admin.dashboard.kpi.pageViewsNote', 'zh-CN', '全站路径；其中文章页：', 200, 'admin', 'admin/dashboard.html: 页面浏览口径说明（拼在数值前面）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
