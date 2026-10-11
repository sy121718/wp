-- 601 · 导航菜单页「来源批量加入菜单」的结果文案（3 个 key，中英成对）。
--
-- 背景：导航菜单页的「从已有内容添加」（来源批量加入）从页面 POST
-- （/admin/navigations/add-source，借 /api/navigation/create 的权限对象）迁到真实端点
-- POST /api/navigation/create 的 action=addSource 档
-- （internal/module/navigation/inbound/http/navigation_add_source_write.go）。
-- 结论不再由服务端渲染成整页提示页，改为回执里的 added/skipped，由 json_write.js 的批量档
-- 就地渲染（internal/templates/admin/navigation/navigations.html 的 data-msg-*）。
--
-- 为什么这三句必须新写、不能复用「删除」那四句（admin.navigations.bulkResult.*，迁移 451）：
-- 那几句的动词与后果都是删除口径（「已删除 N 条词条」「列表未改动」）—— 加入菜单是**非破坏**
-- 动作，句子挂在错的口径上会让用户以为勾中的内容被删了。占位符也不搬：本批刻意用
-- {added}/{skipped}（成功数叫 added，不借用删除的 {deleted}），json_write.js 的
-- summaryText 已同步把 added 填进参数字典。
--
-- 不新增的 key（复用既有词条，逐条核过中英成对、占位符形态也对得上）：
--   · 一条都没勾 → admin.navigations.notice.noSourcePicked（迁移 451，与退役的页面版
--     完全同一句话：「请至少勾选一项要加入菜单的内容。」）；
--   · 候选「暂无公开路径」的逐条原因 → admin.navigations.no_public_path（迁移 192）：
--     抽屉里与回执里说的是同一句话，由服务端按请求语言解析（见 add_source_write.go）；
--   · 批量超 200 条 → shell.err.bulkIdsTooMany（迁移 277，%s 位置占位符：上限、本次条数；
--     基座 fillTooMany 按同一顺序填）；
--   · 请求失败 → ErrInternal（迁移 268，中英成对）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— 不覆盖运营在后台改过的词条；
-- 门槛判据在 register 里**逐条枚举**本批 3 个 item_key × 两种语言（上界封闭）：前缀 LIKE 会被
-- 同期其它批次的行满足而静默跳过，只数中文那几行则会在「英文缺行」时静默跳过（本仓库两种都踩过，
-- 见 226/277/283/592/598/600）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.navigations.addSource.result.allAdded', 'zh-CN', '已加入 {count} 项。', 200, 'admin', '来源批量加入菜单：全部成功（json_write.js 的 all-added 槽，占位符 {count}）', 1, now(), now()),
('admin.navigations.addSource.result.allAdded', 'en-US', 'Added {count} item(s).', 200, 'admin', '来源批量加入菜单：全部成功（json_write.js 的 all-added 槽，占位符 {count}）', 1, now(), now()),
('admin.navigations.addSource.result.allSkipped', 'zh-CN', '{count} 项都未能加入，菜单未改动。', 200, 'admin', '来源批量加入菜单：全部被跳过（占位符 {count}）', 1, now(), now()),
('admin.navigations.addSource.result.allSkipped', 'en-US', 'None of the {count} item(s) could be added; the menu is unchanged.', 200, 'admin', '来源批量加入菜单：全部被跳过（占位符 {count}）', 1, now(), now()),
('admin.navigations.addSource.result.partial', 'zh-CN', '已加入 {added} 项，{skipped} 项未能加入。', 200, 'admin', '来源批量加入菜单：部分成功（占位符 {added}/{skipped}）', 1, now(), now()),
('admin.navigations.addSource.result.partial', 'en-US', 'Added {added}; {skipped} could not be added.', 200, 'admin', '来源批量加入菜单：部分成功（占位符 {added}/{skipped}）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
