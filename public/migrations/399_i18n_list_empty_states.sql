-- 399 · 管理域四个列表页的空态文案（administrators / roles / permissions / datarules）
--
-- 缺陷：四页的空态只有一句「还没有 X，点右上「新建 X」创建一个。」—— 它**靠文字指路而不是给按钮**，
--       用户读完还得自己去右上角找，而右上角那个按钮在无权限时不渲染（文字指的是一条不存在的路）。
--       本轮把四页空态统一成 .empty-title（陈述状态）+ .empty-desc（一句引导）+ .empty-actions（主按钮）。
--
-- 为什么必须有一条迁移而不是只改模板：**模板里的中文只是 t() 兜底，词条命中时显示的是库里的值**。
--   193 已 seed 过 admin.admins.empty / admin.roles.empty / admin.permissions.empty /
--   admin.datarules.empty，而 193 用的是 INSERT ... ON CONFLICT (item_key, lang) DO NOTHING ——
--   seed 是「默认值来源」不是「真相来源」（设计如此：运营在后台改过的词条不能被部署静默回滚）。
--   于是改模板文案对存量库是 no-op，页面继续显示「点右上…」。与 317 / 316 同一手法：
--   新增标题词条 + UPDATE 旧描述词条，历史迁移（193）保持原样。
--
-- 词条分工（与 pages.html 的既有形态一致：admin.pages.empty.title 作标题、admin.pages.empty 作描述）：
--   admin.<mod>.empty.title  新增 —— 空态标题，陈述状态，不含「点右上」
--   admin.<mod>.empty        保留在用 —— 空态描述，改成一句真实引导
-- 旧 key 继续被模板取用，故**不删除、不退役**（删了会让 groupF 的 i18n 双向判据把词条算成孤儿）。
--
-- 幂等：INSERT ... ON CONFLICT DO NOTHING；UPDATE 一律带 item_value = <旧默认值> 前置条件 ——
--   只修正「还是旧默认值」的行，运营在后台手工改过的词条不动；重复执行影响 0 行。
--   判定写法：ConditionSQL 由迁移器 db.Raw 直接执行，没有参数替换，判定用的值只能写进 SQL 字面量。

-- 1) 空态标题词条（4 key × 2 语言）
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.admins.empty.title', 'zh-CN', '还没有管理员', 200, 'admin', 'admin/administrators.html: 空态标题', 1, now(), now()),
('admin.admins.empty.title', 'en-US', 'No administrators yet', 200, 'admin', 'admin/administrators.html: 空态标题', 1, now(), now()),
('admin.roles.empty.title', 'zh-CN', '还没有角色', 200, 'admin', 'admin/roles.html: 空态标题', 1, now(), now()),
('admin.roles.empty.title', 'en-US', 'No roles yet', 200, 'admin', 'admin/roles.html: 空态标题', 1, now(), now()),
('admin.permissions.empty.title', 'zh-CN', '还没有权限点', 200, 'admin', 'admin/permissions.html: 空态标题', 1, now(), now()),
('admin.permissions.empty.title', 'en-US', 'No permissions yet', 200, 'admin', 'admin/permissions.html: 空态标题', 1, now(), now()),
('admin.datarules.empty.title', 'zh-CN', '还没有数据规则', 200, 'admin', 'admin/datarules.html: 空态标题', 1, now(), now()),
('admin.datarules.empty.title', 'en-US', 'No data rules yet', 200, 'admin', 'admin/datarules.html: 空态标题', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 2) 旧描述词条：删掉「点右上…」的文字指路，换成一句描述这个列表能干什么的引导
--    （标题已经说了「还没有 X」，描述不再重复，只回答「建了它有什么用 / 下一步是什么」）

UPDATE sys_i18n
SET item_value = '创建后可在「角色」里分配权限；停用或封禁某个账号，直接在列表里操作。',
    update_time = now()
WHERE item_key = 'admin.admins.empty'
  AND lang = 'zh-CN'
  AND item_value = '还没有管理员，点右上「新建管理员」创建一个。';

UPDATE sys_i18n
SET item_value = 'Assign roles after creating one; to disable or ban an account, use the list directly.',
    update_time = now()
WHERE item_key = 'admin.admins.empty'
  AND lang = 'en-US'
  AND item_value = 'No administrators yet. Click "New administrator" in the top right to create one.';

UPDATE sys_i18n
SET item_value = '角色决定管理员能看到哪些菜单、能调用哪些接口。',
    update_time = now()
WHERE item_key = 'admin.roles.empty'
  AND lang = 'zh-CN'
  AND item_value = '还没有角色，点右上「新建角色」创建一个。';

UPDATE sys_i18n
SET item_value = 'A role decides which menus an administrator sees and which APIs they may call.',
    update_time = now()
WHERE item_key = 'admin.roles.empty'
  AND lang = 'en-US'
  AND item_value = 'No roles yet. Click "New role" in the top right to create one.';

UPDATE sys_i18n
SET item_value = '权限点由路由注册处声明并在启动期自动同步；只有补漏时才需要手工新建。',
    update_time = now()
WHERE item_key = 'admin.permissions.empty'
  AND lang = 'zh-CN'
  AND item_value = '还没有权限点，点右上「新建权限点」创建一个。';

UPDATE sys_i18n
SET item_value = 'Permission codes are declared at route registration and synced automatically at startup; create one by hand only to cover an extra API.',
    update_time = now()
WHERE item_key = 'admin.permissions.empty'
  AND lang = 'en-US'
  AND item_value = 'No permissions yet. Click "New permission" in the top right to create one.';

UPDATE sys_i18n
SET item_value = '规则按数据域限制可见的行与字段；建好后在编辑页配置屏蔽字段与过滤条件。',
    update_time = now()
WHERE item_key = 'admin.datarules.empty'
  AND lang = 'zh-CN'
  AND item_value = '还没有数据规则，点右上「新建规则」创建一个。';

UPDATE sys_i18n
SET item_value = 'A rule limits which rows and fields a data domain returns; configure omitted fields and conditions on its edit page.',
    update_time = now()
WHERE item_key = 'admin.datarules.empty'
  AND lang = 'en-US'
  AND item_value = 'No data rules yet. Click "New rule" in the top right to create one.';
