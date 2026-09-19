-- 289 · 依赖 kind 扩展：按具体菜单项引用（navigation）。
--
-- 背景（批 1「导航菜单 + 超级菜单」）：core.nav 支持两种引用方式，各登记一条依赖 ——
--   · 按位置（Props.Menu）      → menu:{projectID}:{kind}（kind 已由 071 放行 'menu'）
--   · 按具体菜单项（Props.Navigation） → navigation:{itemID}（本次新增，需放行 'navigation'）
--
-- 不放行的表现是**构建期直接失败**（依赖行插入被 CHECK 拒绝）：不是静默失效，
-- 但也意味着「按项引用」这个能力完全不可用 —— 必须与 Go 侧同批落地。
--
-- 与 071 / 174 同形：两张依赖表一起扩展（自动发布实例侧同样会引用菜单项）。
-- 约束按名先删后建（IF EXISTS + 新定义），重复执行安全。

ALTER TABLE page_dependencies
    DROP CONSTRAINT IF EXISTS page_dependencies_dependency_kind_check;

ALTER TABLE page_dependencies
    ADD CONSTRAINT page_dependencies_dependency_kind_check CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime',
        'i18n', 'block', 'site_slot', 'navigation'
    ));

ALTER TABLE presentation_dependencies
    DROP CONSTRAINT IF EXISTS presentation_dependencies_dependency_kind_check;

ALTER TABLE presentation_dependencies
    ADD CONSTRAINT presentation_dependencies_dependency_kind_check CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime',
        'i18n', 'block', 'site_slot', 'navigation'
    ));
