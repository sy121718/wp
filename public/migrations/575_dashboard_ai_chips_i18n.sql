-- 575 · 概览页 AI 区的一行说明与五条快捷提问。
--
-- 为什么单开一条：574 从未发布（文件解析方案已放弃），但 573 及其之前的迁移
-- 已经在开发库与其他库上执行过 —— 迁移一旦发布过就不能追加内容（545 / 546 /
-- 570 / 571 / 573 同一课）。这里用 575 而不是复用 574 号，是因为 574 在本仓库的
-- 历史里已经出现过一次（虽未提交），复用编号会让「这个号到底执行过没有」变成
-- 需要查 git 历史才能回答的问题。
--
-- 五条快捷提问不是装饰：它们是**这页能力的说明书**。用户看到空输入框时
-- 不知道能问什么（此前的占位文字只有一句），点了就填进输入框、可以改措辞再发。
-- 每条都对应真实存在的工具（订单 / 客户 / 流量 / 评论），不写做不到的示例。
--
-- 幂等：ON CONFLICT DO NOTHING；判据取本批自己的代表 key。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.dashboard.ai.hint', 'zh-CN', '直接问，它会自己查订单、商品、客户与流量数据。'),
('admin.dashboard.ai.hint', 'en-US', 'Just ask — it looks up orders, products, customers and traffic on its own.'),
('admin.dashboard.ai.chip1', 'zh-CN', '这两周卖得怎么样？'),
('admin.dashboard.ai.chip1', 'en-US', 'How are sales doing these two weeks?'),
('admin.dashboard.ai.chip2', 'zh-CN', '哪几单还没发货？'),
('admin.dashboard.ai.chip2', 'en-US', 'Which orders have not shipped yet?'),
('admin.dashboard.ai.chip3', 'zh-CN', '这个后台有多少客户？'),
('admin.dashboard.ai.chip3', 'en-US', 'How many customers are there?'),
('admin.dashboard.ai.chip4', 'zh-CN', '这周流量怎么样？'),
('admin.dashboard.ai.chip4', 'en-US', 'How is traffic this week?'),
('admin.dashboard.ai.chip5', 'zh-CN', '有哪些评论在等审核？'),
('admin.dashboard.ai.chip5', 'en-US', 'Which comments are awaiting review?')
ON CONFLICT (item_key, lang) DO NOTHING;
