-- 595 · 词条页「JSON 写表单」的界面文案（5 个 key，中英成对）。
--
-- 背景：词条页的新建 / 编辑 / 删除 / 批量删除从「页面 POST + 服务端跳转页」改为前端 fetch
-- 直调 JSON 端点 POST /api/i18n/save（internal/templates/static/js/ui/json_write.js，
-- 端点实现见 internal/module/admin/inbound/http/admin_i18n_write.go）。页面写动作与
-- 整页提示页一起退役后，有两条文案由页面自己带下去，另有批量结果的三句摘要：
--   · 批量删除结果区里的「刷新列表」入口 —— 刻意**不自动刷新**，让人先看完
--     「已删 N / 跳过 M + 逐条原因」再决定刷新（自动刷新会让结果一闪而过）；
--   · 无 JavaScript 时的提示 —— 写动作全部由浏览器发起，禁用 JS 时这些控件是 hidden 的，
--     没有这句话用户只看到「按钮不见了」，会以为是权限问题；
--   · 批量结果三句摘要（全成功 / 全跳过 / 部分成功）。
--
-- 为什么不复用 283 的 admin.i18nBulk.*：那三句用的是**位置**占位符（`%s`，服务端 Sprintf
-- 口径），而结果区文案由 json_write.js 在浏览器侧填充，只认**命名**占位符 {count} /
-- {deleted} / {skipped}（基座里唯一的 %s 通道 data-msg-too-many 是给批量上限那条用的，
-- 见 593 的说明）。同一个意思要一份句子，但**占位符口径必须跟消费者一致**，所以这里新增
-- 三个同义 key，而不是去改既有词条的值：sys_i18n 是运营的真相来源，迁移只写默认值
-- （DO NOTHING，不覆盖运营在词条页里的修改）—— 改值会破坏这条约定。
-- 遗留：283 的 admin.i18nBulk.{allDeleted,allSkipped,partial} 随页面 POST 的实现一起失去
-- 消费者（Go 侧的 adminI18nBulk* 助手已删），成为孤儿词条；清理它们要单独一条迁移
-- （本批不动既有行），已记入本任务回报。
--
-- 其余文案**不新增 key**，全部复用既有词条：
--   · none-selected → admin.i18nBulk.noneSelected（无占位符，可原样复用）；
--   · 请求失败 → ErrInternal「操作失败，请稍后重试」；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（模板把成品句带进 data 属性，
--     前端按 (max, count) 顺序填 %s —— 与 response.translate 的 %s 位置填参同口径）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在 register 里**逐条枚举**
-- 本批的 5 个 item_key（上界封闭）——前缀 LIKE 会被同期其它批次的行满足而静默跳过，
-- 全库总量则会让本批每次启动重跑（本仓库两种都踩过，理由见 226/277/283/592）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.i18nWrite.bulkResult.allDeleted', 'zh-CN', '已删除 {count} 条词条（构建时回退到组件包内的中文兜底）。', 200, 'admin', '词条页批量删除结果区的全部成功摘要（json_write.js: summaryText，data-msg-all-deleted）', 1, now(), now()),
('admin.i18nWrite.bulkResult.allDeleted', 'en-US', 'Deleted {count} entry(ies) (the build falls back to the built-in Chinese defaults).', 200, 'admin', '词条页批量删除结果区的全部成功摘要（json_write.js: summaryText，data-msg-all-deleted）', 1, now(), now()),
('admin.i18nWrite.bulkResult.allSkipped', 'zh-CN', '{count} 条词条都未能删除，列表未改动。', 200, 'admin', '词条页批量删除结果区的全部跳过摘要（json_write.js: summaryText，data-msg-all-skipped）', 1, now(), now()),
('admin.i18nWrite.bulkResult.allSkipped', 'en-US', 'None of the {count} entry(ies) could be deleted; the list is unchanged.', 200, 'admin', '词条页批量删除结果区的全部跳过摘要（json_write.js: summaryText，data-msg-all-skipped）', 1, now(), now()),
('admin.i18nWrite.bulkResult.partial', 'zh-CN', '已删除 {deleted} 条，{skipped} 条未能删除（可能已被删除）。', 200, 'admin', '词条页批量删除结果区的部分成功摘要（json_write.js: summaryText，data-msg-partial）', 1, now(), now()),
('admin.i18nWrite.bulkResult.partial', 'en-US', 'Deleted {deleted}, {skipped} could not be deleted (possibly already deleted).', 200, 'admin', '词条页批量删除结果区的部分成功摘要（json_write.js: summaryText，data-msg-partial）', 1, now(), now()),
('admin.i18nWrite.refresh', 'zh-CN', '刷新列表', 200, 'admin', '词条页批量删除结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.i18nWrite.refresh', 'en-US', 'Refresh list', 200, 'admin', '词条页批量删除结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.i18nWrite.noJs', 'zh-CN', '本页的新建、编辑与删除需要 JavaScript：写操作由浏览器直接调用接口完成。请开启 JavaScript 后重试。', 200, 'admin', '词条页禁用 JS 时的提示（i18n.html 与 i18n_edit_form.html 的 noscript）', 1, now(), now()),
('admin.i18nWrite.noJs', 'en-US', 'Creating, editing and deleting entries on this page requires JavaScript: the write actions are issued by the browser against the API. Please enable JavaScript and try again.', 200, 'admin', '词条页禁用 JS 时的提示（i18n.html 与 i18n_edit_form.html 的 noscript）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
