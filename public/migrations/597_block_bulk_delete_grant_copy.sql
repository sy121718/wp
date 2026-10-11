-- 597 · 一次性升级复制：历史 block:delete 主体 → block:bulk_delete
-- ========================================
-- 为什么单独一条、且用 Migration（不是 Seed）：
--   1) 「升级那一刻把谁的删除能力搬过来」是一次性历史数据处理，不是长期定义（长期定义在 594）。
--      写进 Seed 的后果是**每次启动都重放**：RunSeeds 按「此刻谁有 block:delete」重新判定并补齐，
--      于是①运维撤掉某角色的批删权后，下一次启动又被授回；②升级后新授的单删权被自动扩到批删。
--   2) Migration 路径（public/migrations/migrator.go 的 ApplyStatements）把**存在性检查与
--      全部语句放在同一个事务里**，而 Seed 路径只有「条件判定 + Exec」、没有事务包装。
--      复制与标记必须同生共死：复制写了一半失败而标记已落 → 永久少一批授权；标记没写成 →
--      下次启动把已经存在的授权再复制一遍。所以这里 register(Migration{...})，不是 registerSeed。
--
-- 执行标记与顺序：
--   · 标记表 sys_migration_marker 由 **596**（更早的结构迁移）创建 —— 本迁移的 CheckSQL 要查它的行，
--     而 PostgreSQL 在解析期就解析整条 SQL 的 FROM 关系，不能靠 CASE 短路兜住「表还不存在」。
--   · 启动顺序（production：routers/assembly）：先 Run（全部 Migration，含 596 → 597），
--     再 RunSeeds（594 才建权限点行），最后 SyncToDB 对齐路由/策略。所以本迁移执行时
--     **目标权限点行可能还不存在** —— 这不影响复制（复制的产物是策略行；权限点行由 594/SyncToDB 补），
--     但它决定了测试必须按完整顺序跑，不能只跑这一段 SQL。
--
-- 复制判据（**按权限码识别源，不按路径**）：
--   源：ptype='p' AND v3='block:delete' AND v2='POST' AND v1 ∈ 两条已知历史路径
--       （/api/block/delete 与 /api/block/bulk-delete）。
--       · 为什么要收路径：双码合并期 SyncToDB 的 `UPDATE … FROM` 在同一 code 的两条路径里
--         挑一条（PostgreSQL 对多个匹配行的选择不可依赖），库里可能停在 /api/block/bulk-delete
--         —— 只认 canonical 路径会漏掉整整一代主体（升级后他们仍然点不动批删）。
--       · 为什么不是「任意 v1 都算」：v1 是派生列，异常值不构成「这个人有删除权」的证据。
--   主体活着：启用的管理员，或 status=1 且有 g2 active 标记的角色。
--   源权限点被人工停用（status=0）则不复制 —— 停用是人工决定，不借这条路径把能力发出去。
--   目标已存在任一条 block:bulk_delete 策略的主体不再复制（NOT EXISTS 只比 v3、不比路径：
--   路径由 SyncToDB 对齐，按路径判重会在它对齐前后各插一条）。
--   同一主体的多条源行（v4/v5 为 NULL 与空串两种历史形态）由 DISTINCT + 目标判重收敛成一条。
--
-- 运维在升级前把 block:bulk_delete 停用了：复制照做。策略行是「谁有能力」的事实记录；
-- 判定层（Casbin matcher）**不读 sys_permission.status**，所以「停用权限点」并不阻断通道 ——
-- 真要不给，得删策略行。本迁移不改 status，也不会把它重新启用。
--
-- 零源主体（新装库）：一条都不复制，**仍然写入标记** —— 一次性动作算完成，此后不再进出这个判断。
-- ========================================

-- 1) 能力保持：把历史单删主体的删除能力复制到批删权限点（只做一次）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT DISTINCT 'p', r.v0, '/api/block/bulk-delete', 'POST', 'block:bulk_delete'
FROM sys_casbin_rule r
WHERE r.ptype = 'p'
  AND r.v3 = 'block:delete'
  AND r.v2 = 'POST'
  AND r.v1 IN ('/api/block/delete', '/api/block/bulk-delete')
  -- 源权限点必须仍启用：人工停用 block:delete 后不该借这条路径把批删权发出去
  AND EXISTS (SELECT 1 FROM sys_permission p
              WHERE p.permission_code = 'block:delete' AND p.status = 1)
  -- 主体必须是活着的：启用的管理员，或启用且带 g2 标记的角色
  AND (
        EXISTS (SELECT 1 FROM sys_admin sa
                WHERE CAST(sa.id AS VARCHAR) = r.v0 AND sa.status = 1)
     OR EXISTS (SELECT 1 FROM sys_role sr
                WHERE sr.role_code = r.v0 AND sr.status = 1
                  AND EXISTS (SELECT 1 FROM sys_casbin_rule g
                              WHERE g.ptype = 'g2' AND g.v0 = r.v0 AND g.v1 = 'active'))
      )
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule x
      WHERE x.ptype = 'p' AND x.v0 = r.v0 AND x.v3 = 'block:bulk_delete'
  );

-- 2) 标记完成（与上一条语句同事务；零源主体也在此落标记）。
INSERT INTO sys_migration_marker (version, component, note)
SELECT '597-block-bulk-delete-grant-copy', 'permission',
       '把历史 block:delete 主体的删除能力一次性复制到 block:bulk_delete（不随后续授权变动重放；源按 v3 识别，含 canonical 与 bulk 两条历史路径）'
WHERE NOT EXISTS (
    SELECT 1 FROM sys_migration_marker WHERE version = '597-block-bulk-delete-grant-copy'
);

-- 回滚（手工）：DELETE FROM sys_casbin_rule WHERE ptype='p' AND v3='block:bulk_delete'
--              AND v0 IN (<升级前的来源主体>);
--            DELETE FROM sys_migration_marker WHERE version='597-block-bulk-delete-grant-copy';
