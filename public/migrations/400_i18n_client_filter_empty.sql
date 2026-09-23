-- 400 · 客户端筛选「无匹配结果」提示的两条词条（menus / departments）
--
-- 背景：menu 与 department 两页用 [data-filter-input] 做**客户端过滤**，此前过滤到 0 行时
--       页面没有任何提示 —— 用户以为「本来就没数据」或「页面坏了」。
--       admin.js 的 applyFilter 早就实现了 [data-filter-empty] 的显隐契约
--       （`note.hidden = !(terms.length > 0 && shown === 0)`，即「有关键词且无可见行」才提示），
--       但**全站只有这两页用客户端过滤，而两页都没有这个元素** —— 契约存在、消费方缺失。
--       本轮补上元素；元素里的文案必须走 t()，否则英文界面会硬编码中文。
--
-- 为什么必须有一条迁移而不是只改模板：**模板里的中文只是 t() 兜底，词条命中时显示的是库里的值**。
--   seed 一律 INSERT ... ON CONFLICT (item_key, lang) DO NOTHING，是「默认值来源」不是「真相来源」
--   （运营在后台改过的词条不能被部署静默回滚）。与 317 / 399 同一手法。
--
-- 词条语义：只在「输入了关键词但 0 行可见」时显示；本来就没数据时不显示（由 applyFilter 判定）。
--   admin.menus.filter_empty —— 菜单管理页
--   admin.depts.filter_empty —— 部门管理页
--
-- 幂等：ON CONFLICT DO NOTHING；本批**只新增、不修改任何既有词条**，故无 UPDATE。
--   判定写法：ConditionSQL 由迁移器 db.Raw 直接执行，没有参数替换，判定值只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.menus.filter_empty', 'zh-CN', '没有匹配的菜单项。', 200, 'admin', 'admin/menus.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),
('admin.menus.filter_empty', 'en-US', 'No menu items match.', 200, 'admin', 'admin/menus.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),
('admin.depts.filter_empty', 'zh-CN', '没有匹配的部门。', 200, 'admin', 'admin/departments.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now()),
('admin.depts.filter_empty', 'en-US', 'No departments match.', 200, 'admin', 'admin/departments.html: 客户端筛选无结果提示（data-filter-empty）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
