-- 534 · 收口 532 引出的两个副作用。
--
-- ① 532 改了菜单路径之后，515 的旧判据（按具体 path 判定）不再成立 → 它每次启动都重跑，
--    并插回一条 /admin/ai/providers（实测 id=159）。判据已在本批同步改成按语义判定
--    （见 register_ai_menu.go），这里把插出来的那条软删掉。
-- ② 533 给 admin.ai.lead 写新文案时用了 DO NOTHING，但 513 早已 seed 过同一个 key →
--    新文案没覆盖旧文案（页面仍显示「为 AI 能力配置供应商…」）。这里显式覆盖。
--
-- 幂等：软删带 deleted_at IS NULL；两条 UPDATE 都收敛到同一个目标值，重跑无副作用。
UPDATE sys_menus SET deleted_at = NOW(), update_time = NOW()
 WHERE path = '/admin/ai/providers' AND title = 'AI 模型' AND deleted_at IS NULL;

UPDATE sys_i18n SET item_value = '两个标签：AI 模型配置供应商与模型目录；AI 会话是每个会话的用量记录 —— 谁在什么时候消耗了多少 token。', update_time = now()
 WHERE item_key = 'admin.ai.lead' AND lang = 'zh-CN';

UPDATE sys_i18n SET item_value = 'Two tabs: AI Models configures providers and their model catalog; AI Sessions is the usage record — who spent how many tokens and when.', update_time = now()
 WHERE item_key = 'admin.ai.lead' AND lang = 'en-US';
