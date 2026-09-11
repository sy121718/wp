-- 112 · 主数据变更记录权限点 + 超管策略（issue #19）。
-- 与 100/104/106/109 同构：权限点 + 超管全量策略。
--
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403
--（与 072/077/078/079/100/104/106 同因）。条件只看本票自己的权限点（masterdata:change_%），
-- 与既有宽匹配互不干扰。
--
-- 本模块是只读模块：四个查询接口，没有写接口（变更记录由业务模块在写操作里追加）。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('masterdata:change_list',     '变更记录列表',     'masterdata', '/api/masterdata/change/list',     'GET'),
    ('masterdata:change_count',    '变更记录计数',     'masterdata', '/api/masterdata/change/count',    'GET'),
    ('masterdata:change_entities', '实体变更历史清单', 'masterdata', '/api/masterdata/change/entities', 'GET'),
    ('masterdata:change_entity',   '单实体变更历史',   'masterdata', '/api/masterdata/change/entity',   'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/masterdata/change/list',     'GET', 'masterdata:change_list'),
    ('/api/masterdata/change/count',    'GET', 'masterdata:change_count'),
    ('/api/masterdata/change/entities', 'GET', 'masterdata:change_entities'),
    ('/api/masterdata/change/entity',   'GET', 'masterdata:change_entity')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
