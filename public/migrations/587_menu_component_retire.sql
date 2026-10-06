-- 587 · component（组件路径）字段退役的收尾：给列打退役注释 + 清掉三条悬空错误词条。
--
-- 背景：sys_menus.component 是 soybean-admin（Vue SPA）时代的产物。后台改成
-- HTMX + Jet SSR 后页面路由只认 path —— component 既没有生产者（新建 / 编辑表单与
-- 接口都不再写它），也没有消费者（导航树、侧栏 partials/sidebar.html、
-- GET /api/admin/routes 都不读它）。代码层已整体删除：model 字段、dto 四处、
-- 校验函数 validateComponentBinding 与 componentPathPattern、三个错误常量。
--
-- 为什么这里只 COMMENT 而**不 DROP COLUMN**：
-- 1. 有 28 个 seed 文件在写这一列（030 / 032 / 084 / 086b / 090 / 093 / 097 / 101 /
--    107 / 110 / 113 / 116 / 126 / 128 / 132 / 133 / 143 / 149 / 150 / 153 / 178 /
--    218 / 463 / 467 / 525 / 544 / 580 / init_schema.sql）。
-- 2. RunSeeds 恒在所有结构迁移之后执行（internal/routers/assembly.go:419），
--    所以 DROP 之后全新库会在种子阶段直接撞 column "component" does not exist。
-- 3. 改写这 28 个文件并不机械：INSERT 的列清单索引与 FROM (VALUES ...) 元组的索引
--    并不一致（元组按 AS v(title, type, path, component, code, sort) 别名顺序对齐），
--    按列清单索引删会静默错位 —— 收益（少一个死列）远小于风险。
-- 4. 列里的历史值本身就是「这个菜单曾经对应哪个 Vue 页面」的迁移考古信息。
-- 于是改为把「已退役」写进列注释，后来人看到就知道可以无视。
--
-- 为什么删三条词条：ErrComponentRequired / ErrComponentNotAllowed / ErrComponentInvalid
-- 是组件路径校验的错误文案（种在 058_i18n_seed_enums.sql），校验已删，词条留着就是
-- 悬空引用。两句都幂等，重复执行无害。

-- 注释包在 DO 块里并吞掉权限异常：这条 seed 可能被业务角色（go_wp_app，非 sys_menus
-- 属主）执行 —— 那种情况下 COMMENT 会报 must be owner of relation sys_menus，
-- 而一条 seed 抛错会中断整个 RunSeeds。列注释只是「后来人别误会」的锦上添花，
-- 不该有阻断启动的权力。DELETE 是普通 DML，业务角色有权限，照常执行。
DO $$
BEGIN
    COMMENT ON COLUMN sys_menus.component IS '已退役（587）：soybean-admin / Vue SPA 时代的组件路径。后台改为 HTMX + Jet SSR 后页面路由只认 path，本列已无生产者与消费者，仅为历史 seed 的写入通道保留，可无视。';
EXCEPTION WHEN insufficient_privilege THEN
    RAISE NOTICE '587：当前角色不是 sys_menus 的属主，跳过 component 列注释';
END $$;

DELETE FROM sys_i18n
WHERE item_key IN ('ErrComponentRequired', 'ErrComponentNotAllowed', 'ErrComponentInvalid');
