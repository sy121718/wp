-- 710 · AI 模型目录写入口的按钮节点 + 令牌状态词条。
--
-- 背景：模型目录的候选/追加从页面 POST 迁到 /api（task-35），并各自拿到独立权限点
--   （ai:provider_models_candidates / ai:provider_models_append）。同时「对外接入点开关」
--   也从「借签发令牌的权限」改成自己的 ai:token_toggle。
--
-- 权限点行本身不用在这里插：internal/permission/sync.go 启动时按路由声明
--   （permission.AIProviderModelsCandidates 等）插 sys_permission 并补超管策略。
--   这里补的是它管不到的两样东西：
--     1) 菜单里的按钮节点（sys_menus type = 3）与读路径要的 sys_menu_permission 关联 ——
--        按钮可见性只看这两张表，缺了就是「后端允许、界面上没有这个按钮」。
--        本批按钮是四个：模型候选/追加、对外接入点开关，以及 ai:session_send
--        （「向指定会话发消息」—— 续写已有会话从页面 POST 迁到 /api 时新开的独立权限点）。
--     2) 令牌状态的两个界面词条：mcp_tokens.html 用 tr("admin.ai.mcp.token.status.active"
--        /".revoked") 取词，之前没有任何 seed，界面上会露出裸 key。
--
-- 幂等：按钮用 NOT EXISTS 按 (type, permission_code) 判；词条用
--   ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

-- 1) 三个按钮节点，挂在「大模型管理」这个二级菜单下（与 589 里其它 ai.* 按钮同一父节点）。
WITH v(code, slug, parent_code) AS (VALUES
        ('ai:provider_models_candidates', 'ai.provider_models_candidates', 'ai:provider_list'),
        ('ai:provider_models_append', 'ai.provider_models_append', 'ai:provider_list'),
        ('ai:token_toggle', 'ai.token_toggle', 'ai:token_list'),
        ('ai:session_send', 'ai.session_send', 'ai:provider_list')
)
INSERT INTO sys_menus (title, parent_id, type, path, icon, permission_code, title_key,
                       is_system, is_hidden, sort_order, create_by, create_time, update_by, update_time)
SELECT COALESCE(NULLIF(trim(v.code), ''), v.slug),
       COALESCE((SELECT pm.id FROM sys_menus pm
                   JOIN sys_menu_permission mp ON mp.menu_id = pm.id
                  WHERE pm.deleted_at IS NULL AND pm.type = 2
                    AND mp.permission_code = v.parent_code
                  ORDER BY pm.id LIMIT 1), 0),
       3, '', '', v.code, v.slug, 1, 0,
       10 + ROW_NUMBER() OVER (PARTITION BY v.parent_code ORDER BY v.code),
       0, NOW(), 0, NOW()
  FROM v
 WHERE NOT EXISTS (
       SELECT 1 FROM sys_menus x
        WHERE x.deleted_at IS NULL AND x.type = 3 AND x.permission_code = v.code
   );

-- 2) 新节点进关联表（读路径只读 sys_menu_permission，见迁移 470）。
INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
SELECT m.id, m.permission_code, NOW(), NOW()
  FROM sys_menus m
 WHERE m.deleted_at IS NULL
   AND m.type = 3
   AND m.permission_code IN ('ai:provider_models_candidates', 'ai:provider_models_append',
                             'ai:token_toggle', 'ai:session_send')
   AND NOT EXISTS (
       SELECT 1 FROM sys_menu_permission mp
        WHERE mp.menu_id = m.id AND mp.permission_code = m.permission_code
   );

-- 3) 令牌状态词条（模板 tr() 的兜底就在这一对 key 上）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.ai.mcp.token.status.active', 'zh-CN', '启用', 200, 'ui',
     'internal/templates/admin/ai/mcp_tokens.html: 令牌状态（有效）', 1, now(), now()),
    ('admin.ai.mcp.token.status.active', 'en-US', 'Active', 200, 'ui',
     'internal/templates/admin/ai/mcp_tokens.html: token status (active)', 1, now(), now()),
    ('admin.ai.mcp.token.status.revoked', 'zh-CN', '已撤销', 200, 'ui',
     'internal/templates/admin/ai/mcp_tokens.html: 令牌状态（已撤销）', 1, now(), now()),
    ('admin.ai.mcp.token.status.revoked', 'en-US', 'Revoked', 200, 'ui',
     'internal/templates/admin/ai/mcp_tokens.html: token status (revoked)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 回滚（手工，无自动回滚）：
--   DELETE FROM sys_menu_permission WHERE permission_code IN ('ai:provider_models_candidates', 'ai:provider_models_append', 'ai:token_toggle', 'ai:session_send');
--   DELETE FROM sys_menus WHERE type = 3 AND permission_code IN ('ai:provider_models_candidates', 'ai:provider_models_append', 'ai:token_toggle', 'ai:session_send');
--   DELETE FROM sys_i18n WHERE item_key IN ('admin.ai.mcp.token.status.active', 'admin.ai.mcp.token.status.revoked');
