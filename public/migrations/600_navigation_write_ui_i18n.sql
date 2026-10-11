-- 600 · 导航菜单页「JSON 写表单」的界面文案（2 个 key，中英成对）。
--
-- 背景：导航菜单页的新建 / 单条删除 / 批量删除改为前端直调真实 JSON 端点
-- （POST /api/navigation/create、POST /api/navigation/delete，见
-- internal/module/navigation/inbound/http/navigation_delete_write.go），结论不再由服务端
-- 渲染成整页提示页，于是「禁用 JS 时怎么办」与「结果区的刷新入口」这两句话必须由页面自己带下去
-- （internal/templates/admin/navigation/navigations.html 的 noscript 与 data-msg-refresh）。
--
-- 不新增的 key（复用既有词条，逐条核过中英成对、占位符形态也对得上）：
--   · 批量结果四句 → admin.navigations.bulkResult.{noneSelected,allDeleted,allSkipped,partial}
--     （迁移 451，命名占位符 {count}/{deleted}/{skipped}，由 json_write.js 的 fill 填参；
--     与迁移前页面提示页时代**同一批句子**，本次迁移没有改口径 —— 换承载形态不改结论用词）；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（迁移 277，%s 位置占位符：上限、本次条数；
--     基座 fillTooMany 按同一顺序填，与 response.translate 的 %s 位置填参同口径）；
--   · 请求失败 → ErrInternal（迁移 268，中英成对）。
--
-- 为什么 noJs 不能复用 block / 词条页 / 联系人页的那三条同名提示：那三句各自点名了本页的动作
-- 范围（「本页的新建与编辑」「本页的新建、编辑与删除与批量动作」），导航页这一档只有新建、
-- 单条删除与批量删除需要 JS（update / move / panel 仍是页面 POST，无 JS 可用）——
-- 借过来会让用户以为「改标题也要开 JS」而去开一个并不需要的开关。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— 不覆盖运营在后台改过的词条；
-- 门槛判据在 register 里**逐条枚举**本批 2 个 item_key × 两种语言（上界封闭）：前缀 LIKE 会被
-- 同期其它批次的行满足而静默跳过，只数中文那两行则会在「英文缺行」时静默跳过（本仓库两种都踩过，
-- 见 226/277/283/592/598）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.navigations.write.noJs', 'zh-CN', '本页的新建与删除需要 JavaScript：写操作由浏览器直接调用接口完成。请开启 JavaScript 后重试。', 200, 'admin', '导航菜单页禁用 JS 时的提示（navigations.html 的 noscript，只点名新建与删除）', 1, now(), now()),
('admin.navigations.write.noJs', 'en-US', 'Creating and deleting items on this page requires JavaScript: the write actions are issued by the browser against the API. Please enable JavaScript and try again.', 200, 'admin', '导航菜单页禁用 JS 时的提示（navigations.html 的 noscript，只点名新建与删除）', 1, now(), now()),
('admin.navigations.write.refresh', 'zh-CN', '刷新列表', 200, 'admin', '导航菜单页批量结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now()),
('admin.navigations.write.refresh', 'en-US', 'Refresh list', 200, 'admin', '导航菜单页批量结果区的刷新入口（json_write.js: refreshEntry）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
