-- 281 · presentation_instances 实例级文档覆盖（override_document）。
--
-- 背景：商品级可视化自定义（docs/04-C-instance-override.md，方案 B）——
-- 商品在选模板后可进入 workbench 画布改组件、调结构，保存的是**该实例自己的文档**，
-- 不影响共享模板，也不影响同模板的其他商品。
--
-- 语义：
--   NULL = 跟随模板（既有行为不变，重建照旧解析模板文档，零回归）；
--   非空 = 该实例发布/重建时以此文档为准（binding 节点照常经 ContentResolver
--          取最新实体数据 —— 实体数据更新后重建不丢自定义）。
-- 模板切换（换底稿）时由 service 层清除此列（同事务），即「放弃自定义」。
--
-- 时间列沿用 *_time 口径（审计 DB-019/212），本列非时间列；类型 jsonb 与
-- document_snapshots.document 同口径，列型真相由本迁移唯一声明（model 不重复声明）。
ALTER TABLE presentation_instances ADD COLUMN IF NOT EXISTS override_document jsonb;

COMMENT ON COLUMN presentation_instances.override_document IS '实例级文档覆盖；NULL=跟随模板';
