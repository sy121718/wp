-- ========================================
-- go_wp — 对齐 blueprints / blueprint_versions 到 model（唯一真源）
--
-- 背景（全项目审查发现，已在真实库 \d blueprints 上实测确认）：
--   两张表存在「双源 DDL」——002-init-builder-schema 先建表，
--   045_blueprint.sql 的 CREATE TABLE IF NOT EXISTS 永远被跳过，
--   于是实际生效的是 002 的旧列集合：
--     blueprints.project_id          uuid NOT NULL（无默认值）
--     blueprint_versions.source_hash text NOT NULL
--     blueprint_versions.created_by  uuid NOT NULL
--   而 BlueprintEntity / VersionEntity 都不写这三列（blueprint 在代码层是
--   工程无关资源，全模块无 ProjectID 引用），因此 Create / CreateVersion
--   在生产 DDL 下 100% 失败（NOT NULL 无默认 + INSERT 不含该列）。
--
-- 处理：DROP COLUMN IF EXISTS（幂等）。
--   002 的建表语句与 045（已删除）已同步修正，新库不再产生分叉。
--
-- 注册：public/migrations/register.go（Migration 073-blueprint-ddl-align）。
-- ========================================
ALTER TABLE blueprints DROP COLUMN IF EXISTS project_id;
ALTER TABLE blueprint_versions DROP COLUMN IF EXISTS source_hash;
ALTER TABLE blueprint_versions DROP COLUMN IF EXISTS created_by;
