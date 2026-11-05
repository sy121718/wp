-- 744 · 修正确认过的旧映射：block:delete 指向批删路径
--
-- 背景：双码合并期，启动反写（SyncToDB）会在同一 code 的两条历史路径里挑一条写回，
-- 库可能停在 /api/block/bulk-delete。启动反写已删除，漂移不能再靠进程启动修正。
-- 这里把**已确认的旧映射**显式修一次：权限点行与既有策略行一起对齐到 canonical 路径。
--
-- 只在检测到漂移时执行（CheckSQL），不对其它权限点做任何改写：
-- 其余漂移由启动核对报出（实例不就绪），由人工或后续版本迁移处理，
-- 不在每次启动时批量重写用户可能自定义过的定义。
-- ========================================

-- 1) 权限点行回到 canonical 路径
UPDATE sys_permission
SET api_path = '/api/block/delete', api_method = 'POST', update_time = NOW()
WHERE permission_code = 'block:delete'
  AND (api_path <> '/api/block/delete' OR api_method <> 'POST');

UPDATE sys_permission
SET api_path = '/api/block/bulk-delete', api_method = 'POST', update_time = NOW()
WHERE permission_code = 'block:bulk_delete'
  AND (api_path <> '/api/block/bulk-delete' OR api_method <> 'POST');

-- 2) 已有策略行对齐到权限点当前路径（只动这两条码，其它授权一行不碰）
UPDATE sys_casbin_rule r
SET v1 = p.api_path, v2 = p.api_method
FROM sys_permission p
WHERE r.ptype = 'p'
  AND r.v3 = p.permission_code
  AND r.v3 IN ('block:delete', 'block:bulk_delete')
  AND (r.v1 <> p.api_path OR r.v2 <> p.api_method);
