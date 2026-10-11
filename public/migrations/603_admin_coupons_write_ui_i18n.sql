-- 603 · 优惠码页「JSON 写表单」的界面文案（10 个 key，中英成对）。
--
-- 背景：优惠码页的新建 / 修改（含停用启用）/ 删除 / 批量删除 / 批量启停从「页面 POST +
-- 服务端提示页」改为前端 fetch 直调 JSON（端点 /api/order/coupon/{create,update,delete}，
-- 见 internal/module/order/inbound/http/coupon_write.go；前端基座 static/js/ui/json_write.js）。
-- 页面写路由（以及它借 API path 当权限点的那层）随之退役。这批词条全部是**页面自己带下去**的：
--   · 批量结果三句摘要 ×2 组动作（删除 / 启停）—— 前端按 outcome 选 all-deleted / all-updated；
--   · 批量条「没勾选任何一条」与结果区「刷新列表」入口 —— 后者刻意不自动刷新，
--     让人先看完「成功 N / 跳过 M + 逐条原因」再决定刷新（自动刷新会让结果一闪而过）；
--   · 批量启停的二次确认（原先把 status 当表单字段提交，现在要显式说清「改成上面那个状态」）；
--   · 无 JavaScript 时的提示 —— 写动作全部由浏览器发起，禁用 JS 时这些控件是 hidden 的，
--     没有这句话用户只看到「按钮不见了」，会以为是权限问题。
--
-- 为什么不复用页面 POST 时代那几个 key（admin.coupons.bulk.* / order.bulk.*）：那些句子用的是
-- **位置**占位符（`%s`，服务端 Sprintf 口径，见 internal/module/order/inbound/http/order_shared.go
-- 的 bulkSummary），而结果区文案由 json_write.js 在**浏览器侧**填充，只认**命名**占位符
-- {count} / {deleted} / {updated} / {skipped}（基座里唯一的 %s 通道是 data-msg-too-many，
-- 因为那条句子的成品由服务端给：见 593 与 595 的说明）。同一个意思要一份句子，但**占位符口径
-- 必须跟消费者一致**，所以这里新增同义 key，而不是去改既有词条的值：sys_i18n 是运营的真相
-- 来源，迁移只写默认值（ON CONFLICT DO NOTHING，不覆盖运营在词条页里的修改）。
--
-- 其余文案**不新增 key**，全部复用既有词条：
--   · 请求失败 → ErrInternal「操作失败，请稍后重试」；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（模板把成品句带进 data 属性，
--     前端按 (max, count) 顺序填 %s —— 与 response.translate 的 %s 位置填参同口径）；
--   · 批量删除确认 → admin.coupons.bulkDeleteConfirm（434 已 seed）；
--   · 单条删除确认 → admin.coupons.list.deleteConfirm；
--   · 批量条的目标状态下拉、按钮文字 → admin.coupons.bulk.{target,toggle,delete}（434 已 seed）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register 里**逐条枚举**本批的
-- 10 个 item_key 且限 lang IN ('zh-CN','en-US')、阈值取**精确组合数 20**（低于它缺词也能满足，
-- 高于它判据永不满足、每次启动重跑）。前缀 LIKE 会被同期其它批次的行满足而静默跳过
-- （本仓库踩过：226/277/283/592；用例见 public/migrations/recent_write_ui_seed_guard_test.go）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.coupons.bulk.noneSelected', 'zh-CN', '没有勾选任何优惠码，列表未改动。', 200, 'admin', '优惠码页批量结果区：一条都没勾选（json_write.js: summaryText，data-msg-none-selected）', 1, now(), now()),
('admin.coupons.bulk.noneSelected', 'en-US', 'No coupon was selected; the list is unchanged.', 200, 'admin', '优惠码页批量结果区：一条都没勾选（json_write.js: summaryText，data-msg-none-selected）', 1, now(), now()),
('admin.coupons.bulk.refresh', 'zh-CN', '刷新列表', 200, 'admin', '优惠码页批量结果区的刷新入口（json_write.js: refreshEntry，data-msg-refresh）', 1, now(), now()),
('admin.coupons.bulk.refresh', 'en-US', 'Refresh the list', 200, 'admin', '优惠码页批量结果区的刷新入口（json_write.js: refreshEntry，data-msg-refresh）', 1, now(), now()),
('admin.coupons.bulkDelete.allDeleted', 'zh-CN', '已删除 {count} 个优惠码。', 200, 'admin', '优惠码页批量删除：全部成功摘要（data-msg-all-deleted）', 1, now(), now()),
('admin.coupons.bulkDelete.allDeleted', 'en-US', 'Deleted {count} coupon(s).', 200, 'admin', '优惠码页批量删除：全部成功摘要（data-msg-all-deleted）', 1, now(), now()),
('admin.coupons.bulkDelete.allSkipped', 'zh-CN', '{count} 个优惠码都未能删除（不存在或已有核销记录）。', 200, 'admin', '优惠码页批量删除：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.coupons.bulkDelete.allSkipped', 'en-US', 'None of the {count} coupon(s) could be deleted (missing, or they already have redemptions).', 200, 'admin', '优惠码页批量删除：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.coupons.bulkDelete.partial', 'zh-CN', '已删除 {deleted} 个优惠码，{skipped} 个未能删除（不存在或已有核销记录）。', 200, 'admin', '优惠码页批量删除：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.coupons.bulkDelete.partial', 'en-US', 'Deleted {deleted} coupon(s); {skipped} could not be deleted (missing, or they already have redemptions).', 200, 'admin', '优惠码页批量删除：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.coupons.bulkToggle.allUpdated', 'zh-CN', '已更新 {count} 个优惠码。', 200, 'admin', '优惠码页批量启停：全部成功摘要（data-msg-all-updated）', 1, now(), now()),
('admin.coupons.bulkToggle.allUpdated', 'en-US', 'Updated {count} coupon(s).', 200, 'admin', '优惠码页批量启停：全部成功摘要（data-msg-all-updated）', 1, now(), now()),
('admin.coupons.bulkToggle.allSkipped', 'zh-CN', '{count} 个优惠码都未能更新（不存在或不属于当前站点）。', 200, 'admin', '优惠码页批量启停：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.coupons.bulkToggle.allSkipped', 'en-US', 'None of the {count} coupon(s) could be updated (missing, or they belong to another site).', 200, 'admin', '优惠码页批量启停：全部跳过摘要（data-msg-all-skipped）', 1, now(), now()),
('admin.coupons.bulkToggle.partial', 'zh-CN', '已更新 {updated} 个优惠码，{skipped} 个未能更新（不存在或不属于当前站点）。', 200, 'admin', '优惠码页批量启停：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.coupons.bulkToggle.partial', 'en-US', 'Updated {updated} coupon(s); {skipped} could not be updated (missing, or they belong to another site).', 200, 'admin', '优惠码页批量启停：部分成功摘要（data-msg-partial）', 1, now(), now()),
('admin.coupons.bulkToggleConfirm', 'zh-CN', '把选中的优惠码改成上面选的状态？', 200, 'admin', '优惠码页批量启停按钮的二次确认（data-confirm）', 1, now(), now()),
('admin.coupons.bulkToggleConfirm', 'en-US', 'Apply the status selected above to the chosen coupons?', 200, 'admin', '优惠码页批量启停按钮的二次确认（data-confirm）', 1, now(), now()),
('admin.coupons.write.noJs', 'zh-CN', '本页的新建、修改、删除与批量动作需要 JavaScript：写操作由浏览器直接调用接口完成。请开启 JavaScript 后重试。', 200, 'admin', '优惠码页 noscript 提示（新建/编辑/列表三个片段的 <noscript>）', 1, now(), now()),
('admin.coupons.write.noJs', 'en-US', 'Creating, editing, deleting and bulk actions on this page require JavaScript: the browser calls the API directly. Please enable JavaScript and retry.', 200, 'admin', '优惠码页 noscript 提示（新建/编辑/列表三个片段的 <noscript>）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
