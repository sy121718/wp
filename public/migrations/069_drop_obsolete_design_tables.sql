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
-- 保留表的实现状态（审计 DB-09 补记于 2026-10；只补注释，本迁移的 SQL 一句未动）：
--   · content_template_component_pins —— **有实现**（contenttemplate 模块的 model）。
--   · page_component_pins —— **保留但无实现**：迁移目录之外全仓零引用（唯一出现处是
--     register_i18n_layer.go 把它的两个外键名字符串列在清理清单里），而姊妹表已在使用。
--     决策：**暂不删除**，理由是删表不可逆，而它已经降级为弱引用（不占任何写入路径、
--     不参与任何查询）。**若半年内仍无使用计划则删除** —— 届时走一条新的迁移，
--     不要回头改本文件（已执行过的迁移只增不改）。
--   · publication_events —— **本迁移的保留决定后来被推翻了**：迁移 207（CQ-015）把它
--     DROP 掉了，理由写在 207 的文件头（发布事实的真源是 publication_receipts）。
--     （不写具体日期：那条迁移的实际执行时间无法从仓库确认，写成「后来」是准确的，
--     写成某个月份就是猜。）
--     留着这行是因为它示范了「保留」这件事是有保质期的：069 当时判它「非废弃」，
--     若干批次之后它照样被删。上面 page_component_pins 的「半年内无使用即删」因此
--     不是客套话，而是这个仓库里已经发生过一次的路径。
--     文档侧的对应记录见 docs/schema-snapshot.md §3.1（含「保留但无实现」与
--     「RLS 豁免名单」两类判断的边界）。
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
