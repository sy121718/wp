-- 174_site_slot_dependency_kind.sql — dependency_kind 扩展：site_slot（审计 VIS-006）。
--
-- 背景：系统页面槽位（BIZ-1）换绑会让产物里的链接路径失效，但页面文档里没有
-- 「我用了购物车槽位」这种声明 —— 是否输出该链接取决于组件渲染时到底取了哪个槽位。
-- 因此这条依赖只能由构建期记录，需要一个独立的依赖类型。
--
-- CHECK 是封闭枚举（docs/03-pipeline.md §8.2），新增取值必须同步扩展；漏了它的话
-- 写入依赖会被数据库拒绝，而构建本身照样成功 —— 表现是「依赖静默没登记」，
-- 等槽位换绑时才发现该重建的页面没重建。
--
-- 与 071 同形：两张依赖表一起扩展（presentation 侧的自动发布实例同样会渲染槽位链接）。

ALTER TABLE page_dependencies
    DROP CONSTRAINT IF EXISTS page_dependencies_dependency_kind_check;

ALTER TABLE page_dependencies
    ADD CONSTRAINT page_dependencies_dependency_kind_check CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime',
        'i18n', 'block', 'site_slot'
    ));

ALTER TABLE presentation_dependencies
    DROP CONSTRAINT IF EXISTS presentation_dependencies_dependency_kind_check;

ALTER TABLE presentation_dependencies
    ADD CONSTRAINT presentation_dependencies_dependency_kind_check CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime',
        'i18n', 'block', 'site_slot'
    ));
