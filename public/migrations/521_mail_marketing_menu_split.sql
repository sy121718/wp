-- 521 · 邮件营销独立成「营销」一级目录；页面按职能拆成四页
--
-- 背景（2026-09 后台信息架构评审）：
--   一页一个职能。现状是三个页面各自塞了多件事 ——
--     /admin/mail/marketing      联系人 + 群发活动 + 导入折叠区
--     /admin/mail                发信账号 + 邮件模板
--     /admin/mail/automation     流程列表 + 运行实例（排障）
--   装不下就该拆页，而不是靠 <details> 叠起来（229 库存那条教训）。
--   本迁移只做信息架构（菜单），页面/路由的拆分在同批代码改动里。
--
-- 本迁移做五件事：
--   1) 一级目录「营销」插在「站点」之前（站点 / 系统的 sort 顺延一位）：
--      营销是运营职能，跟交易而不是跟系统配置放一起；
--   2) 旧「邮件营销」行（/admin/mail/marketing）改造成「群发活动」并改挂到「营销」
--      目录下 —— **权限码保持 mail:campaign_list 不变**：角色授权按 permission_code
--      收集，改码会让已授权角色静默缩权（401 的教训，同款处理）；
--   3) 旧「邮件自动化」行（/admin/mail/automation）改挂到「营销」，标题统一成
--      「自动化任务」；
--   4) 新增「联系人」「邮件模板」两页（权限码 mail:contact_list / mail:template_list
--      是既有权限点，页面可见性随之走）；
--   5) 「邮箱管理」(/admin/mail) 留在「系统」目录不动：发信账号是系统配置，不是营销内容；
--   6) 活动报表行 /admin/mail/campaign 保持隐藏（它是必须带 ?id= 的子资源页，
--      无 id 打开只会落到「请先从活动列表选择一条活动」的提示），入口由新的
--      列表页 /admin/mail/campaigns 承担。
--
-- 隐藏行：/admin/mail/campaign（活动报表）在 401 里被置 is_hidden = 1，本迁移重复做一次
--   不是冗余 —— 401 走结构迁移通道，在「结构迁移先于 seed」的启动顺序下早于 224 的菜单
--   seed 执行：全新库首启时它看到「没有任何 is_hidden = 0 的 campaign 行」即判定已完成
--   而跳过，要等第二次启动才生效，中间这段窗口里侧栏会多出一个点不开的菜单。本迁移走
--   seed 通道并按 Version 排在 224 之后，恰好补上这个窗口，故两侧都幂等保留。
--
-- 幂等：注册见 register_mail_marketing_menu_split.go。CheckSQL 以「营销目录 + 联系人 /
--   邮件模板 / 群发活动 三条菜单都在，且旧 /admin/mail/marketing 已消失」为门槛 ——
--   单条判定最容易先成功，中途失败会把半成品误判成已完成（224 的注释）。
--   SQL 本身也带 NOT EXISTS 保护，门槛失效时重跑不会插重复行、不会反复顺延 sort。

-- 1) 顺延「站点」「系统」腾出 sort = 5（仅在「营销」目录尚不存在时执行）
UPDATE sys_menus SET sort_order = sort_order + 1, update_time = now()
WHERE coalesce(parent_id, 0) = 0
  AND sort_order >= 5
  AND NOT EXISTS (
      SELECT 1 FROM sys_menus d
      WHERE coalesce(d.parent_id, 0) = 0 AND d.title = '营销' AND d.type = 1 AND d.deleted_at IS NULL
  );

-- 2) 一级目录「营销」
INSERT INTO sys_menus (permission_code, title, parent_id, type, path, icon, status,
                       is_hidden, is_public, is_system, sort_order, remark, create_time, update_time)
SELECT NULL, '营销', 0, 1, '', 'megaphone', 1,
       0, 0, 1, 5, '营销模块一级目录：联系人 / 群发活动 / 邮件模板 / 自动化任务', now(), now()
WHERE NOT EXISTS (
    SELECT 1 FROM sys_menus d
    WHERE coalesce(d.parent_id, 0) = 0 AND d.title = '营销' AND d.type = 1 AND d.deleted_at IS NULL
);

-- 3) 旧「邮件营销」→「群发活动」：改标题与路径，改挂到「营销」，权限码原样保留
UPDATE sys_menus
SET title = '群发活动',
    path = '/admin/mail/campaigns',
    icon = 'send',
    parent_id = (SELECT id FROM sys_menus WHERE coalesce(parent_id, 0) = 0 AND title = '营销' AND type = 1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1),
    is_hidden = 0,
    sort_order = 2,
    update_time = now()
WHERE type = 2 AND path = '/admin/mail/marketing' AND deleted_at IS NULL;

-- 4) 旧「邮件自动化」→「自动化任务」：只改标题、图标与挂载点，路径不变
--
--    路径不变的段必须带**值差异守卫**。第 3 段改了 path，重放时 WHERE 自然不再命中；
--    本段路径原样不动，若只看 `path = '/admin/mail/automation'`，则每次重放都会命中同一行 ——
--    哪怕所有目标值都已就位，也会白写一次并刷新 update_time（下游按 update_time 做增量
--    失效时会多出无意义的失效）。守卫让「值已达目标」与「无需执行」等价，与第 7 段的
--    `AND is_hidden = 0` 保持同一约定。
--    守卫缺位由 qa-verify 的 TestMailSplitMenuReplayAffectsNoRows 抓出：完整态下逐语句
--    重放 521，期望 7 段全 0 行，实测仅本段影响 1 行（sys_menus 里 /admin/mail/automation
--    那行的 update_time 被刷新），其余 6 段 0 行。
UPDATE sys_menus
SET title = '自动化任务',
    icon = 'workflow',
    parent_id = (SELECT id FROM sys_menus WHERE coalesce(parent_id, 0) = 0 AND title = '营销' AND type = 1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1),
    sort_order = 4,
    update_time = now()
WHERE type = 2 AND path = '/admin/mail/automation' AND deleted_at IS NULL
  AND (
      title IS DISTINCT FROM '自动化任务'
      OR coalesce(icon, '') IS DISTINCT FROM 'workflow'
      OR coalesce(sort_order, 0) IS DISTINCT FROM 4
      OR coalesce(parent_id, 0) IS DISTINCT FROM (
          SELECT id FROM sys_menus WHERE coalesce(parent_id, 0) = 0 AND title = '营销' AND type = 1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1
      )
  );

-- 5) 新增「联系人」/admin/mail/contacts（营销目录内第 1 项）
INSERT INTO sys_menus (permission_code, title, parent_id, type, path, icon, status,
                       is_hidden, is_public, is_system, sort_order, remark, create_time, update_time)
SELECT 'mail:contact_list', '联系人', m.id, 2, '/admin/mail/contacts', 'users', 1,
       0, 0, 1, 1, '联系人名单：筛选 / 改状态 / 导入', now(), now()
FROM sys_menus m
WHERE coalesce(m.parent_id, 0) = 0 AND m.title = '营销' AND m.type = 1 AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.path = '/admin/mail/contacts' AND x.deleted_at IS NULL);

-- 6) 新增「邮件模板」/admin/mail/templates（营销目录内第 3 项）
INSERT INTO sys_menus (permission_code, title, parent_id, type, path, icon, status,
                       is_hidden, is_public, is_system, sort_order, remark, create_time, update_time)
SELECT 'mail:template_list', '邮件模板', m.id, 2, '/admin/mail/templates', 'mail', 1,
       0, 0, 1, 3, '邮件模板：标识 / 语言 / 主题 / 变量', now(), now()
FROM sys_menus m
WHERE coalesce(m.parent_id, 0) = 0 AND m.title = '营销' AND m.type = 1 AND m.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM sys_menus x WHERE x.path = '/admin/mail/templates' AND x.deleted_at IS NULL);

-- 7) 活动报表行不出现在侧栏（理由见文件头「隐藏行」一段：补 401 在全新库首启的时序窗口）
UPDATE sys_menus SET is_hidden = 1, update_time = now()
WHERE type = 2 AND path = '/admin/mail/campaign' AND deleted_at IS NULL AND is_hidden = 0;

-- 回滚（手工，无自动回滚）：
--   DELETE FROM sys_menus WHERE path IN ('/admin/mail/contacts', '/admin/mail/templates');
--   UPDATE sys_menus SET title = '邮件营销', path = '/admin/mail/marketing', parent_id = (SELECT id FROM sys_menus WHERE title = '系统' AND type = 1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1), sort_order = 2 WHERE path = '/admin/mail/campaigns';
--   UPDATE sys_menus SET title = '邮件自动化', parent_id = (SELECT id FROM sys_menus WHERE title = '系统' AND type = 1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1), sort_order = 4 WHERE path = '/admin/mail/automation';
--   DELETE FROM sys_menus WHERE title = '营销' AND type = 1;
--   UPDATE sys_menus SET sort_order = sort_order - 1 WHERE coalesce(parent_id, 0) = 0 AND sort_order >= 5;
--   （/admin/mail/campaign 的 is_hidden 恢复由 401 自己的回滚负责，本迁移不代劳。）
