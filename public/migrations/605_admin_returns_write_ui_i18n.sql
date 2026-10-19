-- 605 · 退货入库页「JSON 写表单」的界面文案（3 个 key，中英成对）。
--
-- 背景：退货页的五个写动作（同意 / 拒绝 / 确认收货 / 批量同意 / 批量拒绝）从「页面 POST +
-- 服务端提示页」改为前端 fetch 直调 JSON（单条走 /api/order/return/{approve,reject,receive}，
-- 批量走同一批端点的**批档形状**：体里有 ids 即批档，见
-- internal/module/order/inbound/http/return_bulk_write.go；前端基座 static/js/ui/json_write.js）。
-- 页面写路由（以及它们借 API path 当权限点的那层）随之退役，order 模块的 shell.RenderJump
-- 消费者归零。
--
-- 这批词条全部是**页面自己带下去**的：
--   · 批量条「没勾选任何一条」与结果区「刷新列表」入口；
--   · 批量成功只有一句结论（整批要么全成、要么全败，见 product/order 的整批事务口径）；
--   · 无 JavaScript 时的提示 —— 写动作全部由浏览器发起，禁用 JS 时这些控件是 hidden 的，
--     没有这句话用户只看到「按钮不见了」，会以为是权限问题。
--
-- 为什么不复用页面 POST 时代那几个 key（order.bulk.* 的 %s 位置占位符、服务端 Sprintf 口径）：
-- 结果区文案由 json_write.js 在**浏览器侧**填充，只认**命名**占位符 {count} / {updated} /
-- {skipped}（基座里唯一的 %s 通道是 data-msg-too-many，因为那条句子的成品由服务端给）。
-- 同一个意思要一份句子，但**占位符口径必须跟消费者一致**，所以这里新增同义 key，而不是去改
-- 既有词条的值：sys_i18n 是运营的真相来源，迁移只写默认值（ON CONFLICT DO NOTHING）。
--
-- 其余文案**不新增 key**，全部复用既有词条：
--   · 请求失败 → ErrInternal「操作失败，请稍后重试」；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（模板把成品句带进 data 属性，前端按
--     (max, count) 顺序填 %s —— 与 response.translate 的 %s 位置填参同口径）；
--   · 批量拒绝确认 → admin.returns.bulkRejectConfirm（既有）；
--   · 单条成功**不走文案通道**：create 档成功即整页刷新，状态徽章就是结论（退掉了旧提示页
--     「已同意 / 已收货」那两句按落地态换句的逻辑，见 return_page.go 的迁移注释）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register 里**逐条枚举**本批的
-- 3 个 item_key 且限 lang IN ('zh-CN','en-US')、阈值取**精确组合数 6**（低于它缺词也能满足，
-- 高于它判据永不满足、每次启动重跑）。前缀 LIKE 会被同期其它批次的行满足而静默跳过
-- （本仓库踩过：226/277/283/592；用例见 public/migrations/recent_write_ui_seed_guard_test.go）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.returns.bulk.noneSelected', 'zh-CN', '没有勾选任何退货申请，列表未改动。', 200, 'admin', '退货页批量结果区：一条都没勾选（json_write.js: summaryText，data-msg-none-selected）', 1, now(), now()),
('admin.returns.bulk.noneSelected', 'en-US', 'No return request was selected; the list is unchanged.', 200, 'admin', '退货页批量结果区：一条都没勾选（json_write.js: summaryText，data-msg-none-selected）', 1, now(), now()),
('admin.returns.bulk.refresh', 'zh-CN', '刷新列表', 200, 'admin', '退货页批量结果区的刷新入口（json_write.js: refreshEntry，data-msg-refresh）', 1, now(), now()),
('admin.returns.bulk.refresh', 'en-US', 'Refresh the list', 200, 'admin', '退货页批量结果区的刷新入口（json_write.js: refreshEntry，data-msg-refresh）', 1, now(), now()),
('admin.returns.write.noJs', 'zh-CN', '本页的审核、收货与两个批量动作需要 JavaScript：写操作由浏览器直接调用接口完成。请开启 JavaScript 后重试。', 200, 'admin', '退货页 noscript 提示（列表页 <noscript>）', 1, now(), now()),
('admin.returns.write.noJs', 'en-US', 'Reviewing, receiving and the two bulk actions on this page require JavaScript: the browser calls the API directly. Please enable JavaScript and retry.', 200, 'admin', '退货页 noscript 提示（列表页 <noscript>）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
