-- 175_archive_presentation_role.sql — 归档型模板与实例（审计 EDT-004）。
--
-- 背景：presentation 此前只服务「实体详情页」—— 一个实体（分类 / 品牌 / 标签）一个实例。
-- 但「分类归档页」（该分类下的商品列表）与「分类详情页」是**同一个实体**的两张不同页面：
-- 前者列商品、后者讲这个分类。没有角色维度时，两者会争同一个唯一键，
-- 后建的那个会被当成「已存在」而直接返回前一个实例。
--
-- 因此加两个角色列：模板侧 template_role（detail / archive）决定这套模板画的是什么，
-- 实例侧 instance_role 决定这个实例是哪一张页面。都是 NOT NULL DEFAULT 'detail'，
-- 既有数据自动落到详情语义，行为逐字不变。

ALTER TABLE content_templates
    ADD COLUMN IF NOT EXISTS template_role text NOT NULL DEFAULT 'detail';

ALTER TABLE presentation_instances
    ADD COLUMN IF NOT EXISTS instance_role text NOT NULL DEFAULT 'detail';

-- 唯一键从「实体」放宽到「实体 + 角色」：同一个分类可以同时有详情页与归档页实例。
-- 用 DROP CONSTRAINT IF EXISTS + CREATE UNIQUE INDEX 而不是 ADD CONSTRAINT：
-- 约束名由 PostgreSQL 自动生成，不同环境可能不同，按名字删约束会静默失败。
ALTER TABLE presentation_instances
    DROP CONSTRAINT IF EXISTS presentation_instances_entity_type_entity_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS uq_presentation_instances_entity_role
    ON presentation_instances (entity_type, entity_id, instance_role);

-- 归档实例按「工程 + 角色」查（列出某工程的全部归档页 / 只挑详情页），
-- 与上面那条唯一索引的前缀不同，必须单独建。
CREATE INDEX IF NOT EXISTS idx_presentation_instances_project_role
    ON presentation_instances (project_id, instance_role);

-- 模板侧同理：取默认模板时要能只挑该角色的模板。
CREATE INDEX IF NOT EXISTS idx_content_templates_project_role
    ON content_templates (project_id, entity_type, template_role);
