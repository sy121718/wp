-- 604 · 订单列表页「JSON 写表单」的界面文案（10 个 key，中英成对）。
--
-- 背景：订单列表页的六个写动作（状态流转 / 取消 / 退款 / 备注 + 批量流转 / 批量取消）从
-- 「页面 POST + 服务端提示页」改为前端 fetch 直调 JSON（单条走 /api/order/{status,cancel,
-- refund,note}，批量走 /api/order/{status,cancel} 的**批档形状**：体里有 ids 即批档，
-- 见 internal/module/order/inbound/http/order_bulk_write.go；前端基座
-- static/js/ui/json_write.js）。页面写路由（以及它借 API path 当权限点的那层）随之退役。
--
-- 这批词条全部是**页面自己带下去**的：
--   · 批量结果三句摘要 ×2 组动作（流转 / 取消）—— 前端按 outcome=update 选 all-updated；
--   · 批量条「没勾选任何一条」与结果区「刷新列表」入口 —— 后者刻意不自动刷新，
--     让人先看完「成功 N / 跳过 M + 逐条原因」再决定刷新（自动刷新会让结果一闪而过）；
--   · 批量流转的二次确认（迁移前是直接提交，现在要显式说清「改成上面那个状态」；
--     批量取消沿用它原本就有的 admin.orders.bulkCancelConfirm，不新增）；
--   · 无 JavaScript 时的提示 —— 写动作全部由浏览器发起，禁用 JS 时这些控件是 hidden 的，
--     没有这句话用户只看到「按钮不见了」，会以为是权限问题。
--
-- 为什么不复用页面 POST 时代那几个 key（order.bulk.*，`%s` 位置占位符、服务端 Sprintf 口径）：
-- 结果区文案由 json_write.js 在**浏览器侧**填充，只认**命名**占位符 {count} / {updated} /
-- {skipped}（基座里唯一的 %s 通道是 data-msg-too-many，因为那条句子的成品由服务端给）。
-- 同一个意思要一份句子，但**占位符口径必须跟消费者一致**，所以这里新增同义 key，而不是去改
-- 既有词条的值：sys_i18n 是运营的真相来源，迁移只写默认值（ON CONFLICT DO NOTHING，不覆盖
-- 运营在词条页里的修改）。
--
-- 其余文案**不新增 key**，全部复用既有词条：
--   · 请求失败 → ErrInternal「操作失败，请稍后重试」；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（模板把成品句带进 data 属性，前端按
--     (max, count) 顺序填 %s —— 与 response.translate 的 %s 位置填参同口径）；
--   · 批量取消确认 → admin.orders.bulkCancelConfirm（既有）；
--   · 批量条的目标状态下拉与按钮文字 → admin.orders.bulk.{target,status,cancel}（既有）；
--   · 批量取消缺原因 → order.bulk.cancelReasonRequired（既有枚举 key，由 400 的 message 回给页面）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register 里**逐条枚举**本批的
-- 10 个 item_key 且限 lang IN ('zh-CN','en-US')、阈值取**精确组合数 20**（低于它缺词也能满足，
-- 高于它判据永不满足、每次启动重跑）。前缀 LIKE 会被同期其它批次的行满足而静默跳过
-- （本仓库踩过：226/277/283/592；用例见 public/migrations/recent_write_ui_seed_guard_test.go）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.orders.bulk.noneSelected', 'zh-CN', '没有勾选任何订单，列表未改动。', 200, 'admin', '订单列表页批量结果区：一条都没勾选（json_write.js: summaryText，data-msg-none-selected）', 1, now(), now()),
('admin.orders.bulk.noneSelected', 'en-US', 'No order was selected; the list is unchanged.', 200, 'admin', '订单列表页批量结果区：一条都没勾选（json_write.js: summaryText，data-msg-none-selected）', 1, now(), now()),
('admin.orders.bulk.refresh', 'zh-CN', '刷新列表', 200, 'admin', '订单列表页批量结果区的刷新入口（json_write.js: refreshEntry，data-msg-refresh）', 1, now(), now()),
('admin.orders.bulk.refresh', 'en-US', 'Refresh the list', 200, 'admin', '订单列表页批量结果区的刷新入口（json_write.js: refreshEntry，data-msg-refresh）', 1, now(), now()),
('admin.orders.bulkStatus.allUpdated', 'zh-CN', '已流转 {count} 个订单。', 200, 'admin', '订单列表页批量流转：全部成功摘要（data-msg-all-updated，outcome=update）', 1, now(), now()),
('admin.orders.bulkStatus.allUpdated', 'en-US', 'Transitioned {count} order(s).', 200, 'admin', '订单列表页批量流转：全部成功摘要（data-msg-all-updated，outcome=update）', 1, now(), now()),
('admin.orders.bulkStatus.allSkipped', 'zh-CN', '{count} 个订单都未能流转（状态不允许这条边或已不存在）。', 200, 'admin', '订单列表页批量流转：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.orders.bulkStatus.allSkipped', 'en-US', 'None of the {count} order(s) could be transitioned (the transition is not allowed, or they no longer exist).', 200, 'admin', '订单列表页批量流转：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.orders.bulkStatus.partial', 'zh-CN', '已流转 {updated} 个订单，{skipped} 个未能流转（状态不允许这条边或已不存在）。', 200, 'admin', '订单列表页批量流转：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.orders.bulkStatus.partial', 'en-US', 'Transitioned {updated} order(s); {skipped} could not be transitioned (the transition is not allowed, or they no longer exist).', 200, 'admin', '订单列表页批量流转：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.orders.bulkStatusConfirm', 'zh-CN', '把选中的订单流转成上面选的状态？状态机不允许这条边的会被跳过。', 200, 'admin', '订单列表页批量流转按钮的二次确认（data-confirm）', 1, now(), now()),
('admin.orders.bulkStatusConfirm', 'en-US', 'Apply the status selected above to the chosen orders? Orders where the transition is not allowed will be skipped.', 200, 'admin', '订单列表页批量流转按钮的二次确认（data-confirm）', 1, now(), now()),
('admin.orders.bulkCancel.allUpdated', 'zh-CN', '已取消 {count} 个订单。', 200, 'admin', '订单列表页批量取消：全部成功摘要（data-msg-all-updated，outcome=update）', 1, now(), now()),
('admin.orders.bulkCancel.allUpdated', 'en-US', 'Cancelled {count} order(s).', 200, 'admin', '订单列表页批量取消：全部成功摘要（data-msg-all-updated，outcome=update）', 1, now(), now()),
('admin.orders.bulkCancel.allSkipped', 'zh-CN', '{count} 个订单都未能取消（已发货 / 已完成或已不存在）。', 200, 'admin', '订单列表页批量取消：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.orders.bulkCancel.allSkipped', 'en-US', 'None of the {count} order(s) could be cancelled (already shipped/completed, or they no longer exist).', 200, 'admin', '订单列表页批量取消：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.orders.bulkCancel.partial', 'zh-CN', '已取消 {updated} 个订单，{skipped} 个未能取消（已发货 / 已完成或已不存在）。', 200, 'admin', '订单列表页批量取消：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.orders.bulkCancel.partial', 'en-US', 'Cancelled {updated} order(s); {skipped} could not be cancelled (already shipped/completed, or they no longer exist).', 200, 'admin', '订单列表页批量取消：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.orders.write.noJs', 'zh-CN', '本页的流转、取消、退款、备注与两个批量动作需要 JavaScript：写操作由浏览器直接调用接口完成。请开启 JavaScript 后重试。', 200, 'admin', '订单列表页 noscript 提示（列表页 <noscript>）', 1, now(), now()),
('admin.orders.write.noJs', 'en-US', 'Transitioning, cancelling, refunding, saving notes and the two bulk actions on this page require JavaScript: the browser calls the API directly. Please enable JavaScript and retry.', 200, 'admin', '订单列表页 noscript 提示（列表页 <noscript>）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
