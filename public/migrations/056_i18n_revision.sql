-- 056 · sys_i18n_revision（单行资源版本号）
--
-- 对齐 a2 历史结构：i18n 资源版本号，单调递增（SWR 协商用）。
-- 幂等：CREATE TABLE IF NOT EXISTS + INSERT ... ON CONFLICT DO NOTHING。
CREATE TABLE IF NOT EXISTS sys_i18n_revision (
    id          SMALLINT     PRIMARY KEY DEFAULT 1,
    revision    BIGINT       NOT NULL DEFAULT 0,
    update_time TIMESTAMP(3),
    CONSTRAINT ck_sys_i18n_revision_single CHECK (id = 1)
);
INSERT INTO sys_i18n_revision (id, revision, update_time)
VALUES (1, 0, now())
ON CONFLICT (id) DO NOTHING;
COMMENT ON TABLE sys_i18n_revision IS 'i18n 资源版本号（单行，单调递增）';
COMMENT ON COLUMN sys_i18n_revision.revision IS '资源版本号';
