CREATE TABLE IF NOT EXISTS sys_baseline (
    id              BIGSERIAL    PRIMARY KEY,
    version         TEXT         NOT NULL,
    manifest_sha256 CHAR(64)     NOT NULL,
    applied_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    note            TEXT         NOT NULL DEFAULT ''
);
