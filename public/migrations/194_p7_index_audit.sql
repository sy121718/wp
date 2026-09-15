-- 194_p7_index_audit.sql
--
-- 审计 P7 索引类四条（IDX-008 / IDX-017 / IDX-018 / IDX-020）的落地。
--
-- 核对口径：先取 pg_stat_user_indexes 的实际 idx_scan，再逐条比对代码里的真实查询面。
-- 结论是四条里只有两条需要 DDL —— 另外两条的审计前提与现状不符，理由留在各自小节里，
-- 不为了「有交代」而硬加索引。
-- 迁移器逐语句执行且不包事务（public/migrations/migrator.go），所以每条都必须幂等。

-- ── 1. IDX-008：删仓守卫的部分索引 ──────────────────────────────────────────
--
-- 查询：internal/module/product/inventory/model/inventory_model.go 的 CountNonZeroStocks
--       WHERE warehouse_id = ? AND quantity <> 0
--
-- 099 的 idx_inventory_stocks_warehouse 是单列全量索引：命中后仍要把该仓所有行取回来
-- 逐行过滤 quantity。删仓守卫只关心「还有没有非零行」，部分索引把非零行直接编进去，
-- 命中即答案，索引体积也随非零行比例下降。
--
-- 不删 099 的单列索引：按仓库的其它统计（CountStockByWarehouse 等）仍要用它。

CREATE INDEX IF NOT EXISTS idx_inventory_stocks_warehouse_nonzero
    ON inventory_stocks (warehouse_id)
    WHERE quantity <> 0;

COMMENT ON INDEX idx_inventory_stocks_warehouse_nonzero IS '删仓守卫：非零库存计数（审计 IDX-008）';

-- ── 2. IDX-020：page_routes 的 route_kind 筛选 ──────────────────────────────
--
-- 查询：internal/module/publication/model/publication_model.go 的
--       ListActivePaths 与 ListActiveRoutes
--       WHERE project_id = ? AND route_kind = ? ORDER BY path ASC
--
-- 主键 (project_id, path) 只能吃 project_id 前导列，route_kind 要回表过滤，之后还要排序。
--
-- 第三列 path 是审计原文没写的：两个查询都 ORDER BY path，把它编进索引后既免排序，
-- 又让走 Pluck("path") 的 ListActivePaths 变成 Index Only Scan（不必回表）。
-- 路由表是页面数量级，多一列 text 的体积可忽略。

CREATE INDEX IF NOT EXISTS idx_page_routes_project_kind_path
    ON page_routes (project_id, route_kind, path);

COMMENT ON INDEX idx_page_routes_project_kind_path IS '激活路由列举：按 kind 过滤 + path 排序（审计 IDX-020）';

-- ── 3. IDX-017：核对后无 DDL ────────────────────────────────────────────────
--
-- 审计原文称 order_status_logs / product_ratings / mail_automation_node_logs「缺索引」，
-- 与现状不符 —— 三张表在 135 / 120 / 131 三个迁移里各自已有索引，且三条主查询都走索引：
--   · ListByOrderID    WHERE order_id = ?             → idx_order_status_logs_order (order_id, id)
--   · 评分明细与聚合   WHERE product_id = ?           → idx_product_ratings_product_id (product_id)
--   · ListNodeLogs     WHERE run_id = ? ORDER BY id   → idx_mail_automation_node_logs_run (run_id, id)
-- 实测 EXPLAIN 确认 NodeLogExists 走 Index Scan using idx_mail_automation_node_logs_run。
--
-- 生命周期一侧也已落地（审计 IDX-019）：internal/retention/catalog.go 对三张表各有声明 ——
-- order_status_logs 不清理并写明「随订单存续，将来归档时应随归档搬运」、
-- mail_automation_node_logs 保留 180 天由 mail 每日任务清理、
-- product_ratings 不清理并写明「是业务数据，按时间清理等于悄悄改变前台呈现」。
--
-- 两处「看起来像缺口」的地方核实后同样是无需改动：
--   · NodeLogExists 的 (run_id, node_key, status) —— 单个 run 的日志行数等于流程节点数
--     （个位到几十行），走 (run_id, id) 再过滤已经是最优路径，再加一列只多一份写成本。
--   · DeleteNodeLogsBefore 的 WHERE create_time < ? ORDER BY id LIMIT ? —— SQL 按 id 排序，
--     主键顺序扫描在取满 LIMIT 后立刻停止，代价是「已过期的行都挤在 id 头部」的常态下的最优解。
--     换成 create_time 索引反而要把全部过期行取出来再排序，是负优化。要真正改好必须先改 SQL
--     （把 ORDER BY 换成 create_time），那超出本批「只动 DDL」的范围，故不动。

-- ── 4. IDX-018：核对后无 DDL ────────────────────────────────────────────────
--
-- 四个候选逐一核对真实查询面，没有一条同时满足「idx_scan 为 0」与「功能与其它索引重复」：
--
--   · idx_orders_create_time (create_time DESC)
--     不能用 (project_id, status, create_time) 替代。order_model.go 的超时订单扫描是
--     全局谓词 WHERE status = ? AND create_time < ? ORDER BY create_time ASC，没有 project_id
--     条件 —— 复合索引前导列无值即不可用，这条索引是该查询唯一可用的索引，删掉就是全表扫加排序。
--
--   · idx_inventory_movements_reason (reason_code)
--     它是分区索引（pg_class.relkind = 'I'，真实落在各分区上的子索引是 *_reason_code_idx），
--     且 inventory_change_model.go 有 mv.reason_code = ? 的过滤谓词。库里没有任何其它索引
--     含 reason_code 列，删掉即失去该维度，只在「低基数单列」这一条上成立「价值低」。
--
--   · idx_product_ratings_project_id (project_id)
--     全库非测试代码里 product_ratings 的谓词只有 product_id（ListRatings 与两处聚合子查询）
--     和 id（GetRating / DeleteRating），project_id 只在写入时赋值、从不进 WHERE。
--     这条最接近「可删」，但仍不满足判据的第二个合取项 —— 它提供的 project_id 维度检索能力
--     没有替代品（product_id 索引不同列），只是当前无人使用，属运维观察项而非冗余。
--
--   · idx_products_primary_category (primary_category_id)
--     product_category_model.go 的 category_ids @> ?::jsonb OR primary_category_id = ?
--     靠 BitmapOr 组合 GIN 与该 btree。虽然「主分类必然出现在 category_ids 里」的不变量
--     让第二个分支恒为真值冗余，但删掉它会让不变量一旦被破坏时的查询退化为顺扫，收益不确定。
--
-- 另外这四个候选的 idx_scan 全为 0，本身不构成删除依据：开发库这几张表都是空表
-- （pg_stat_user_tables.n_live_tup = 0），空库上连唯一约束索引同样是 0 次扫描，
-- 统计计数在这里没有区分度。审计原文要求的是「生产库至少一个完整业务周期的增量快照」，
-- 本批不具备该条件 —— 所以本批不执行任何 DROP。

-- 本批核对结论汇总（供后续复核）：
--   需 DDL：IDX-008（部分索引）、IDX-020（复合索引）
--   无需 DDL：IDX-017（索引与保留期均已在位）、IDX-018（四条候选均证伪，待生产库快照）
