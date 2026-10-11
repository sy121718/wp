-- 598 · 联系人页「JSON 写表单」的界面文案（10 个 key，中英成对）。
--
-- 背景：联系人页的新建 / 编辑 / 单条删除 / 批量删除 / 批量打标签改为前端 fetch 直调真实 JSON
-- 端点（POST /api/mail/contact/{save,delete,tag}，见 internal/module/mail/inbound/http/mail_contact_write.go），
-- 结论不再由服务端渲染成整页提示页，于是「就地结果区」那几句话必须由页面自己带下去。
--
-- 为什么这些 key 不能复用既有词条（逐条给过判据，避免同一个意思两份句子、也避免一句错话）：
--   · 通用批量句那批（迁移 283 的 admin.bulk.done / admin.bulk.partial）是 **%s 位置占位符**，
--     由服务端 Sprintf 填参；浏览器侧只认命名占位符（{count}/{deleted}/{skipped}），
--     带过去会显示字面量 "%s"（错得安静：没有异常、没有日志）；
--   · 那批的名词表只有管理员 / 角色 / 权限点 / 菜单 / 部门 / 数据规则，**没有「联系人」**；
--   · block / i18n 页的同位 key（admin.blocks.bulkResult.*、admin.i18nWrite.*）说的是块与词条，
--     借过来会对着联系人念「已删除 N 个块」。
--
-- 「打标签」那一组刻意用 **updated / 已更新** 而不是 deleted / 已删除：打标签不是破坏性动作，
-- 基座按 data-write-outcome="update" 读 data.updated、且**不移除任何行**；把成功说成「已删除」
-- 是假话（联系人还在），所以这里连兜底句都不借用删除口径（缺配置时基座宁可不给句子，
-- 只列逐条原因与刷新入口）。
--
-- 不新增的 key（复用既有词条，避免同一意思两份句子）：
--   · 请求失败 → ErrInternal「操作失败，请稍后重试」（迁移 451 已 seed）；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（模板把成品句带进 data 属性，前端按 (max, count)
--     顺序填 %s —— 与 response.translate 的 %s 位置填参同口径，这也是基座 fillTooMany 的由来）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— 不覆盖运营在后台改过的词条；
-- 门槛判据在 register 里**逐条枚举**本批 10 个 item_key（上界封闭）：前缀 LIKE 会被同期其它
-- 批次的行满足而静默跳过，全库总量会让本批每次启动重跑（本仓库两种都踩过，见 226/277/283/592）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.mail.contacts.bulk.noneSelected', 'zh-CN', '没有勾选任何联系人，列表未改动。', 200, 'admin', '联系人页批量动作未勾选任何行时的提示（json_write.js: data-msg-none-selected）', 1, now(), now()),
('admin.mail.contacts.bulk.noneSelected', 'en-US', 'No contact was selected; the list is unchanged.', 200, 'admin', '联系人页批量动作未勾选任何行时的提示（json_write.js: data-msg-none-selected）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.allDeleted', 'zh-CN', '已删除 {count} 位联系人。', 200, 'admin', '联系人页批量删除结果区（json_write.js: outcome=delete 的 all-deleted）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.allDeleted', 'en-US', 'Deleted {count} contact(s).', 200, 'admin', '联系人页批量删除结果区（json_write.js: outcome=delete 的 all-deleted）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.allSkipped', 'zh-CN', '{count} 位联系人都未能删除（可能已被删除）。', 200, 'admin', '联系人页批量删除结果区（全部失败）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.allSkipped', 'en-US', 'None of the {count} contact(s) could be deleted (they may already be gone).', 200, 'admin', '联系人页批量删除结果区（全部失败）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.partial', 'zh-CN', '已删除 {deleted} 位联系人，{skipped} 位未能删除（可能已被删除）。', 200, 'admin', '联系人页批量删除结果区（部分成功）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.partial', 'en-US', 'Deleted {deleted} contact(s); {skipped} could not be deleted (they may already be gone).', 200, 'admin', '联系人页批量删除结果区（部分成功）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.refresh', 'zh-CN', '刷新列表', 200, 'admin', '联系人页批量结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.mail.contacts.bulkDelete.refresh', 'en-US', 'Refresh list', 200, 'admin', '联系人页批量结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.mail.contacts.bulkTag.allUpdated', 'zh-CN', '已为 {count} 位联系人更新标签。', 200, 'admin', '联系人页批量打标签结果区（json_write.js: outcome=update 的 all-updated）', 1, now(), now()),
('admin.mail.contacts.bulkTag.allUpdated', 'en-US', 'Updated tags for {count} contact(s).', 200, 'admin', '联系人页批量打标签结果区（json_write.js: outcome=update 的 all-updated）', 1, now(), now()),
('admin.mail.contacts.bulkTag.allSkipped', 'zh-CN', '{count} 位联系人都未能更新标签（可能已被删除）。', 200, 'admin', '联系人页批量打标签结果区（全部失败）', 1, now(), now()),
('admin.mail.contacts.bulkTag.allSkipped', 'en-US', 'Tags were not updated for any of the {count} contact(s) (they may already be gone).', 200, 'admin', '联系人页批量打标签结果区（全部失败）', 1, now(), now()),
('admin.mail.contacts.bulkTag.partial', 'zh-CN', '已为 {updated} 位联系人更新标签，{skipped} 位未能更新（可能已被删除）。', 200, 'admin', '联系人页批量打标签结果区（部分成功）', 1, now(), now()),
('admin.mail.contacts.bulkTag.partial', 'en-US', 'Updated tags for {updated} contact(s); {skipped} could not be updated (they may already be gone).', 200, 'admin', '联系人页批量打标签结果区（部分成功）', 1, now(), now()),
('admin.mail.contacts.bulkTag.refresh', 'zh-CN', '刷新列表', 200, 'admin', '联系人页批量结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.mail.contacts.bulkTag.refresh', 'en-US', 'Refresh list', 200, 'admin', '联系人页批量结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.mail.contacts.write.noJs', 'zh-CN', '本页的新建、编辑、删除与批量动作需要 JavaScript：写操作由浏览器直接调用接口完成。请开启 JavaScript 后重试。', 200, 'admin', '联系人页禁用 JS 时的提示（mail_contacts.html 的 noscript）', 1, now(), now()),
('admin.mail.contacts.write.noJs', 'en-US', 'Creating, editing, deleting and bulk actions on this page require JavaScript: the write actions are issued by the browser against the API. Please enable JavaScript and try again.', 200, 'admin', '联系人页禁用 JS 时的提示（mail_contacts.html 的 noscript）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
