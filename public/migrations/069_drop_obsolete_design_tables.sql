-- 069：删除「设计已被取代」的 6 张空表（代码零引用 + 设计已被取代，2026-09 核对）。
--
-- 删除理由：
--   * media_asset / media_asset_variant / media_reference —— 02-B 原媒体中心设计。
--     已改用 sys_attachment / sys_file_category / sys_media_variant 实现
--     （稳定引用、md5 去重、generation 代数、extra_info jsonb refs 引用保护，见 067_media_center.sql
--      与 docs/02-B-media-center.md §6 实现映射）。
--   * global_components / global_component_versions / global_component_policies —— 原 component 模块设计。
--     语义已由 block.reuse_mode（global 引用 / template 一次性复制）承担
--     （见 049_block_reuse_mode.sql 与 docs/02-domain.md §4.2/§4.3）。
--
-- 明确保留（本迁移不触碰）：
--   build_jobs（表在 init 中创建，队列引擎尚未接入 Go，非废弃）、
--   page_component_pins、content_template_component_pins、publication_events。
--
-- 外键依赖处理（前置条件，必须先解绑）：
--   page_component_pins / content_template_component_pins 两张保留表原有 4 个外键指向
--   global_components / global_component_versions。被引用表删除前必须解绑这些约束，
--   否则 DROP TABLE 会因依赖而失败。此处仅删约束，表、列与主键全部保留（降级为弱引用）。
--
-- 幂等：DROP CONSTRAINT IF EXISTS / DROP TABLE IF EXISTS 均可重复执行；
--       DO 块用 to_regclass 守卫，全新库（保留表尚未创建）同样安全。

-- 1) 解绑保留表上指向废弃表的外键（仅删约束，不删表）
DO $$ BEGIN
    IF to_regclass('page_component_pins') IS NOT NULL THEN
        ALTER TABLE page_component_pins DROP CONSTRAINT IF EXISTS page_component_pins_component_id_fkey;
        ALTER TABLE page_component_pins DROP CONSTRAINT IF EXISTS page_component_pins_pinned_version_id_fkey;
    END IF;
    IF to_regclass('content_template_component_pins') IS NOT NULL THEN
        ALTER TABLE content_template_component_pins DROP CONSTRAINT IF EXISTS content_template_component_pins_component_id_fkey;
        ALTER TABLE content_template_component_pins DROP CONSTRAINT IF EXISTS content_template_component_pins_pinned_version_id_fkey;
    END IF;
END $$;

-- 2) 媒体三表：先变体/引用（依赖方），后主表（被引用方）
DROP TABLE IF EXISTS media_asset_variant;
DROP TABLE IF EXISTS media_reference;
DROP TABLE IF EXISTS media_asset;

-- 3) 全局组件三表：先 versions/policies（依赖方），后主表（被引用方）
DROP TABLE IF EXISTS global_component_policies;
DROP TABLE IF EXISTS global_component_versions;
DROP TABLE IF EXISTS global_components;
