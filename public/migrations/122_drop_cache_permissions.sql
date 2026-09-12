-- 122_drop_cache_permissions.sql
-- 删除库存缓存相关的权限点与策略（issue #32）。
--
-- 迁移 104 为「缓存同步 / 对账」两个端点建了权限点与 casbin 策略，端点随缓存一起去掉了，
-- 留着会变成指向不存在路由的死授权（后台勾选后毫无作用，误导配置者）。
--
-- 顺序：先删策略（引用权限点对应的 path/method），再删权限点本身。
DELETE FROM sys_casbin_rule
 WHERE ptype = 'p'
   AND ((v1 = '/api/inventory/cache/sync' AND v2 = 'POST')
        OR (v1 = '/api/inventory/cache/reconcile' AND v2 = 'POST'));

DELETE FROM sys_permission
 WHERE permission_code IN ('inventory:cache_sync', 'inventory:cache_reconcile');
