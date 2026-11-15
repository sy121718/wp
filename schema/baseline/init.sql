-- 初始化基线载入脚本：结构 + 系统数据 + 台账一行。
-- 用法：psql -v ON_ERROR_STOP=1 -d <新库> -f schema/baseline/init.sql
-- 载入后再跑 ./app -migrate-only：迁移器会跳过基线覆盖的迁移与 seed，只执行段外增量。
\set ON_ERROR_STOP on
-- pg_dump 会把 search_path 清空（防恢复期受它影响），而触发器函数用未限定表名；
-- 载入系统数据前必须把 search_path 设回来，否则 COPY 触发触发器时报 relation 不存在。
SET search_path = public, ext_shared;
\ir structure.sql
SET search_path = public, ext_shared;
\ir system_data.sql

INSERT INTO sys_baseline (version, manifest_sha256, applied_at, note)
VALUES ('baseline-8a6d0f78', '8a6d0f7857d7f15b92c30f11dd88dabfe7f1506d6a6ce9227075f9d3a9331eda', now(), '由 scripts/baseline-generate.sh 生成');
