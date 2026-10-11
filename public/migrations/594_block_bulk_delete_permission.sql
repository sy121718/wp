-- 594 · 区块批量删除的独立权限点（block:bulk_delete）—— **长期定义**
-- ========================================
-- 背景：块管理页的批量删除改走 JSON API（POST /api/block/bulk-delete）后，这条路由
-- 最初仍声明 block:delete。但那在本仓的权限模型里不成立 —— sys_permission 每个权限点
-- 只有**一列** api_path/api_method，permission.SyncToDB 按路由声明对齐这两列；
-- 同一个 code 挂在两条 API 路径上时，同步只能留下一行，另一条路径上的策略随即失配
-- （超管 403、角色静默失权）。所以批删必须有自己的 code。
--
-- 本迁移只做**长期定义**两件事（都幂等）：
--   1) 补 block:bulk_delete 权限点行（api_path=/api/block/bulk-delete, api_method=POST）；
--   2) 超管策略补缺 —— 与 152/294 同款：权限点与策略分开补会让中间态出现「权限点有了、
--      策略没补」，那种状态下超管点批删就是 403。
--
-- 历史授权的一次性复制**不在这里**，在 597（`597_block_bulk_delete_grant_copy.sql`，标记表由 596 建立）。写进来的后果很具体：
--   RunSeeds 每次启动都会按「此刻谁有 block:delete」重新判定并补一遍，于是
--   ① 运维撤掉某角色的批删权后，下一次启动又被授回；
--   ② 升级后新授的单删权被自动扩到批删 —— 库里长出一个没人审批过的权限。
--
-- 人工列语义（本迁移绝不改）：permission_name / module / remark / status 都是运维可改的。
-- 已存在的权限点行**一个字段都不动**（NOT EXISTS 只判 permission_code）：运维把权限点停用
-- 就保持停用，本迁移不替他把 status 改回 1。api_path/api_method 是派生列，由启动末尾的
-- permission.SyncToDB 按路由声明对齐，也不在这里硬改。
--
-- 与注册条件的一致性（register_block_bulk_delete_permission.go）：条件判「权限点行存在」
-- 并且「**权限点启用期间**超管策略不缺」。所以运维停用权限点后条件照样成立、不再反复重放；
-- 反过来若条件要求「status=1 且 api_path 正确」，就会与这里的 INSERT（只判 code 存在）
-- 永久不一致 —— 行存在但被停用/路径被改时条件永远为 0，这段 SQL 每次启动都重跑一遍。
--
-- 幂等注意（**NULL v4/v5 重复陷阱**）：sys_casbin_rule 的唯一索引含 v4/v5，而历史迁移插入
-- 的行这两列是 NULL —— Postgres 唯一索引里 NULL 互不相等，所以判据里若写 v4 = '' 就永远
-- 匹配不到历史行，重复执行会一行行堆出重复授权。这里与 597 的 NOT EXISTS 都**只比对
-- ptype/v0/v1/v2/v3**，NULL 与空串两种历史形态都能命中。
-- ========================================

-- 1) 权限点（只补缺；已存在则整行不动 —— 运维改过的 name/module/status/remark 全部保留）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT 'block:bulk_delete', '批量删除区块', 'block', '/api/block/bulk-delete', 'POST', 1, 0, NOW(), 0, NOW()
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = 'block:bulk_delete');

-- 2) 超管策略补缺（取权限点**当前**的 api_path/api_method，与 SyncToDB 的补插口径一致）。
--    权限点被运维停用（status=0）时不再补 —— 停用是人工决定，不该被迁移覆盖。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
JOIN sys_permission p
  ON p.permission_code = 'block:bulk_delete' AND p.status = 1
WHERE a.is_admin = 1
  AND a.status = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );

-- 回滚（手工，无自动回滚；顺序与升级相反：先把一次性复制的内容撤掉，再撤长期定义）：
--   1) 撤掉 597 复制出来的历史授权，并删掉它的执行标记（**不删标记**的话，重跑升级时
--      「已执行过一次」仍然成立，那批被删掉的授权不会再被复制回来）：
--        DELETE FROM sys_casbin_rule WHERE ptype = 'p' AND v3 = 'block:bulk_delete'
--          AND v0 IN (<升级前持有 block:delete 的主体>);
--        DELETE FROM sys_migration_marker WHERE version = '597-block-bulk-delete-grant-copy';
--   2) 撤掉本迁移的长期定义（超管策略 + 权限点行）：
--        DELETE FROM sys_casbin_rule WHERE ptype = 'p' AND v3 = 'block:bulk_delete';
--        DELETE FROM sys_permission WHERE permission_code = 'block:bulk_delete';
--   3) 把 block_router.go 的 /bulk-delete 声明改回 block:delete。
--   标记表 sys_migration_marker 本身（596）不要动：它是通用载体，删表会让所有一次性动作
--   失去「已执行」的判据。
