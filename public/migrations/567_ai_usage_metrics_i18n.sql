-- 567 · AI 会话页「验收数字」三张卡的词条（docs/16 §3.1）。
--
-- 背景：会话页补上命中率 / 摘要占用上下文 / 上报缓存的调用三张卡。
--       前两个是 docs/16 §3.1 的验收数字，第三个是命中率的分母构成。
--
-- 覆盖：7 个新 key × 2 语言 = 14 条。
--
-- 「没有上报」的说明必须与「命中率是 0%」分开写：前者是上游不给数据，
-- 后者是前缀纪律失效 —— 把两者显示成同一句话等于把最有价值的观测抹掉。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.session.stat.hitRate', 'en-US', 'Prefix cache hit rate', 200, 'admin', 'admin/ai/sessions.html: 用量卡标签（命中率）', 1, now(), now()),
('admin.ai.session.stat.hitRate', 'zh-CN', '前缀缓存命中率', 200, 'admin', 'admin/ai/sessions.html: 用量卡标签（命中率）', 1, now(), now()),
('admin.ai.session.stat.hitRate.note', 'en-US', 'Cached / reported cache calls; healthy range 95-97%', 200, 'admin', 'admin/ai/sessions.html: 命中率说明（有数据时）', 1, now(), now()),
('admin.ai.session.stat.hitRate.note', 'zh-CN', '命中 / 上报缓存的调用输入；健康区间 95–97%', 200, 'admin', 'admin/ai/sessions.html: 命中率说明（有数据时）', 1, now(), now()),
('admin.ai.session.stat.hitRate.noData', 'en-US', 'Provider does not report cache fields, hit rate unknown', 200, 'admin', 'admin/ai/sessions.html: 命中率说明（无数据时）', 1, now(), now()),
('admin.ai.session.stat.hitRate.noData', 'zh-CN', '供应商没有上报缓存字段，算不出命中率', 200, 'admin', 'admin/ai/sessions.html: 命中率说明（无数据时）', 1, now(), now()),
('admin.ai.session.stat.compactCost', 'en-US', 'Summary share of context', 200, 'admin', 'admin/ai/sessions.html: 用量卡标签（压缩开销）', 1, now(), now()),
('admin.ai.session.stat.compactCost', 'zh-CN', '摘要占用上下文', 200, 'admin', 'admin/ai/sessions.html: 用量卡标签（压缩开销）', 1, now(), now()),
('admin.ai.session.stat.compactCost.note', 'en-US', 'Summary tokens / all event tokens; above 2% long-term means folding too often', 200, 'admin', 'admin/ai/sessions.html: 压缩开销说明', 1, now(), now()),
('admin.ai.session.stat.compactCost.note', 'zh-CN', '折叠摘要的 token / 全部事件 token；长期超过 2% 说明压得太频繁', 200, 'admin', 'admin/ai/sessions.html: 压缩开销说明', 1, now(), now()),
('admin.ai.session.stat.cachedCalls', 'en-US', 'Calls reporting cache', 200, 'admin', 'admin/ai/sessions.html: 用量卡标签（分母）', 1, now(), now()),
('admin.ai.session.stat.cachedCalls', 'zh-CN', '上报缓存的调用', 200, 'admin', 'admin/ai/sessions.html: 用量卡标签（分母）', 1, now(), now()),
('admin.ai.session.stat.cachedCalls.note', 'en-US', 'Hit rate only holds over these calls; unreported ones are excluded', 200, 'admin', 'admin/ai/sessions.html: 分母说明', 1, now(), now()),
('admin.ai.session.stat.cachedCalls.note', 'zh-CN', '分母：命中率只在这些调用上成立，未上报的不计入', 200, 'admin', 'admin/ai/sessions.html: 分母说明', 1, now(), now());
