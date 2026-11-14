-- 初始化基线载入脚本：结构 + 系统数据 + 台账一行。
-- 用法：psql -v ON_ERROR_STOP=1 -d <新库> -f schema/baseline/init.sql
-- 载入后再跑 ./app -migrate-only：迁移器会跳过 manifest 内已覆盖的历史迁移。
\set ON_ERROR_STOP on
-- pg_dump 会把 search_path 清空（防恢复期受它影响），而触发器函数用未限定表名；
-- 载入系统数据前必须把 search_path 设回来，否则 COPY 触发触发器时报 relation 不存在。
SET search_path = public, ext_shared;
\ir structure.sql
SET search_path = public, ext_shared;
\ir system_data.sql

INSERT INTO sys_baseline (version, manifest_sha256, applied_at, note)
VALUES ('baseline-2026-11-15', '01bcfdf34259c6047613a2a172e8c35968f5ce285031f888b8bd69e6e076fdb0', now(), '由 scripts/baseline-generate.sh 生成');
