-- 470 · 菜单 ↔ 权限点从「单值列」升级为多对多关联表（sys_menu_permission）。
--
-- 为什么要有这张表：sys_menus.permission_code 是单个 varchar，一个菜单节点最多挂一个
-- 权限码。这与实际授权模型对不上 —— 角色分权（RoleMenuSave）、管理员额外权限
-- （AdminMenuSave）都要按 menu_ids 收集「这个菜单节点代表哪些权限」，而一个菜单背后
-- 往往不止一个动作（查看 / 新建 / 更新 / 删除）。此前只能靠「在菜单下再挂 type=3 按钮
-- 子节点」逐个承载，于是权限点的归属被迫绑定到菜单树结构上：想给「商品管理」多一个
-- 动作，就必须在树里多造一个按钮节点并把它挂到侧栏不可见的位置。
--
-- 为什么不复用 sys_permission 加一列：试过并否掉了。sys_permission.menu_id 只能表达
-- 「一个权限码属于一个菜单」，而**现网已经有 3 个权限码被两条菜单同时登记**
-- （project:detail、inventory:warehouse_list、mail:campaign_list 各 2 条，
--  见迁移 250 的菜单范围收口与 128/229 的重复登记），反向 1:N 表达不了这个事实。
--
-- 为什么旧列**保留不删**：sys_menus.permission_code 不只有「被读」的一侧。
--   · 27 条注册为 Seed 的 SQL 会写这张表的这一列（030/052/084/090/.../467，共 24 条
--     含 permission_code，约 80 处），而 Seeds 台账在 Migrations 台账之后执行 ——
--     本迁移跑完，它们每次装配仍会执行；
--   · 其中 4 条拿 permission_code 当**幂等判据**（090_product_taxonomy_menu.sql:31、
--     101_inventory_menu.sql:22、126_mail.sql:47、128_mail_campaign.sql:23）：
--     删列或清空这一列会让判据永远查不到 → 每次启动重跑 → 重复插菜单行
--     （AGENTS.md 记过的 058/076 同类故障）。
--   于是旧列在本批之后降级为「seed 兼容写入点」：由下面的触发器单向补进新表，
--   读路径一律只读新表。历史迁移与 seed SQL 因此一行都不用改。
--
-- 为什么带触发器而不是「搬迁 + 清列」：清列等于让上面那 4 条判据失效；而只搬迁不加
-- 触发器，则**将来任何新写的 seed**（含历史迁移重放）直写旧列时，新表读不到，
-- 表现为「权限配了却不生效」，且没有任何报错。触发器把这条通道封住。
--
-- 幂等：CREATE TABLE / INDEX / FUNCTION 全带 IF NOT EXISTS 或 CREATE OR REPLACE，
--   触发器先 DROP 再建，搬迁走 ON CONFLICT DO NOTHING。重复执行安全。
-- 注册见 register_menu_permission.go（新表，用默认「表存在即跳过」检查）。
--
-- ⚠ 本文件写完后用 psql 事务回滚做过静态 + 语义校验（DDL 不能用 PREPARE）：
--   psql -U go_wp_app -d wp -v ON_ERROR_STOP=1 -c "BEGIN;" -f 470_sys_menu_permission.sql -c "ROLLBACK;"

-- 1. 关联表。menu_id 与 permission_code 都不加外键：
--    sys_menus 是软删除（deleted_at），外键表达不了「菜单已软删」；
--    permission_code 的合法性由写侧 ExistsEnabledCode 校验（权限点目录的真源是
--    sys_permission，且由 permission.SyncToDB 在装配期幂等对齐），
--    与 membership_entitlements.tier_id 同一种取舍（128 起的先例）。
CREATE TABLE IF NOT EXISTS sys_menu_permission (
    id              BIGSERIAL    PRIMARY KEY,
    menu_id         BIGINT       NOT NULL,
    permission_code VARCHAR(100) NOT NULL,
    create_time     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    update_time     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 一个菜单挂同一个码只有一条（写侧的全量替换是 DELETE + INSERT，这条索引同时是并发兜底）。
CREATE UNIQUE INDEX IF NOT EXISTS uq_sys_menu_permission_menu_code
    ON sys_menu_permission (menu_id, permission_code);

-- 反查方向（permission_code → menu_id）与权限点删除前的引用计数都走这条：
-- 唯一索引的前导列是 menu_id，对这两条查询帮不上忙。
CREATE INDEX IF NOT EXISTS idx_sys_menu_permission_code
    ON sys_menu_permission (permission_code);

-- 2. 搬迁：旧列是这次搬迁的唯一来源。已软删的菜单不再持有授权，不搬。
INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
SELECT m.id, m.permission_code, now(), now()
FROM sys_menus m
WHERE m.deleted_at IS NULL
  AND m.permission_code IS NOT NULL
  AND m.permission_code <> ''
ON CONFLICT (menu_id, permission_code) DO NOTHING;

-- 3. 旧列 → 新表的单向同步：seed 与历史迁移直写旧列的路径因此仍然生效。
--    只补插、不改不删 —— 它不为界面而生（界面对这一列**只读不写**，见 model 的
--    menuUpdateColumns 注释），所以「界面上删掉的码」不会被它按旧列的值补回来。
CREATE OR REPLACE FUNCTION sys_menus_sync_menu_permission() RETURNS trigger AS $$
BEGIN
    IF NEW.permission_code IS NOT NULL AND NEW.permission_code <> '' THEN
        INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
        VALUES (NEW.id, NEW.permission_code, now(), now())
        ON CONFLICT (menu_id, permission_code) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sys_menus_sync_menu_permission ON sys_menus;
CREATE TRIGGER trg_sys_menus_sync_menu_permission
    AFTER INSERT OR UPDATE OF permission_code ON sys_menus
    FOR EACH ROW EXECUTE FUNCTION sys_menus_sync_menu_permission();
