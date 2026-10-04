-- 518 · 清理 AI 模块早期「一码多路由」留下的陈旧权限行。
--
-- 背景：AI 模块初版把 17 条路由挂在两个权限点码上（ai:view 挂 8 条、ai:manage 挂 7 条）。
--   但 sys_permission 上有唯一索引 uk_sys_permission_code ON (permission_code) —— 一目
--   权点码只能对应一条路由；装配末尾 permission.SyncToDB 的 fixPermissionRoutes 对同一
--   code 反复覆盖 api_path，库里最终只剩每个码的最后一笔（ai:view → GET /api/ai/session/get、
--   ai:manage → POST /api/ai/session/fold），其余 13 条路由在 sys_casbin_rule 里没有策略行，
--   含超管在内全员 403。后来拆成 18 个一码一路由的独立码（ai:provider_* / ai:session_* / ai:chat），
--   路由与策略都已正确，但 SyncToDB 只做 upsert（insertMissingPermissions 是 ON CONFLICT
--   DO NOTHING、fixPermissionRoutes 只更新 api_path/api_method/update_time、insertSuperadminPolicies
--   是 NOT EXISTS 补插），**没有任何删除陈旧行的逻辑** —— 于是这两个死码至今留在库里。
--
-- 为什么不改 SyncToDB 去自动清理：sync_test.go 的 TestSyncToDBIsIdempotentAndKeepsManualGrants
--   明确钉住「保留人工授权」这条不变量（人工改名 + status=0 的行必须原样保留），加了自动删除
--   就会把它一起干掉。陈旧行的清理属于「一次性历史数据处理」，放迁移里比放每次启动都跑的
--   同步逻辑里更合适 —— 前者只跑一次、可审计，后者会一直替人做决定。
--
-- 删两处，都用 permission_code 精确定位：
--   1. sys_permission 的两个死码行；
--   2. sys_casbin_rule 里 v3 = 这两个码的策略行（v3 存的就是 permission_code，见
--      internal/permission/sync.go 的 insertSuperadminPolicies：SELECT 'p', a.id, p.api_path,
--      p.api_method, p.permission_code）。
--
-- 不会误伤新码：ai:session_get / ai:session_fold 与死码同 api_path，但 v3 不同，保留。
--
-- 幂等：DELETE 天然幂等；ConditionSQL 判「两个死码都不存在」即整批跳过。

DELETE FROM sys_permission
WHERE permission_code IN ('ai:view', 'ai:manage');

DELETE FROM sys_casbin_rule
WHERE ptype = 'p' AND v3 IN ('ai:view', 'ai:manage');
