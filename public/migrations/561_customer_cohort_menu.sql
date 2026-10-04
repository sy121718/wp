-- 561 · 客户目录新增「群组留存」菜单（sys_menus）
--
-- 背景：客户目录已有 客户列表（/admin/customers）、客户概览（555）、RFM 分析（559），
--       本批补上第四项 Cohort 分析页（GET /admin/customers/cohort）。
--
-- 为什么不补进 559/555：那一批的 ConditionSQL 在已经执行过它的库上判为「完成」并跳过，
-- 往里加一行在那些库上永远不生效。新增对象一律新开迁移（AGENTS.md 的迁移约定）。
--
-- 列清单照抄 555 / 559 的 INSERT（sys_menus 上没有 visible，是 is_hidden / is_public）。
-- 权限点复用 user:customer_list：同一批客户按时间的另一种看法，不是另一份数据。

INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT '群组留存',
       COALESCE((SELECT p.id FROM sys_menus p
                  WHERE p.title = '客户' AND p.type = 1 AND p.deleted_at IS NULL
                  ORDER BY p.id LIMIT 1), 0),
       2, '/admin/customers/cohort', '', 'user:customer_list', 1, 4, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_menus m
                   WHERE m.type = 2 AND m.path = '/admin/customers/cohort' AND m.deleted_at IS NULL);
