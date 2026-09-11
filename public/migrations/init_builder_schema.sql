-- ========================================
-- go_wp — 可视化构建器 DDL（24 张表）
-- 来源：docs/02-domain.md §7（领域层 14 张）+ docs/03-pipeline.md §9（流水线层 10 张）
-- 与本地 PostgreSQL / wp 库对齐
-- 按依赖顺序排列（文档原序中 page_routes 前向引用 presentation_instances，此处已重排）；
-- ALTER 复合外键在全部基表就绪后统一执行
-- ========================================

-- ============ 领域层（docs/02-domain.md §7） ============

-- 1. 站点工程
CREATE TABLE IF NOT EXISTS projects (
    id          uuid PRIMARY KEY,
    name        text NOT NULL,
    settings    jsonb NOT NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL
);

-- 2. Blueprint（Page Document 初始化工具，用完即弃）
-- 注意：blueprint 是「Page Document 初始化工具」，在代码层面是工程无关的
-- （BlueprintEntity 无 ProjectID 字段，见 internal/module/blueprint/model）。
-- 本表曾误带 project_id uuid NOT NULL（无默认值），导致 blueprint Create 必然失败；
-- 已移除该列，历史库由迁移 073 对齐。045_blueprint.sql 的重复建表已删除。
CREATE TABLE IF NOT EXISTS blueprints (
    id              uuid PRIMARY KEY,
    name            text NOT NULL,
    kind            text NOT NULL,
    draft_document  jsonb NOT NULL,
    draft_version   bigint NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL
);

-- 列集合以 VersionEntity 为唯一真源：source_hash / created_by 是 045 旧设计的残留，
-- model 从不写入，NOT NULL 下 CreateVersion 必然失败，故一并移除（历史库由 073 对齐）。
CREATE TABLE IF NOT EXISTS blueprint_versions (
    id              uuid PRIMARY KEY,
    blueprint_id    uuid NOT NULL REFERENCES blueprints(id),
    version         bigint NOT NULL,
    document        jsonb NOT NULL,
    created_at      timestamptz NOT NULL,
    UNIQUE (blueprint_id, version)
);

-- 3. Global Component（全局组件与版本）：⚠️ 已废弃删除（2026-09）。
-- 原 global_components / global_component_versions 建表语句已移除，语义由 block.reuse_mode
-- （global 引用 / template 一次性复制）承担，见 docs/02-domain.md §4.2 与迁移 069。

-- 4. ContentTemplate 与版本（每次构建参与）
CREATE TABLE IF NOT EXISTS content_templates (
    id                  uuid PRIMARY KEY,
    project_id          uuid NOT NULL REFERENCES projects(id),
    name                text NOT NULL,
    entity_type         text NOT NULL,  -- 类型合法性由实体类型注册表判定（迁移 080 解掉枚举）
    draft_document      jsonb NOT NULL,
    draft_version       bigint NOT NULL DEFAULT 1,
    current_version_id  uuid NULL,
    created_at          timestamptz NOT NULL,
    updated_at          timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS content_template_versions (
    id                   uuid PRIMARY KEY,
    template_id          uuid NOT NULL REFERENCES content_templates(id),
    version              bigint NOT NULL,
    document             jsonb NOT NULL,
    source_hash          text NOT NULL,
    created_by           uuid NOT NULL,
    created_at           timestamptz NOT NULL,
    UNIQUE (template_id, version),
    UNIQUE (id, template_id)
);

-- 5. 手工 Page
CREATE TABLE IF NOT EXISTS pages (
    id                    uuid PRIMARY KEY,
    project_id            uuid NOT NULL REFERENCES projects(id),
    kind                  text NOT NULL,
    content_target_type         text NOT NULL,
    content_target_id           uuid NULL,
    source_blueprint_version_id uuid NULL REFERENCES blueprint_versions(id),
    draft_path                  text NOT NULL,
    active_path           text NULL,
    draft_document        jsonb NOT NULL,
    draft_version         bigint NOT NULL DEFAULT 1,
    staged_artifact_id    uuid NULL,
    active_artifact_id    uuid NULL,
    stale                 boolean NOT NULL DEFAULT true,
    deleted_at            timestamptz NULL,
    published_at          timestamptz NULL,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    UNIQUE (id, project_id),
    CONSTRAINT pages_content_contract_check CHECK (
        (kind IN ('home', 'archive', 'search', 'notFound')
            AND content_target_type = 'none'
            AND content_target_id IS NULL)
        OR (kind = 'page'
            AND content_target_type = 'page'
            AND content_target_id IS NOT NULL)
        OR (kind = 'article'
            AND content_target_type = 'article'
            AND content_target_id IS NOT NULL)
        OR (kind = 'tag'
            AND content_target_type = 'tag'
            AND content_target_id IS NOT NULL)
    )
);

-- 5.1 Page 草稿修订（每次保存追加不可变快照）
CREATE TABLE IF NOT EXISTS page_revisions (
    id              uuid PRIMARY KEY,
    page_id         uuid NOT NULL REFERENCES pages(id),
    version         bigint NOT NULL,
    draft_path      text NOT NULL,
    draft_document  jsonb NOT NULL,
    source_hash     text NOT NULL,
    created_at      timestamptz NOT NULL,
    UNIQUE (page_id, version)
);

-- 6. PresentationInstance（自动页面实例）
CREATE TABLE IF NOT EXISTS presentation_instances (
    id                    uuid PRIMARY KEY,
    project_id            uuid NOT NULL REFERENCES projects(id),
    entity_type           text NOT NULL CHECK (entity_type IN ('product', 'article', 'category')),
    entity_id             uuid NOT NULL,
    url_path              text NOT NULL,
    template_id           uuid NOT NULL REFERENCES content_templates(id),
    current_snapshot_id   uuid NULL,
    staged_snapshot_id    uuid NULL,
    staged_artifact_id    uuid NULL,
    active_artifact_id    uuid NULL,
    stale                 boolean NOT NULL DEFAULT true,
    deleted_at            timestamptz NULL,
    published_at          timestamptz NULL,
    created_at            timestamptz NOT NULL,
    updated_at            timestamptz NOT NULL,
    UNIQUE (entity_type, entity_id),
    UNIQUE (project_id, url_path)
);

-- 7. DocumentSnapshot（PresentationInstance 的文档快照）
CREATE TABLE IF NOT EXISTS document_snapshots (
    id                            uuid PRIMARY KEY,
    presentation_instance_id      uuid NOT NULL REFERENCES presentation_instances(id),
    source_template_version_id    uuid NOT NULL REFERENCES content_template_versions(id),
    source_entity_revision_id     uuid NOT NULL,
    document                      jsonb NOT NULL,
    created_at                    timestamptz NOT NULL,
    UNIQUE (id, presentation_instance_id)
);

-- 8. URL 占用表（draft/active/redirect 唯一占用）
CREATE TABLE IF NOT EXISTS page_routes (
    project_id   uuid NOT NULL REFERENCES projects(id),
    path         text NOT NULL,
    page_id      uuid NULL REFERENCES pages(id),
    presentation_id uuid NULL REFERENCES presentation_instances(id),
    route_kind   text NOT NULL CHECK (route_kind IN ('reserved', 'active', 'redirect')),
    artifact_id  uuid NULL,
    updated_at   timestamptz NOT NULL,
    PRIMARY KEY (project_id, path),
    CHECK (
        (page_id IS NOT NULL AND presentation_id IS NULL)
        OR (page_id IS NULL AND presentation_id IS NOT NULL)
    )
);

-- 9. 全局组件更新策略：⚠️ 已废弃删除（2026-09）。
-- 原 global_component_policies 建表语句已移除，语义由 block.reuse_mode 承担，
-- 见 docs/02-domain.md §4.3 与迁移 069。

-- 10. 页面级组件版本锁定（表保留，见 03-pipeline 规划）
-- ⚠️ 原 global_components / global_component_versions 已废弃删除（语义由 block.reuse_mode 承担），
--    故去掉指向它们的外键；表、列、主键全部保留（降级为弱引用）。
--    见 docs/02-domain.md §4.2/§4.3 与 069_drop_obsolete_design_tables.sql。
CREATE TABLE IF NOT EXISTS page_component_pins (
    page_id       uuid NOT NULL REFERENCES pages(id),
    component_id  uuid NOT NULL,
    pinned_version_id uuid NOT NULL,
    PRIMARY KEY (page_id, component_id)
);

-- 11. ContentTemplate 级组件版本锁定（表保留，见 02-domain §4 规划）
-- ⚠️ 原 global_components / global_component_versions 已废弃删除（语义由 block.reuse_mode 承担），
--    故去掉指向它们的外键；表、列、主键全部保留（降级为弱引用）。
--    见 docs/02-domain.md §4.2/§4.3 与 069_drop_obsolete_design_tables.sql。
CREATE TABLE IF NOT EXISTS content_template_component_pins (
    template_id       uuid NOT NULL REFERENCES content_templates(id),
    component_id      uuid NOT NULL,
    pinned_version_id uuid NOT NULL,
    PRIMARY KEY (template_id, component_id)
);

-- ============ 流水线层（docs/03-pipeline.md §9） ============

-- 12. Page Artifact（构建产物元数据，不保存文件字节）
CREATE TABLE IF NOT EXISTS page_artifacts (
    id                           uuid PRIMARY KEY,
    page_id                      uuid NOT NULL REFERENCES pages(id),
    version                      bigint NOT NULL,
    source_document              jsonb NOT NULL,
    page_document_schema_version integer NOT NULL,
    source_hash                  text NOT NULL,
    build_input_manifest         jsonb NOT NULL,
    build_input_hash             text NOT NULL,
    artifact_provider            text NOT NULL,
    artifact_key                 text NOT NULL,
    artifact_hash                text NOT NULL,
    compiler_version             text NOT NULL,
    registry_version             text NOT NULL,
    manifest                     jsonb NOT NULL,
    payload_state                text NOT NULL DEFAULT 'available',
    payload_deleted_at           timestamptz NULL,
    note                         text NOT NULL DEFAULT '',
    created_by                   uuid NOT NULL,
    created_at                   timestamptz NOT NULL,
    UNIQUE (page_id, version),
    UNIQUE (id, page_id),
    CHECK (payload_state IN ('available', 'gc_pending', 'deleted'))
);

-- 13. 共享内容对象（Locator 投影）
CREATE TABLE IF NOT EXISTS content_objects (
    content_hash   text PRIMARY KEY,
    provider       text NOT NULL,
    object_key     text NOT NULL,
    byte_size      bigint NOT NULL CHECK (byte_size >= 0),
    created_at     timestamptz NOT NULL,
    deleted_at     timestamptz NULL,
    UNIQUE (provider, object_key)
);

CREATE TABLE IF NOT EXISTS page_artifact_objects (
    artifact_id   uuid NOT NULL REFERENCES page_artifacts(id),
    content_hash  text NOT NULL REFERENCES content_objects(content_hash),
    PRIMARY KEY (artifact_id, content_hash)
);

-- 14. Page 依赖记录（依赖失效与重建队列依据）
CREATE TABLE IF NOT EXISTS page_dependencies (
    page_id          uuid NOT NULL,
    artifact_id      uuid NOT NULL,
    dependency_kind  text NOT NULL CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime'
    )),
    dependency_key   text NOT NULL,
    revision         text NULL,
    last_checked     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (artifact_id, dependency_kind, dependency_key),
    FOREIGN KEY (artifact_id, page_id)
        REFERENCES page_artifacts(id, page_id)
);

-- 15. PresentationInstance Artifact
CREATE TABLE IF NOT EXISTS presentation_artifacts (
    id                           uuid PRIMARY KEY,
    presentation_instance_id     uuid NOT NULL REFERENCES presentation_instances(id),
    snapshot_id                  uuid NOT NULL,
    version                      bigint NOT NULL,
    source_hash                  text NOT NULL,
    build_input_manifest         jsonb NOT NULL,
    build_input_hash             text NOT NULL,
    artifact_provider            text NOT NULL,
    artifact_key                 text NOT NULL,
    artifact_hash                text NOT NULL,
    compiler_version             text NOT NULL,
    registry_version             text NOT NULL,
    manifest                     jsonb NOT NULL,
    payload_state                text NOT NULL DEFAULT 'available',
    payload_deleted_at           timestamptz NULL,
    note                         text NOT NULL DEFAULT '',
    created_by                   uuid NOT NULL,
    created_at                   timestamptz NOT NULL,
    UNIQUE (presentation_instance_id, version),
    UNIQUE (id, presentation_instance_id),
    FOREIGN KEY (snapshot_id, presentation_instance_id)
        REFERENCES document_snapshots(id, presentation_instance_id),
    CHECK (payload_state IN ('available', 'gc_pending', 'deleted'))
);

CREATE TABLE IF NOT EXISTS presentation_artifact_objects (
    artifact_id   uuid NOT NULL REFERENCES presentation_artifacts(id),
    content_hash  text NOT NULL REFERENCES content_objects(content_hash),
    PRIMARY KEY (artifact_id, content_hash)
);

-- 16. PresentationInstance 依赖记录
CREATE TABLE IF NOT EXISTS presentation_dependencies (
    presentation_id  uuid NOT NULL,
    artifact_id      uuid NOT NULL,
    dependency_kind  text NOT NULL CHECK (dependency_kind IN (
        'direct_content', 'content_collection', 'content_template',
        'menu', 'media', 'global_component', 'site_setting', 'runtime'
    )),
    dependency_key   text NOT NULL,
    revision         text NULL,
    last_checked     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (artifact_id, dependency_kind, dependency_key),
    FOREIGN KEY (artifact_id, presentation_id)
        REFERENCES presentation_artifacts(id, presentation_instance_id)
);

-- 17. 发布事件（审计）
CREATE TABLE IF NOT EXISTS publication_events (
    id                   uuid PRIMARY KEY,
    source_type          text NOT NULL CHECK (source_type IN ('page', 'presentation')),
    page_id              uuid NULL REFERENCES pages(id),
    presentation_id      uuid NULL REFERENCES presentation_instances(id),
    action               text NOT NULL,
    path                 text NOT NULL,
    from_artifact_id     uuid NULL,
    to_artifact_id       uuid NULL,
    receipt              jsonb NOT NULL,
    created_by           uuid NOT NULL,
    created_at           timestamptz NOT NULL,
    CHECK (
        (source_type = 'page' AND page_id IS NOT NULL AND presentation_id IS NULL)
        OR (source_type = 'presentation' AND page_id IS NULL AND presentation_id IS NOT NULL)
    )
);

-- 18. 构建队列
CREATE TABLE IF NOT EXISTS build_jobs (
    id                uuid PRIMARY KEY,
    source_type       text NOT NULL CHECK (source_type IN ('page', 'presentation')),
    source_id         uuid NOT NULL,
    draft_version     bigint NOT NULL,
    build_input_hash  text NOT NULL,
    status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'superseded', 'failed', 'succeeded')),
    artifact_id       uuid NULL,
    error_message     text NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    started_at        timestamptz NULL,
    completed_at      timestamptz NULL
);

-- 19. 发布回执（故障恢复：pending → committed / rolled_back）
CREATE TABLE IF NOT EXISTS publication_receipts (
    id                uuid PRIMARY KEY,
    source_type       text NOT NULL CHECK (source_type IN ('page', 'presentation')),
    source_id         uuid NOT NULL,
    action            text NOT NULL,
    path              text NOT NULL,
    from_artifact_id  uuid NULL,
    to_artifact_id    uuid NULL,
    receipt_state     text NOT NULL CHECK (receipt_state IN ('pending', 'committed', 'rolled_back')),
    receipt_data      jsonb NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz NULL
);

-- ============ 复合外键（全部基表就绪后统一补加，幂等） ============

-- PresentationInstance 当前快照复合外键
DO $$ BEGIN
    ALTER TABLE presentation_instances
        ADD CONSTRAINT presentation_instances_snapshot_fk
            FOREIGN KEY (current_snapshot_id, id)
            REFERENCES document_snapshots(id, presentation_instance_id);
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- pages 的 staged/active Artifact 复合外键
DO $$ BEGIN
    ALTER TABLE pages
        ADD CONSTRAINT pages_staged_artifact_fk
            FOREIGN KEY (staged_artifact_id, id)
            REFERENCES page_artifacts(id, page_id);
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    ALTER TABLE pages
        ADD CONSTRAINT pages_active_artifact_fk
            FOREIGN KEY (active_artifact_id, id)
            REFERENCES page_artifacts(id, page_id);
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- presentation_instances 的 staged/active Artifact 复合外键
DO $$ BEGIN
    ALTER TABLE presentation_instances
        ADD CONSTRAINT presentation_instances_staged_artifact_fk
            FOREIGN KEY (staged_artifact_id, id)
            REFERENCES presentation_artifacts(id, presentation_instance_id);
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
    ALTER TABLE presentation_instances
        ADD CONSTRAINT presentation_instances_active_artifact_fk
            FOREIGN KEY (active_artifact_id, id)
            REFERENCES presentation_artifacts(id, presentation_instance_id);
EXCEPTION WHEN duplicate_object THEN NULL; END $$;