-- 001_init.sql — l0demo 插件 L1 数据层初始化
CREATE SCHEMA IF NOT EXISTS plugin_l0demo;

CREATE TABLE IF NOT EXISTS plugin_l0demo.demo_records (
    id   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name text NOT NULL,
    create_time timestamptz NOT NULL DEFAULT now()
);
