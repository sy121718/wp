-- 576 · 概览页 AI 区的「继续上次对话」。
--
-- 背景：概览页此前是「有历史回答就自动铺开」，而展开状态（.is-asking）只存在于
-- 内存里 —— 关掉浏览器重新登录进来，历史回填把整页变成一屏聊天记录，看起来像
-- 「登录后进错了页」。改成默认收起、由用户主动点这个入口才铺开。
--
-- 文案取「继续上次对话」而不是「查看历史」：它接的是**同一段会话**（同一个会话键），
-- 点开之后接着问就是续上一轮，不是打开一个只读的记录页。
--
-- 幂等：ON CONFLICT DO NOTHING；判据取本批自己的 key。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.dashboard.ai.resume', 'zh-CN', '继续上次对话'),
('admin.dashboard.ai.resume', 'en-US', 'Continue last chat')
ON CONFLICT (item_key, lang) DO NOTHING;
