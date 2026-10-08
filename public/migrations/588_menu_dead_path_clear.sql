-- 588 · 三条「能力节点」菜单清空 path（它们不是页面）。
--
-- 背景：sys_menus 里有三条 is_hidden=1 的 type=2 行，path 指向**不存在的页面**：
--   · /project     「项目管理」  → 没有这个页面（工程管理已折进页面管理 / 站点设置）
--   · /artifact    「构建产物」  → 只有 GET /api/artifact/detail，没有页面
--   · /publication 「发布管理」  → 只有 GET /api/publication/receipts/pending，没有页面
--
-- 为什么**不删这三行**（实测 2026-10-07）：它们各自是下列权限码的**唯一承载节点** ——
--   project:list / artifact:detail / publication:receipts_pending
-- 删掉之后，这三个码在授权界面上就勾不到了（后台的「codes → menu_ids」反查找不到节点），
-- 而它们仍被 Casbin 强制执行 —— 后果是「角色编辑一次就静默丢权限」。所以保留节点、
-- 只把假的 path 清掉：它们是**能力节点**（承载一个可授权的能力），不是页面入口。
--
-- 判据按「本批自己的对象」逐条列举（上界封闭，见 AGENTS.md §数据库）：
-- 只动 path 命中那三条、且确实绑着那三个码的隐藏菜单行；偏差方向取「宁可重跑」。
--
-- 幂等：ConditionSQL 判「这三条 path 已清空」，命中即跳过；重复执行安全。
-- 注册见 register_menu_dead_path.go。

UPDATE sys_menus
   SET path = '', update_time = NOW()
 WHERE deleted_at IS NULL
   AND type = 2
   AND is_hidden = 1
   AND path IN ('/project', '/artifact', '/publication')
   AND EXISTS (
       SELECT 1 FROM sys_menu_permission mp
        WHERE mp.menu_id = sys_menus.id
          AND mp.permission_code IN ('project:list', 'artifact:detail', 'publication:receipts_pending')
   );
