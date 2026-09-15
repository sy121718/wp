-- 173_partition_append_only_tables.sql
--
-- 审计 DB-004：三张只增表（page_views / inventory_stock_movements / master_data_changes）未分区。
-- 分区后：时间窗查询可走分区裁剪、过期数据能整分区 DETACH 归档（不必逐行 DELETE，
-- 也不产生 VACUUM 压力）、append-only 触发器的作用范围可按分区界定。
--
-- 三条硬约束决定了这段 SQL 的形状：
--   1. **主键必须含分区键**：PostgreSQL 要求分区表的唯一约束包含全部分区列，
--      因此主键从 (id) 改为 (id, <时间列>)。实测这三张表没有任何外键指向，改主键不影响引用方。
--   2. **迁移器逐语句执行、不包事务**（public/migrations/migrator.go），所以每一步都必须幂等：
--      中途失败后重跑是唯一的恢复手段，而重跑不能产生「数据搬两遍」或「旧表已删」。
--   3. **DEFAULT 分区兜底**：page_views 的写入方是访客打点（静默失败），
--      一旦某个时刻缺分区，没有 DEFAULT 会直接丢数据且无人察觉。
--
-- 迁移方式：重命名旧表 → 建分区父表 → 建分区 → 搬数据（ON CONFLICT DO NOTHING）→ 删旧表 →
-- 重建索引与触发器。数据搬运走一条 INSERT ... SELECT，整个过程可重复执行。

-- ── 1. page_views（按 viewed_at 月分区）─────────────────────────────────────────

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'page_views' AND n.nspname = current_schema() AND c.relkind = 'p') THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'page_views' AND n.nspname = current_schema() AND c.relkind = 'r')
       AND NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'page_views_legacy' AND n.nspname = current_schema()) THEN
        ALTER TABLE page_views RENAME TO page_views_legacy;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS page_views (
    id            bigserial   NOT NULL,
    project_id    uuid        NOT NULL,
    path          text        NOT NULL,
    lang          text        NOT NULL DEFAULT '',
    session_id    text        NOT NULL DEFAULT '',
    visitor_hash  text        NOT NULL DEFAULT '',
    referrer_host text        NOT NULL DEFAULT '',
    ua_class      text        NOT NULL DEFAULT '',
    ip_hash       text        NOT NULL DEFAULT '',
    viewed_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_page_views_path_len  CHECK (char_length(path) > 0 AND char_length(path) <= 512),
    CONSTRAINT chk_page_views_path_root CHECK (left(path, 1) = '/'),
    CONSTRAINT chk_page_views_lang_len  CHECK (char_length(lang) <= 35),
    CONSTRAINT chk_page_views_sess_len  CHECK (char_length(session_id) <= 64),
    CONSTRAINT chk_page_views_hash_len  CHECK (char_length(visitor_hash) <= 64),
    CONSTRAINT chk_page_views_ip_len    CHECK (char_length(ip_hash) <= 64),
    CONSTRAINT chk_page_views_ref_len   CHECK (char_length(referrer_host) <= 255),
    CONSTRAINT chk_page_views_ua_class  CHECK (ua_class IN ('', 'desktop', 'tablet', 'mobile', 'bot')),
    PRIMARY KEY (id, viewed_at)
) PARTITION BY RANGE (viewed_at);

-- ── 2. inventory_stock_movements（按 created_at 月分区）─────────────────────────

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'inventory_stock_movements' AND n.nspname = current_schema() AND c.relkind = 'p') THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'inventory_stock_movements' AND n.nspname = current_schema() AND c.relkind = 'r')
       AND NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'inventory_stock_movements_legacy' AND n.nspname = current_schema()) THEN
        ALTER TABLE inventory_stock_movements RENAME TO inventory_stock_movements_legacy;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS inventory_stock_movements (
    id                uuid        NOT NULL DEFAULT gen_random_uuid(),
    project_id        uuid        NOT NULL,
    warehouse_id      uuid        NOT NULL,
    product_id        uuid        NOT NULL,
    variant_id        uuid        NOT NULL,
    sku_code          text        NOT NULL DEFAULT '',
    direction         text        NOT NULL,
    quantity          integer     NOT NULL,
    delta             integer     NOT NULL,
    quantity_before   integer     NOT NULL,
    quantity_after    integer     NOT NULL,
    reason_id         uuid,
    reason_code       text        NOT NULL,
    parent_variant_id uuid,
    source_type       text        NOT NULL DEFAULT '',
    source_ref        text        NOT NULL DEFAULT '',
    remark            text        NOT NULL DEFAULT '',
    operator_id       text        NOT NULL DEFAULT '',
    batch_id          uuid        NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_stock_movements_direction_check CHECK (direction IN ('in', 'out', 'adjust')),
    CONSTRAINT inventory_stock_movements_quantity_check  CHECK (quantity > 0),
    CONSTRAINT inventory_stock_movements_after_check     CHECK (quantity_after >= 0),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- ── 3. master_data_changes（按 created_at 月分区）───────────────────────────────

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'master_data_changes' AND n.nspname = current_schema() AND c.relkind = 'p') THEN
        RETURN;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'master_data_changes' AND n.nspname = current_schema() AND c.relkind = 'r')
       AND NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'master_data_changes_legacy' AND n.nspname = current_schema()) THEN
        ALTER TABLE master_data_changes RENAME TO master_data_changes_legacy;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS master_data_changes (
    id           uuid        NOT NULL DEFAULT gen_random_uuid(),
    project_id   uuid        NOT NULL,
    entity_type  text        NOT NULL,
    entity_id    uuid        NOT NULL,
    entity_label text        NOT NULL DEFAULT '',
    action       text        NOT NULL,
    field        text        NOT NULL,
    old_value    text        NOT NULL DEFAULT '',
    new_value    text        NOT NULL DEFAULT '',
    origin       text        NOT NULL DEFAULT '',
    operator_id  text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT master_data_changes_action_check      CHECK (action IN ('create', 'update', 'delete')),
    CONSTRAINT master_data_changes_entity_type_check CHECK (entity_type IN ('product', 'product_variant', 'inventory_source')),
    CONSTRAINT master_data_changes_field_check       CHECK (field <> ''),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- ── 4. 分区：覆盖旧数据范围 + 未来窗口 + DEFAULT 兜底 ───────────────────────────
--
-- 窗口取「旧表最早数据所在月」到「当前月 + 2 个月」，另加 DEFAULT 分区兜底。
-- 日常维护由 internal/partition 的 EnsureAhead 负责提前建分区（启动时与每日各一次）。

DO $$
DECLARE
    spec       RECORD;
    -- 用两个独立变量而不是一个 RECORD：RECORD 在赋值前没有结构，
    -- 直接 SELECT ... INTO span.a, span.b 会报「record is not assigned yet」。
    v_from     timestamptz;
    v_to       timestamptz;
    from_month date;
    to_month   date;
    cur        date;
    part_name  text;
BEGIN
    FOR spec IN SELECT * FROM (VALUES
            ('page_views', 'viewed_at'),
            ('inventory_stock_movements', 'created_at'),
            ('master_data_changes', 'created_at')
        ) AS t(tbl, tcol)
    LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                       WHERE c.relname = spec.tbl AND n.nspname = current_schema() AND c.relkind = 'p') THEN
            CONTINUE;
        END IF;
        EXECUTE format('SELECT MIN(%I), MAX(%I) FROM %I', spec.tcol, spec.tcol, spec.tbl) INTO v_from, v_to;
        from_month := COALESCE(date_trunc('month', v_from)::date, date_trunc('month', now())::date);
        to_month := GREATEST(COALESCE(date_trunc('month', v_to)::date, from_month), date_trunc('month', now())::date);
        cur := from_month;
        WHILE cur <= (to_month + INTERVAL '2 months')::date LOOP
            part_name := format('%s_%s', spec.tbl, to_char(cur, 'YYYY_MM'));
            EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
                           part_name, spec.tbl, cur, (cur + INTERVAL '1 month')::date);
            cur := (cur + INTERVAL '1 month')::date;
        END LOOP;
        EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I DEFAULT', spec.tbl || '_default', spec.tbl);
    END LOOP;
END $$;

-- ── 5. 搬数据（幂等：ON CONFLICT DO NOTHING）────────────────────────────────────

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'page_views_legacy' AND n.nspname = current_schema()) THEN
        INSERT INTO page_views (id, project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at)
        SELECT id, project_id, path, lang, session_id, visitor_hash, referrer_host, ua_class, ip_hash, viewed_at
        FROM page_views_legacy ON CONFLICT DO NOTHING;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'inventory_stock_movements_legacy' AND n.nspname = current_schema()) THEN
        INSERT INTO inventory_stock_movements (id, project_id, warehouse_id, product_id, variant_id, sku_code, direction,
            quantity, delta, quantity_before, quantity_after, reason_id, reason_code, parent_variant_id, source_type,
            source_ref, remark, operator_id, batch_id, created_at)
        SELECT id, project_id, warehouse_id, product_id, variant_id, sku_code, direction,
            quantity, delta, quantity_before, quantity_after, reason_id, reason_code, parent_variant_id, source_type,
            source_ref, remark, operator_id, batch_id, created_at
        FROM inventory_stock_movements_legacy ON CONFLICT DO NOTHING;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
               WHERE c.relname = 'master_data_changes_legacy' AND n.nspname = current_schema()) THEN
        INSERT INTO master_data_changes (id, project_id, entity_type, entity_id, entity_label, action, field,
            old_value, new_value, origin, operator_id, created_at)
        SELECT id, project_id, entity_type, entity_id, entity_label, action, field,
            old_value, new_value, origin, operator_id, created_at
        FROM master_data_changes_legacy ON CONFLICT DO NOTHING;
    END IF;
END $$;

DROP TABLE IF EXISTS page_views_legacy;
DROP TABLE IF EXISTS inventory_stock_movements_legacy;
DROP TABLE IF EXISTS master_data_changes_legacy;

-- ── 6. 索引（父表上建，自动下沉到分区）──────────────────────────────────────────
--
-- 自带 CREATE EXTENSION：entity_label 的 trgm 索引依赖它，而扩展按 schema 解析
--（169 踩过这个坑：在别的 schema 里装过，这里就找不到 gin_trgm_ops）。幂等，已装时空操作。

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_page_views_project_time ON page_views (project_id, viewed_at DESC);
CREATE INDEX IF NOT EXISTS idx_page_views_project_path_time ON page_views (project_id, path, viewed_at DESC);

CREATE INDEX IF NOT EXISTS idx_inventory_movements_project_time ON inventory_stock_movements (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_variant_time ON inventory_stock_movements (variant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_wh_time ON inventory_stock_movements (warehouse_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_movements_reason ON inventory_stock_movements (reason_code);
-- batch_id 索引补上分区键：不含分区键的索引在分区表上只能逐分区查（跨分区批次回查会扫全部活跃分区）。
CREATE INDEX IF NOT EXISTS idx_inventory_movements_batch ON inventory_stock_movements (batch_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_master_data_changes_time ON master_data_changes (project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_master_data_changes_entity ON master_data_changes (project_id, entity_type, entity_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_master_data_changes_field ON master_data_changes (project_id, entity_type, field, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_master_data_changes_label_trgm ON master_data_changes USING gin (entity_label gin_trgm_ops);

-- ── 7. append-only 触发器（父表上建，PG 会为每个分区自动建子触发器）──────────────
--
-- 关键在于**父表上也有这条触发器**：直接往分区里写（或将来 ATTACH 进来的分区）同样被拦。
-- 旧的 _legacy 表已删除，其上的触发器随之消失，不会留下悬空定义。

DROP TRIGGER IF EXISTS trg_master_data_changes_append_only ON master_data_changes;
CREATE TRIGGER trg_master_data_changes_append_only
    BEFORE UPDATE OR DELETE ON master_data_changes
    FOR EACH ROW EXECUTE FUNCTION master_data_changes_append_only();

COMMENT ON TABLE page_views IS '页面浏览明细（BIZ-8；按月分区，审计 DB-004）：分区按 viewed_at，DEFAULT 分区兜底缺窗口';
COMMENT ON TABLE inventory_stock_movements IS '库存流水（issue #16；按月分区，审计 DB-004）：每次真源变动一行';
COMMENT ON TABLE master_data_changes IS '主数据变更记录（issue #19；按月分区 + append-only 触发器）：字段级审计';
