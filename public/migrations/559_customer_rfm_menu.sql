-- 559 · 客户目录新增「RFM 分析」菜单（sys_menus）
--
-- 背景：P7 的客户目录在 555 里建好（一级分组「客户」+ 客户概览 + 客户列表），
--       本批补上第三项 RFM 分析（GET /admin/customers/rfm）。
--
-- 为什么不补进 555：555 的 ConditionSQL 在已经执行过它的库上判为「完成」并**直接跳过**，
-- 所以往 555 里加一行在那些库上永远不会生效（而在新库上会）—— 这正是
-- AGENTS.md 记的「幂等条件让跳过看起来像通过」。新增对象一律新开一个迁移。
--
-- 列清单照抄 555 的最后一处 INSERT（那一条已在真库跑通过）：sys_menus 上叫
-- is_hidden / is_public，**没有 visible 这个列** —— 凭记忆写 schema 的代价是一次
-- `column "visible" does not exist`，而这条只在真跑时才暴露（mustSQL 只校验文件存在，
-- Go 编译更看不到）。status 留默认值（555 也没写）。
--
-- 权限点复用 user:customer_list：RFM 是同一张用户表的另一种看法，
-- 与客户概览页同一条口径（同一份数据的两种看法不是两个权限）。

INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT 'RFM 分析',
       COALESCE((SELECT p.id FROM sys_menus p
                  WHERE p.title = '客户' AND p.type = 1 AND p.deleted_at IS NULL
                  ORDER BY p.id LIMIT 1), 0),
       2, '/admin/customers/rfm', '', 'user:customer_list', 1, 3, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus m
                   WHERE m.type = 2 AND m.path = '/admin/customers/rfm' AND m.deleted_at IS NULL);
