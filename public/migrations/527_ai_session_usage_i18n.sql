-- 527 · AI 会话页用量看板（指标卡 / 趋势图 / 多维筛选 / 分页）的词条。
--
-- 背景：m00289 / m00350 把会话页从「只有关键词 + 状态的列表」扩成用量看板，
--   新增 24 个 key（admin/ai/sessions.html）。516 与 519 已覆盖会话页原有的词条，
--   本批只补新增的，不重复登记。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
-- 门槛：ConditionSQL 只枚举本批自己的 key（register_ai_session_usage_i18n.go），
--   不用 LIKE 前缀 / 全库计数 —— 存量库永远满足会让「补词条」的迁移永远不执行（迁移 494 的坑）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.tab.sessions', 'zh-CN', 'AI 会话', 200, 'admin', 'admin/ai/sessions.html: 标签-会话', 1, now(), now()),
('admin.ai.tab.sessions', 'en-US', 'AI Sessions', 200, 'admin', 'admin/ai/sessions.html: tab sessions', 1, now(), now()),
('admin.ai.tab.models', 'zh-CN', 'AI 模型', 200, 'admin', 'admin/ai/sessions.html: 标签-模型', 1, now(), now()),
('admin.ai.tab.models', 'en-US', 'AI Models', 200, 'admin', 'admin/ai/sessions.html: tab models', 1, now(), now()),
('admin.ai.session.stat.sessions', 'zh-CN', '会话数', 200, 'admin', 'admin/ai/sessions.html: 指标卡-会话数', 1, now(), now()),
('admin.ai.session.stat.sessions', 'en-US', 'Sessions', 200, 'admin', 'admin/ai/sessions.html: stat sessions', 1, now(), now()),
('admin.ai.session.stat.events', 'zh-CN', '事件数', 200, 'admin', 'admin/ai/sessions.html: 指标卡-事件数', 1, now(), now()),
('admin.ai.session.stat.events', 'en-US', 'Events', 200, 'admin', 'admin/ai/sessions.html: stat events', 1, now(), now()),
('admin.ai.session.stat.tokens', 'zh-CN', '累计 token', 200, 'admin', 'admin/ai/sessions.html: 指标卡-累计 token', 1, now(), now()),
('admin.ai.session.stat.tokens', 'en-US', 'Total tokens', 200, 'admin', 'admin/ai/sessions.html: stat total tokens', 1, now(), now()),
('admin.ai.session.stat.tokens.note', 'zh-CN', '按事件正文估算，非当前上下文', 200, 'admin', 'admin/ai/sessions.html: 累计 token 口径', 1, now(), now()),
('admin.ai.session.stat.tokens.note', 'en-US', 'Estimated from event bodies, not the live context', 200, 'admin', 'admin/ai/sessions.html: total tokens note', 1, now(), now()),
('admin.ai.session.stat.compacts', 'zh-CN', '压缩次数', 200, 'admin', 'admin/ai/sessions.html: 指标卡-压缩次数', 1, now(), now()),
('admin.ai.session.stat.compacts', 'en-US', 'Compactions', 200, 'admin', 'admin/ai/sessions.html: stat compactions', 1, now(), now()),
('admin.ai.session.stat.avg', 'zh-CN', '平均每会话', 200, 'admin', 'admin/ai/sessions.html: 指标卡-平均每会话', 1, now(), now()),
('admin.ai.session.stat.avg', 'en-US', 'Avg per session', 200, 'admin', 'admin/ai/sessions.html: stat avg tokens', 1, now(), now()),
('admin.ai.session.stat.avg.note', 'zh-CN', '累计 token ÷ 会话数', 200, 'admin', 'admin/ai/sessions.html: 均值口径', 1, now(), now()),
('admin.ai.session.stat.avg.note', 'en-US', 'Total tokens ÷ sessions', 200, 'admin', 'admin/ai/sessions.html: avg note', 1, now(), now()),
('admin.ai.session.trend.title', 'zh-CN', 'Token 用量趋势', 200, 'admin', 'admin/ai/sessions.html: 趋势图标题', 1, now(), now()),
('admin.ai.session.trend.title', 'en-US', 'Token usage trend', 200, 'admin', 'admin/ai/sessions.html: trend title', 1, now(), now()),
('admin.ai.session.trend.empty', 'zh-CN', '这段时间还没有事件。', 200, 'admin', 'admin/ai/sessions.html: 趋势空态', 1, now(), now()),
('admin.ai.session.trend.empty', 'en-US', 'No events in this period.', 200, 'admin', 'admin/ai/sessions.html: trend empty', 1, now(), now()),
('admin.ai.session.trend.peak', 'zh-CN', '峰值', 200, 'admin', 'admin/ai/sessions.html: 趋势峰值', 1, now(), now()),
('admin.ai.session.trend.peak', 'en-US', 'Peak', 200, 'admin', 'admin/ai/sessions.html: trend peak', 1, now(), now()),
('admin.ai.session.filter.allProvider', 'zh-CN', '全部供应商', 200, 'admin', 'admin/ai/sessions.html: 供应商筛选', 1, now(), now()),
('admin.ai.session.filter.allProvider', 'en-US', 'All providers', 200, 'admin', 'admin/ai/sessions.html: provider filter', 1, now(), now()),
('admin.ai.session.filter.allModel', 'zh-CN', '全部模型', 200, 'admin', 'admin/ai/sessions.html: 模型筛选', 1, now(), now()),
('admin.ai.session.filter.allModel', 'en-US', 'All models', 200, 'admin', 'admin/ai/sessions.html: model filter', 1, now(), now()),
('admin.ai.session.filter.user', 'zh-CN', '创建人', 200, 'admin', 'admin/ai/sessions.html: 创建人筛选', 1, now(), now()),
('admin.ai.session.filter.user', 'en-US', 'Creator', 200, 'admin', 'admin/ai/sessions.html: creator filter', 1, now(), now()),
('admin.ai.session.filter.allUser', 'zh-CN', '全部创建人', 200, 'admin', 'admin/ai/sessions.html: 创建人筛选默认项', 1, now(), now()),
('admin.ai.session.filter.allUser', 'en-US', 'All creators', 200, 'admin', 'admin/ai/sessions.html: creator filter default', 1, now(), now()),
('admin.ai.session.filter.from', 'zh-CN', '起始日期', 200, 'admin', 'admin/ai/sessions.html: 时间区间起', 1, now(), now()),
('admin.ai.session.filter.from', 'en-US', 'From', 200, 'admin', 'admin/ai/sessions.html: range from', 1, now(), now()),
('admin.ai.session.filter.to', 'zh-CN', '结束日期', 200, 'admin', 'admin/ai/sessions.html: 时间区间止', 1, now(), now()),
('admin.ai.session.filter.to', 'en-US', 'To', 200, 'admin', 'admin/ai/sessions.html: range to', 1, now(), now()),
('admin.ai.session.filter.reset', 'zh-CN', '重置', 200, 'admin', 'admin/ai/sessions.html: 重置筛选', 1, now(), now()),
('admin.ai.session.filter.reset', 'en-US', 'Reset', 200, 'admin', 'admin/ai/sessions.html: reset filter', 1, now(), now()),
('admin.ai.session.col.provider', 'zh-CN', '供应商', 200, 'admin', 'admin/ai/sessions.html: 列-供应商', 1, now(), now()),
('admin.ai.session.col.provider', 'en-US', 'Provider', 200, 'admin', 'admin/ai/sessions.html: column provider', 1, now(), now()),
('admin.ai.session.col.model', 'zh-CN', '模型', 200, 'admin', 'admin/ai/sessions.html: 列-模型', 1, now(), now()),
('admin.ai.session.col.model', 'en-US', 'Model', 200, 'admin', 'admin/ai/sessions.html: column model', 1, now(), now()),
('admin.ai.session.list.total', 'zh-CN', '共', 200, 'admin', 'admin/ai/sessions.html: 列表计数前缀', 1, now(), now()),
('admin.ai.session.list.total', 'en-US', 'Total', 200, 'admin', 'admin/ai/sessions.html: list count prefix', 1, now(), now()),
('admin.ai.session.page.prev', 'zh-CN', '上一页', 200, 'admin', 'admin/ai/sessions.html: 上一页', 1, now(), now()),
('admin.ai.session.page.prev', 'en-US', 'Previous', 200, 'admin', 'admin/ai/sessions.html: previous page', 1, now(), now()),
('admin.ai.session.page.next', 'zh-CN', '下一页', 200, 'admin', 'admin/ai/sessions.html: 下一页', 1, now(), now()),
('admin.ai.session.page.next', 'en-US', 'Next', 200, 'admin', 'admin/ai/sessions.html: next page', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
