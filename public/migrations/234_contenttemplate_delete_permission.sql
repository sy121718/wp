-- ========================================
-- 234 · 内容模板删除权限点与超管策略（后台列表页批量删除）
--
-- 背景：/admin/content-templates 列表页此前只有「可视化编辑 / 编辑入口」两个动作 ——
-- contenttemplate 模块**根本没有删除能力**（contract / service / model 里 Delete 零命中，
-- 模块路由只有 create/update/get/list，权限点表里也只有 create/update/get/list）。
-- 本批补上删除链路（contract + dto + model + service 同批）并让列表页提供批量删除。
--
-- 为什么权限点必须与路由同批：批量删除端点经 CasbinMiddlewareForPath 按**实际请求路径**
-- enforce —— 权限点缺失时没有任何策略能匹配，**含超管在内全员 403**
--（072/077/078/079/151/213 各踩过一次）。超管策略同理：只补权限点不补策略，超管自己也点不动。
--
-- 权限点只有一条，对应删除动作本身：
--   POST /api/contenttemplate/delete  删除模板（连带其全部历史版本与组件版本锁定行）
-- 路径写 /api/contenttemplate/delete 而不是页面路径 /admin/content-templates/bulk-delete：
-- 权限点登记的是**模块能力**（与 create/update/get/list 同族，该路径同时是 Casbin 的
-- enforce path），页面只是它的一个调用方；写页面路径会让同一个能力在权限表里分裂成两条，
-- 将来再加一个入口（例如模板编辑页的删除按钮）又要补一条权限点。
--
-- 不含菜单：删除动作长在列表页上，没有独立页面可挂（与 218 那批「权限点 + 菜单」不同）。
--
-- 幂等：两段各自带 NOT EXISTS 守卫（重复执行不报错、不产生重复行）。
-- 注册：public/migrations/register_admin_i18n.go（Seed 234-contenttemplate-delete-permission）。
-- ========================================

-- 1) 权限点（1 条）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('contenttemplate:delete', '模板删除', 'contenttemplate', '/api/contenttemplate/delete', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission p WHERE p.permission_code = v.code);

-- 2) 超管策略（身份表全量超管 × 这条权限点，缺哪条补哪条）
INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
SELECT 'p', CAST(a.id AS VARCHAR), p.api_path, p.api_method, p.permission_code
FROM sys_admin a
CROSS JOIN sys_permission p
WHERE a.is_admin = 1
  AND p.status = 1
  AND p.permission_code = 'contenttemplate:delete'
  AND NOT EXISTS (
      SELECT 1 FROM sys_casbin_rule r
      WHERE r.ptype = 'p' AND r.v0 = CAST(a.id AS VARCHAR)
        AND r.v1 = p.api_path AND r.v2 = p.api_method AND r.v3 = p.permission_code
  );
