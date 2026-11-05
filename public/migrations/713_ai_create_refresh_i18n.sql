-- 713 · AI 新建供应商「刷新页面查看新供应商」的界面词条
--
-- 为什么单独一批：新建成功后不再整页刷新（同页其它供应商卡片里可能还有用户刚填的密钥
--   与未保存的模型草稿，自动刷新会把它们一起吃掉）。新卡片仍由服务端渲染，所以需要一个
--   用户**自己点**的刷新入口；那句话必须有词库词条，否则门禁判它「被引用但未 seed」，
--   缺词时用户看到的是归口文案，看不出这是「内容已经保存、只差刷新」。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据见 register_ai_create_refresh_i18n.go。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.create.refresh', 'zh-CN', '刷新页面查看新供应商', 200, 'ai', 'admin/ai/provider_create.html: 新建成功后的手动刷新入口', 1, now(), now()),
('admin.ai.create.refresh', 'en-US', 'Refresh the page to see the new provider', 200, 'ai', 'admin/ai/provider_create.html: manual refresh entry after create', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
