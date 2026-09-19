-- 294 · 文案词条页的只读权限点（补 178 留下的读侧缺口）。
--
-- 背景：178 只建了写权限点（i18n:manage → POST /api/i18n/save），并明确说明
-- 「列表与筛选是只读的（GET 不挂 Casbin）」。但**菜单隐藏不是访问控制** ——
-- 任何登录的后台账号直接输入 /admin/i18n 就能打开页面、读到全部词条。
-- 六领域只读页（迁移 050 的 GET 权限点）已经收口，这里是最后一处同类缺口。
--
-- 为什么必须新建权限点、不能复用 i18n:manage：CasbinMiddlewareForPath 的 act 取自
-- **实际请求方法**，页面是 GET 而 i18n:manage 的策略只有 POST —— 直接复用会让
-- Enforce(user, "/api/i18n/save", "GET") 匹配不到任何策略，**含超管在内全员 403**。
--
-- api_path 取 /api/i18n/list：它是「文案列表查看」这一能力的语义锚点。该 GET 接口
-- 当前并不存在（页面 handler 直接查库渲染），权限点在这里是**策略载体**而不是路由映射
-- —— 与 050「页面复用 API 权限点」是同一套用法。check-permission-gaps.sh 会把它归入
-- 「库中存在但代码未声明」那一类并原样列出（该项不影响退出码）。
--
-- 菜单绑定同时从 i18n:manage 改为 i18n:view：菜单表示「能不能进入这个功能」，
-- 进入之后的保存/删除仍然需要 i18n:manage。非超管的查看权限只能通过角色分配菜单获得，
-- 所以这个绑定必须存在，否则 i18n:view 没有任何发放渠道（会成为一条只对超管生效的权限点）。
--
-- 幂等：权限点与策略用 NOT EXISTS 守卫；菜单用 WHERE 限定旧值，重复执行为空操作。

INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('i18n:view', '文案词条查看', 'i18n', '/api/i18n/list', 'GET')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission WHERE permission_code = v.code);

-- 超管全量策略（p, user_id, path, method, code）。
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), t.path, t.method, t.code
FROM sys_admin a
CROSS JOIN (VALUES
    ('/api/i18n/list', 'GET', 'i18n:view')
) AS t(path, method, code)
WHERE a.is_admin = 1
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR) AND r.v1 = t.path AND r.v2 = t.method
  );

-- 菜单绑定改为查看权限（限定旧值与路径，保证幂等）。
UPDATE sys_menus
SET permission_code = 'i18n:view', update_time = NOW()
WHERE permission_code = 'i18n:manage' AND path = '/admin/i18n';
