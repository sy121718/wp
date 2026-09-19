-- 288_contenttemplate_activate_permission.sql — 模板「切换生效」权限点（多套存着、单套生效）。
--
-- 为什么必须单独一支权限点：Casbin 中间件按**实际请求路径** enforce，sys_casbin_rule 里只有
-- (/api/contenttemplate/update, POST) 这条策略；请求 /api/contenttemplate/activate 时没有任何策略
-- 匹配 → **含超管在内全员 403**（AGENTS.md 记过 072/077/078/079/151 五次同因故障）。
-- 也不能让一个 permission_code 挂两条路径：sys_permission 上 uk_sys_permission_code 唯一。
--
-- 幂等：按 permission_code 逐条 NOT EXISTS（只看 module 或计数会在同模块新增权限点时误跳过）。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('contenttemplate:activate', '模板切换生效', 'contenttemplate', '/api/contenttemplate/activate', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（与 034 第二个 INSERT 同构）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/contenttemplate/activate', 'POST', 'contenttemplate:activate')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
