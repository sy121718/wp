-- 531 · 折线图图例的「其他」词条（admin/ai/sessions.html）。
--
-- 背景：趋势图从柱状换成按 (供应商, 模型) 的多序列折线，超上限的几家合并成一条「其他」，
-- 需要一个新词条。530 已执行过，按「已上线的迁移不改」另开一条。
--
-- 形态：中英成对 INSERT，ON CONFLICT (item_key, lang) DO NOTHING，可重复执行。
-- 门槛：ConditionSQL 只枚举本批自己的 key（register_ai_session_trend_other_i18n.go）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.session.trend.other', 'zh-CN', '其他', 200, 'admin', 'admin/ai/sessions.html: 折线图图例的合并线', 1, now(), now()),
('admin.ai.session.trend.other', 'en-US', 'Other', 200, 'admin', 'admin/ai/sessions.html: merged line in trend legend', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
