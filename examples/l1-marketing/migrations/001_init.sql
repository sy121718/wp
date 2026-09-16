-- 001_init.sql — campaignkit 插件 L1 数据层初始化（docs/06-plugin-system.md §8）
-- 执行器已把 search_path 锁定到 plugin_campaignkit，未限定名的对象不会外溢到 public；
-- 语句内的「DROP SCHEMA / GRANT / 访问 public. / 文件与系统目录」一律被安全策略拒绝。
CREATE SCHEMA IF NOT EXISTS plugin_campaignkit;

CREATE TABLE IF NOT EXISTS campaigns (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text NOT NULL UNIQUE,
    title       text NOT NULL,
    discount    text NOT NULL DEFAULT '',
    starts_time timestamptz NOT NULL DEFAULT now(),
    ends_time   timestamptz,
    create_time timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_campaigns_starts_time ON campaigns (starts_time DESC);
