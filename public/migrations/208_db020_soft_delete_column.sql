-- 208 · DB-020 软删除列名统一：sys_menus.deleted_time → deleted_at。
--
-- 决策（YG 拍板）：软删列一律 deleted_at。
-- 全库只有 sys_menus 用 deleted_time（pages / users / presentation_instances /
-- content_objects 都是 deleted_at），改名后这一族只剩一种写法。
--
-- 连带：
--   · 070 建的 (deleted_time, sort_order) 部分索引与 178 的 i18n 权限 seed 都引用旧列名，
--     前者由 PG 自动跟随 RENAME（索引表达式会被重写），后者是历史迁移、在本迁移之前执行；
--   · 21 个菜单 seed 的 WHERE deleted_time IS NULL 已同步改成新列名 ——
--     seed 是「可重复执行」的，不能留旧列名（否则改名后重跑即报错）。
--
-- 幂等：仅当旧列存在时才改名。

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'sys_menus'
                 AND column_name = 'deleted_time') THEN
        ALTER TABLE sys_menus RENAME COLUMN deleted_time TO deleted_at;
    END IF;
END $$;
