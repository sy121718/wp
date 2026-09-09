-- 070 · PG 优化第二梯队：sys_* 软删除/状态列索引核对 + 三处「真实查询条件未被索引覆盖」补建
--
-- 背景（第一批 068 的遗留项 ①）：sys_* 表的软删除/状态列名与 pages 不同
--   · sys_menus        → deleted_time（不是 deleted_at）
--   · sys_attachment   → status（0=草稿/停用，1=启用，软删除即置 0）
--   · sys_file_category / sys_permission / sys_role / sys_rule / sys_i18n / sys_admin → status
--   · sys_media_variant → status 是字符串（pending/ready/failed），不是软删除
-- 本迁移的做法与第一批一致：**先逐个核对 model 里真实出现的 Where 子句，
-- 只给「查询真的会用到、且现有索引没覆盖」的列建索引**，其余明确跳过并写清原因。
--
-- 核对结论（实测数据见下）：14 张 sys_* 表里 9 张已有 status/deleted_time 相关索引，
-- 真正的缺口只有 3 处，全部落在媒体中心（sys_attachment / sys_media_variant）。
--
-- 幂等：所有语句 IF NOT EXISTS；register.go 按索引名判定
--（这几张表早已存在，默认「表存在即跳过」必然误跳过，与 037/038/068 同一手法）。
-- 在 wp_pg_lab 实验室（sys_attachment 20 万行 / sys_media_variant 60 万行）实测：
--   · 构建期按 file_path 反查附件   47.246ms → 0.156ms（约 303x）
--   · 构建期按 file_path 反查变体   74.601ms → 0.120ms（约 622x）
--   · 媒体库按分类列表              2.437ms → 0.212ms（约 11.5x）

-- ---------------------------------------------------------------------------
-- 1) 构建期媒体反查：sys_attachment.file_path（原图）
--
-- 查询形态（media_model.go 的 GetByFilePath）：
--   WHERE status = 1 AND (file_path = ? OR file_path = ?)   -- 兼容带/不带前导斜杠
-- 调用方：media_ref.go attachmentIDByURL（构建期从产物 HTML 提取的每个 /storage/ URL
-- 都要反查一次附件）、media_variant.go。产物里 N 个媒体 → N 次反查，而 file_path
-- 此前没有任何索引，每次都是全表扫（20 万行 47ms），是构建期最明显的 N+1。
-- PG 会把 a = ? OR a = ? 改写为 a = ANY(...)，一条索引即可服务两个分支
--（实测 Index Searches: 2，同一次 Index Scan）。
-- 部分条件 status = 1 与查询一致：草稿/已删除附件不参与反查，不进索引。
CREATE INDEX IF NOT EXISTS idx_att_file_path_alive
    ON sys_attachment (file_path) WHERE status = 1;

-- ---------------------------------------------------------------------------
-- 2) 构建期媒体反查：sys_media_variant.file_path（变体）
--
-- 查询形态（media_variant_model.go 的 GetByFilePath）：
--   WHERE (file_path = ? OR file_path = ?) ORDER BY id ASC
-- 页面可直接引用 <id>_thumb.jpg 这类变体地址，原图反查未命中时落到这里。
-- 该查询不带 status 条件（变体的 status 是 pending/ready/failed 的生成态，
-- 不是软删除），故这里是普通索引而非部分索引。
-- 变体行数约等于附件数 × 变体类型数（60 万行实测 74.6ms → 0.120ms）。
CREATE INDEX IF NOT EXISTS idx_mva_file_path
    ON sys_media_variant (file_path);

-- ---------------------------------------------------------------------------
-- 3) 媒体库按分类列表：sys_attachment (category_id, create_time DESC) WHERE status = 1
--
-- 查询形态（media_model.go 的 List，媒体库左树点分类）：
--   WHERE status = 1 AND category_id = ? ORDER BY create_time DESC LIMIT ?
-- 现有 idx_att_category_id 只有单列：planner 反而去扫 idx_att_status_time
-- 再按 category_id 过滤，20 万行里为凑 20 行要回表丢弃 2825 行（2.437ms）。
-- 复合 (category_id, create_time DESC) 同时服务等值与排序，部分条件去掉已删除行
--（实测 0.212ms，Index Cond: category_id = 42，无 Rows Removed by Filter）。
-- 与第一批 idx_pages_theme_alive 同一思路：等值列 + 排序列 + 存活谓词。
CREATE INDEX IF NOT EXISTS idx_att_cat_time_alive
    ON sys_attachment (category_id, create_time DESC) WHERE status = 1;

COMMENT ON INDEX idx_att_file_path_alive IS
    '构建期按 file_path 反查启用附件（media_ref.go attachmentIDByURL），部分索引 status = 1';
COMMENT ON INDEX idx_mva_file_path IS
    '构建期按 file_path 反查媒体变体（media_variant_model.go GetByFilePath）';
COMMENT ON INDEX idx_att_cat_time_alive IS
    '媒体库按分类分页列表（status = 1 AND category_id = ? ORDER BY create_time DESC），部分索引 status = 1';

-- ---------------------------------------------------------------------------
-- 4) 明确跳过的表与列（不在本迁移建索引，避免「为用 PG 特性而用」）
--
-- 实测环境：wp_pg_lab，sys_attachment 20 万行、sys_media_variant 60 万行，
-- 其余按「现实规模」与「成长规模」两档测（menus 300/1 万、file_category 300/1 万、
-- dept 200/5000、permission 500、role 50、rule 50、i18n 2 万/20 万）。
--
--   · sys_admin.status（smallint 1启用/2禁用/3封禁）
--       已有 idx_sys_admin_status 覆盖；列表查询是 is_admin != 1 + 可选 status 筛选，
--       且管理员表数量级 10^1~10^2。跳过。
--   · sys_attachment.file_type 分支（WHERE status = 1 AND file_type = ? ORDER BY create_time DESC）
--       实测建 (file_type, create_time DESC) WHERE status = 1 后 planner 仍选
--       idx_att_status_time（0.298ms，Rows Removed by Filter: 26）——image 占 3/7 行，
--       索引不划算，planner 自己不用。**不加**（加了也只是白占空间）。
--   · sys_attachment.file_name（WHERE status = 1 AND file_name LIKE '%x%'）
--       btree 无法服务前后通配的 LIKE，需要 pg_trgm 扩展；本批未启用扩展，
--       且媒体库搜索是低频人工操作（20 万行 32.8ms）。留作遗留项。
--   · sys_dept.status
--       dept_model.go 全部查询按 id / dept_code / ancestors / parent_id，没有任何
--       查询带 status 条件（DeptTree 是 ListAll 全表 + 应用层拼树）。条件不匹配，
--       部分索引不会被使用。跳过。
--   · sys_file_category.status
--       已有 idx_fc_status + idx_fc_parent_id；ListAll 全表 0.155ms（300 行）/
--       4.03ms（1 万行，整棵树一次读回），HasChildren 0.085ms。跳过。
--   · sys_i18n.status
--       已有 idx_sys_i18n_status；但 LoadCache 是全表读取（status=1 占 98%），
--       索引只会让 planner 在 index-only 与 seq scan 之间摇摆，20 万行 23.4ms 且
--       每 20s 一次的后台刷新，成本可忽略。跳过。
--   · sys_media_variant.status
--       media_variant_model.go 只按 attachment_id / file_path / id 查，无一处按
--       status 过滤。现有 idx_mva_status 实际未被任何查询使用（见遗留项）。跳过。
--   · sys_menus.deleted_time
--       全部查询都带 deleted_time IS NULL，但菜单表现实规模 300 行、成长规模 1 万行，
--       整棵树一次读回 0.263ms / 4.15ms；CountByParentID 已被 idx_sys_menus_parent_id
--       覆盖（1 万行 0.439ms）。索引不会带来可测量收益。跳过。
--   · sys_permission.status
--       已有 idx_sys_permission_status + idx_sys_permission_module + uk_sys_permission_code；
--       ListEnabled 0.129ms、ListByModule 0.122ms、ExistsByCode 0.073ms。跳过。
--   · sys_role.status
--       唯一的 status 条件在 GetEnabledIDsByCodes（role_code IN ? AND status = ?），
--       role_code 已被 uk_sys_role_code 命中（0.040ms）。跳过。
--   · sys_rule.status
--       已有 idx_sys_rule_status + idx_sys_rule_domain；GetRules（domain = ? AND status = ?）
--       在 50 行规模 0.060ms，数据规则不会到 10^3 级。跳过。
--   · sys_translation / sys_rule_assignment / sys_casbin_rule / sys_i18n_revision
--       无软删除/状态列（sys_translation 按 source_hash+context+lang 内容寻址）。
--       不涉及。
