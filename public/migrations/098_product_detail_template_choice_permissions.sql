-- 098 · 详情页模板可选与预览权限点 + 超管策略（issue #14）。
--
-- 背景（spec #2「商品展示资产的落点」延伸）：一个商品类型下可有多套命名模板，
-- 商品发布时可指定用哪套模板，发布前可预览渲染效果。这两个新接口各自需要权限点：
--   · presentation:preview         —— 发布前预览（只读渲染：不落库、不落盘、不激活）；
--   · presentation:get_by_entity   —— 按内容实体查实例（后台「详情页模板」页读当前绑定）。
--
-- 与 034（presentation 基础权限点）/ 082 / 086a / 087 / 089 / 092 / 096 同构：权限点 + 超管策略。
-- 未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403。
-- 条件只看本票自己的两个权限点，与 034 的逐路径 seed 互不干扰（不同 api_path）。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('presentation:preview',       '实例预览',     'presentation', '/api/presentation/preview',       'POST'),
    ('presentation:get_by_entity', '实例按实体查询', 'presentation', '/api/presentation/get-by-entity', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/presentation/preview',       'POST', 'presentation:preview'),
    ('/api/presentation/get-by-entity', 'GET',  'presentation:get_by_entity')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (SELECT 1 FROM sys_casbin_rule r
                  WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
                    AND r.v1 = t.path AND r.v2 = t.method);
