-- 167_index_foundation.sql
--
-- 索引地基：让「模糊搜索」「按时间的列表与 GC」「会话/邮件台账」真正有索引可用。
-- 对应审计条目 DB-001（pg_trgm 未启用）、IDX-002（订单列表）、IDX-004（产物 GC）、
-- IDX-006（商品列表）、IDX-009（客户列表）、IDX-011（活跃设备）、IDX-012（邮件事件）、
-- IDX-013（冗余索引清理）。

-- ── 1. pg_trgm ───────────────────────────────────────────────────────────────
-- 全库原本只装了 plpgsql，而代码里有十余处 ILIKE '%x%'（订单三列、商品名、客户四列、
-- 媒体文件名、主数据实体名）—— 没有扩展时这些查询只能全表扫。
-- 安装扩展需要建库角色有权限（superuser / rds_superuser）：这里刻意**不吞异常**，
-- 建不上就让迁移失败并暴露出来，而不是留下「索引建了、其实用不上」的静默状态。
--
-- 固定装到专用 schema ext_shared：ext_shared 只承载扩展对象、不会被业务表污染（借 public 会让它
-- 进入测试 search_path 后干扰迁移的 to_regclass 判定）。不带 SCHEMA 会装进「当前 schema」，
-- 而 pg_trgm 是库级唯一的 —— 那会让它只对第一个跑迁移的 schema 生效，并行的其它 schema
-- 解析不到 gin_trgm_ops（见 210）。
CREATE SCHEMA IF NOT EXISTS ext_shared;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA ext_shared;

-- ── 2. 订单列表（IDX-002）────────────────────────────────────────────────────
-- 关键词对 order_no / customer_email / customer_name 三列做 OR ILIKE。
CREATE INDEX IF NOT EXISTS idx_orders_order_no_trgm ON orders USING gin (order_no gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_orders_customer_email_trgm ON orders USING gin (customer_email gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_orders_customer_name_trgm ON orders USING gin (customer_name gin_trgm_ops);
-- 列表条件：project_id [+ status] [+ create_time 范围]，按 create_time 倒序。
-- 既有的 (project_id, status) 与 (create_time DESC) 各自都不够用，需要带排序键的复合索引。
CREATE INDEX IF NOT EXISTS idx_orders_project_status_time ON orders (project_id, status, create_time DESC);

-- ── 3. 商品列表（IDX-006）────────────────────────────────────────────────────
CREATE INDEX IF NOT EXISTS idx_products_name_trgm ON products USING gin (name gin_trgm_ops);
-- 集合源 ListForCollection 常带 project_id + status，按 sort / created_at / id 排序。
CREATE INDEX IF NOT EXISTS idx_products_project_status_sort ON products (project_id, status, sort, created_at, id);

-- ── 4. 客户列表（IDX-009）────────────────────────────────────────────────────
-- 四列 OR ILIKE 若各建一个 GIN，写入侧要维护四个索引；改成一个生成列 + 单个 trgm 索引：
-- 搜索列由 username / email / nickname / display_name 拼接并小写。生成列由写入侧自动
-- 计算，不需要改任何写入逻辑；查询侧改为对 search_text 匹配。
ALTER TABLE users ADD COLUMN IF NOT EXISTS search_text text GENERATED ALWAYS AS (
    lower(coalesce(username, '') || ' ' || coalesce(email, '') || ' ' || coalesce(nickname, '') || ' ' || coalesce(display_name, ''))
) STORED;
CREATE INDEX IF NOT EXISTS idx_users_search_trgm ON users USING gin (search_text gin_trgm_ops);

-- ── 5. 媒体文件名与主数据实体名（DB-001 的覆盖面）────────────────────────────
CREATE INDEX IF NOT EXISTS idx_att_file_name_trgm ON sys_attachment USING gin (file_name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_master_data_changes_label_trgm ON master_data_changes USING gin (entity_label gin_trgm_ops);

-- ── 6. 产物 GC 与对账（IDX-004）──────────────────────────────────────────────
-- 三条查询各自需要索引：GC 候选（payload_state + created_at）、registry 版本扫描
-- （payload_state + registry_version）、按 hash 查产物（page_id + artifact_hash）。
CREATE INDEX IF NOT EXISTS idx_page_artifacts_gc ON page_artifacts (created_at) WHERE payload_state = 'available';
CREATE INDEX IF NOT EXISTS idx_page_artifacts_registry ON page_artifacts (registry_version) WHERE payload_state = 'available';
CREATE INDEX IF NOT EXISTS idx_page_artifacts_page_hash ON page_artifacts (page_id, artifact_hash);


-- ── 8. 邮件事件（IDX-012）────────────────────────────────────────────────────
-- 报表按活动 + 时间窗聚合；既有索引是 (campaign_id, event_type)，缺时间维度。
CREATE INDEX IF NOT EXISTS idx_mail_campaign_events_time ON mail_campaign_events (campaign_id, create_time DESC);

-- ── 9. 冗余索引清理（IDX-013）────────────────────────────────────────────────
-- sys_attachment 只有 6 行数据却挂了 15 个索引。删除的四条都有确定的替代物：
--   idx_att_md5_type    与唯一索引 uq_attachment_md5_type_active 同列同谓词，完全重复
--   idx_att_category_id / idx_att_status / idx_att_file_type 分别是
--   idx_att_cat_time_alive / idx_att_status_time / idx_att_type_status 的前缀，永远走不到
DROP INDEX IF EXISTS idx_att_md5_type;
DROP INDEX IF EXISTS idx_att_category_id;
DROP INDEX IF EXISTS idx_att_status;
DROP INDEX IF EXISTS idx_att_file_type;
