-- 586 · 菜单管理树状分页（admin/system/menus.html）的 5 个新词条 + 1 处文案修正。
--
-- 为什么新增词条：列表从「平铺的行分页 + 上级菜单列」改成「树状分页 + 折叠 / 展开」，
-- 出现了三类新文案 —— 搜索态的命中标记与祖先标记、展开 / 折叠全部按钮。
-- 命名与商品分类树（446 迁移）对齐：同一件事在两处用同一套措辞，用户不必学两种说法。
--
-- 为什么加 parent_hint：编辑抽屉里「上级菜单」从 hidden 变成可改的下拉，
-- 于是「不选 = 顶级」和「灰色项为什么不能选」这两件事必须在界面上说清楚，
-- 否则用户面对一个灰掉的自己名字只会以为是下拉坏了。
--
-- 为什么改 filter_placeholder：关键词现在多匹配一个面（权限码），
-- 占位文案不写出来，用户不会知道「按权限点找菜单」这条路存在。
--
-- 5 个 key × 2 语言 = 10 行。修正文案的那 2 行是本批自己的 key（不是别批的），
-- 一并放在这里，免得措辞改动散落在两个迁移里。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.menus.search.matched', 'zh-CN', '匹配', 200, 'admin', 'menu tree matched-row badge', 1, now(), now()),
('admin.menus.search.matched', 'en-US', 'Match', 200, 'admin', 'menu tree matched-row badge', 1, now(), now()),
('admin.menus.search.ancestor', 'zh-CN', '上级路径', 200, 'admin', 'menu tree ancestor context label', 1, now(), now()),
('admin.menus.search.ancestor', 'en-US', 'Ancestor path', 200, 'admin', 'menu tree ancestor context label', 1, now(), now()),
('admin.menus.tree.expandAll', 'zh-CN', '展开全部', 200, 'admin', 'menu tree expand all rows button', 1, now(), now()),
('admin.menus.tree.expandAll', 'en-US', 'Expand all', 200, 'admin', 'menu tree expand all rows button', 1, now(), now()),
('admin.menus.tree.collapseAll', 'zh-CN', '折叠全部', 200, 'admin', 'menu tree collapse all rows button', 1, now(), now()),
('admin.menus.tree.collapseAll', 'en-US', 'Collapse all', 200, 'admin', 'menu tree collapse all rows button', 1, now(), now()),
('admin.menus.field.parent_hint', 'zh-CN', '选「（根菜单）」或留空表示顶级菜单；灰色项是它自己与它的子孙，选了会形成循环。', 200, 'admin', 'menu edit form parent select hint', 1, now(), now()),
('admin.menus.field.parent_hint', 'en-US', 'Choose "(root)" or leave empty for a top-level menu; greyed entries are the menu itself and its descendants — picking one would create a loop.', 200, 'admin', 'menu edit form parent select hint', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- filter_placeholder 在 192 迁移里种下（'筛选：标题 / 路径 / 备注…'）；这里补上权限码这一面。
UPDATE sys_i18n SET item_value = '筛选：标题 / 路径 / 备注 / 权限点…'
 WHERE item_key = 'admin.menus.filter_placeholder' AND lang = 'zh-CN';
UPDATE sys_i18n SET item_value = 'Filter: title / path / remark / permission code…'
 WHERE item_key = 'admin.menus.filter_placeholder' AND lang = 'en-US';

-- 清掉本批早期版本种下的两个 key（页面上的「匹配 N 条；…」提示行已去掉）。
-- 已执行过早期版本的库（含本地开发库）会留下这两行孤儿词条，DELETE 让它们与最终形态一致；
-- 未执行过的库上这是空操作。
DELETE FROM sys_i18n
 WHERE item_key IN ('admin.menus.search.matchCount', 'admin.menus.search.matchSuffix');
