-- 544 · 「MCP 与外部访问」菜单项（挂到「站点」目录下，与「系统设置」「大模型管理」并列）
--
-- 背景：外部接入点（POST /mcp）与对外访问令牌（ai_access_token）已落地（542 / ddf5e157），
--   但没有任何后台入口 —— 只能靠 SQL 建令牌，而令牌的明文只在创建那一刻存在，
--   手工 SQL 意味着「谁建的、什么时候建的」全靠人记。
--
-- 挂哪：用户的原话是「挂在设置里面」。菜单树里「站点」目录下已有「系统设置」(/admin/system)、
--   「大模型管理」(/admin/ai/sessions)，本项与它们并列（sort 10，紧跟大模型管理的 9）。
--   不挂「系统」目录：那里是邮箱 / 邮件活动 / 插件管理，与本站自身的模型与工具配置不是一类。
--
-- permission_code 填 ai:token_list（页面的读权限点）：菜单可见性与页面路由用的是同一个权限点，
--   两处写不同的值会出现「菜单看得见、点进去 403」。
--
-- 父菜单用 title + type 定位而不是 id：id 是库内自增值，跨库（开发 / 测试 / 生产）不一致。
--   JOIN 保证父目录缺失时一行都不插 —— 挂到不存在的父节点下的菜单会飘在树外，
--   且下次修好父目录也不会自动归位。
--
-- 幂等：NOT EXISTS 按 path（页面身份的唯一键，不是 title）判定；
--   注册见 register_ai_mcp_menu.go，判据枚举本批自己的这一行。

INSERT INTO sys_menus (title, parent_id, type, path, component, permission_code, is_system, sort_order, create_by, create_time, update_by, update_time)
SELECT 'MCP 与外部访问', p.id, 2, '/admin/ai/mcp', '', 'ai:token_list', 1, 10, 0, NOW(), 0, NOW()
FROM sys_menus p
WHERE p.title = '站点' AND p.type = 1 AND p.deleted_at IS NULL
  AND NOT EXISTS (
    SELECT 1 FROM sys_menus x WHERE x.path = '/admin/ai/mcp' AND x.type = 2 AND x.deleted_at IS NULL
  );

-- 回滚（手工，无自动回滚）：
--   DELETE FROM sys_menus WHERE type = 2 AND path = '/admin/ai/mcp';
