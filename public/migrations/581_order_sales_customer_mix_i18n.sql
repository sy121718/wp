-- 581 · 销售概览页「月度趋势按客户类型拆分」的词条（BIZ-1）。
--
-- 579 建的页面加了三个新概念：柱图现在按新客 / 回头客 / 游客分三段，图例要解释颜色，
-- 悬停要能读出三段的金额，所以本批只补这 5 个 key（其余词条复用 579 已种的那些）。
--
-- 三段的含义（与代码里的口径一致，改代码时这两处要一起改）：
--   new       该客户的**首单**落在与当前订单同一个自然月内
--   returning 首单在这个月之前
--   guest     订单没有账号（user_id 为空），既非新客也非回头客
-- `mix.hint` 那句「与查询区间无关」是刻意写进界面的：laravel CRM 的同名口径按
-- 区间裁剪后的当月起止判，本仓按自然月判，两者在「查部分月份」时结果不同 ——
-- 把这条差异挂在图例上，比写在提交信息里更有机会被读到。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.order.sales.mix.new', 'zh-CN', '新客'),
('admin.order.sales.mix.new', 'en-US', 'New'),
('admin.order.sales.mix.returning', 'zh-CN', '回头客'),
('admin.order.sales.mix.returning', 'en-US', 'Returning'),
('admin.order.sales.mix.guest', 'zh-CN', '游客单'),
('admin.order.sales.mix.guest', 'en-US', 'Guest'),
('admin.order.sales.mix.hint', 'zh-CN', '新客 = 该客户首单落在本自然月（与查询区间无关）'),
('admin.order.sales.mix.hint', 'en-US', 'New = the customer''s first order falls in that calendar month (independent of the filter range)'),
('admin.order.sales.monthly.total', 'zh-CN', '合计'),
('admin.order.sales.monthly.total', 'en-US', 'Total')
ON CONFLICT (item_key, lang) DO NOTHING;
