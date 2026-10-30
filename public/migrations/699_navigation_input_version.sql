-- 699 · 实体输入代次（page / presentation 发布侧）。
--
-- 背景：发布激活的判据此前是「提交顺序号」（epoch = cur + 1），它只能证明「谁后提交」，
-- 证明不了「谁编译的是更新的输入」—— 旧输入晚到照样拿到更大的号并覆盖新产物。
-- 本迁移引入**实体输入代次** input_version：由输入变更的那个事务原子 +1，
-- 激活只验证（WHERE input_version = $expect）不推进。
--
-- 本迁移只加两列 + 放宽一处 CHECK，不做任何回填：
--   · pages.input_version                —— 手工页面的编译输入代次；
--   · presentation_instances.input_version —— 自动发布实例的编译输入代次；
--   · publication_receipts.receipt_state 增加 'superseded' 终态
--     （「本次尝试被更权威的结果取代」，与 'rolled_back'「没生效」区分）。
--
-- 与迁移 698（build owner）严格分离：那边是 build_jobs 的**任务序列号**，
-- 这边的实体版本与任务号不互相赋值（任务表会被 retention 清理，号会倒退）。
--
-- 幂等：ADD COLUMN IF NOT EXISTS；约束先 DROP 再 ADD。可重复执行。
-- 不加 BEGIN/COMMIT：迁移器（ApplyStatements）已把整批语句包在同一个事务里。
-- ADD COLUMN … NOT NULL DEFAULT 0 在 PG 11+ 不重写表（只改元数据）。

ALTER TABLE pages ADD COLUMN IF NOT EXISTS input_version bigint NOT NULL DEFAULT 0;
ALTER TABLE presentation_instances ADD COLUMN IF NOT EXISTS input_version bigint NOT NULL DEFAULT 0;

COMMENT ON COLUMN pages.input_version IS '页面编译输入代次：输入变更（草稿/译文扇出/依赖标记）同事务 +1，激活只验证不推进';
COMMENT ON COLUMN presentation_instances.input_version IS '实例编译输入代次：输入变更同事务 +1，激活只验证不推进';

-- 回执新增终态 superseded（只放宽 CHECK，不改列型）。
ALTER TABLE publication_receipts DROP CONSTRAINT IF EXISTS publication_receipts_receipt_state_check;
ALTER TABLE publication_receipts ADD CONSTRAINT publication_receipts_receipt_state_check
    CHECK (receipt_state IN ('pending', 'committed', 'rolled_back', 'superseded'));
