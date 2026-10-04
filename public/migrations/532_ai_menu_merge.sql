-- 532 · AI 的两个菜单入口合并成一个「大模型管理」，并补两个词条。
--
-- 背景：/admin/ai/providers（AI 模型）与 /admin/ai/sessions（AI 会话）原本是两个菜单项、
--   两个页面。合成一个入口后：模型标签在前、会话在后，菜单只剩一条（m00210）。
--
-- 执行顺序不能反：先把旧的「AI 会话」软删，再把「AI 模型」那条改名改路径 ——
--   反过来的话两条会在 /admin/ai/sessions 上撞车，第二次执行还会把改好的那条一起软删
--   （所以删除那一条额外用 title 兜住，保证幂等）。
--
-- 菜单可见性挂两个码：sys_menu_permission 是「任一命中即显示」（见 menu_authz.go 的
--   matchedMenuCodes），所以只有会话权限的人也能看到入口。
--
-- 幂等：三条 UPDATE/INSERT 都以本批自己的对象为判据；i18n 走 ON CONFLICT DO NOTHING。
UPDATE sys_menus SET deleted_at = NOW(), update_time = NOW()
 WHERE path = '/admin/ai/sessions' AND title = 'AI 会话' AND deleted_at IS NULL;

UPDATE sys_menus SET title = '大模型管理', title_key = 'admin.ai.menu.title',
       path = '/admin/ai/sessions', sort_order = 9, update_time = NOW()
 WHERE path = '/admin/ai/providers' AND deleted_at IS NULL;

INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
SELECT m.id, 'ai:session_list', NOW(), NOW()
  FROM sys_menus m
 WHERE m.path = '/admin/ai/sessions' AND m.deleted_at IS NULL
   AND NOT EXISTS (
       SELECT 1 FROM sys_menu_permission mp
        WHERE mp.menu_id = m.id AND mp.permission_code = 'ai:session_list'
   );

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time) VALUES
('admin.ai.menu.title', 'zh-CN', '大模型管理', 200, 'admin', '侧栏菜单与页面 h1：合并后的统一入口', 1, now(), now()),
('admin.ai.menu.title', 'en-US', 'LLM Management', 200, 'admin', 'sidebar + h1 of the merged entry', 1, now(), now()),
('admin.ai.noAccess', 'zh-CN', '你没有任何 AI 功能的访问权限，请联系管理员。', 200, 'admin', 'admin/ai/sessions.html: 两个标签都无权限时的空态', 1, now(), now()),
('admin.ai.noAccess', 'en-US', 'You have no access to any AI feature. Ask an administrator.', 200, 'admin', 'admin/ai/sessions.html: empty state', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
