# IDX-018 与 IDX-017 的核对结论（子代理 f5376683，2026-09-14）

两条审计条目的前提经实测**不成立或部分不成立**，记录证据与后续依据。

## IDX-017「order_status_logs / product_ratings / mail_automation_node_logs 缺索引」

**前提不成立**：三张表都已有索引，且主查询都走索引。

```
tbl                        | idx                                    | def
order_status_logs          | idx_order_status_logs_order            | btree (order_id, id)      [135 迁移]
product_ratings            | idx_product_ratings_product_id         | btree (product_id)        [120 迁移]
mail_automation_node_logs  | idx_mail_automation_node_logs_run      | btree (run_id, id)        [131 迁移]
```

`EXPLAIN` 实测幂等查询走 `Index Scan using idx_mail_automation_node_logs_run`。
生命周期一侧也已落地：`internal/retention/catalog.go` 对三张表各有声明（节点日志 180 天清理，
另两张「不清理 + 理由」），即 **IDX-019 已覆盖本条的 remediation**。

结论：本条按「现状已满足」标记 resolved，**依据不是本批的 DDL 改动**，而是实测的现有索引与主查询计划。

另两处「疑似缺口」核实为无需改动，其中一条值得记下来：
`DeleteNodeLogsBefore` 的 `WHERE create_time < ? ORDER BY id LIMIT ?` —— 按 id 排序时主键顺序扫
取满 LIMIT 即停，换成 create_time 索引反而要把全部过期行取出再排序，**是负优化**。
要改好必须同时改 SQL 写法，不属于索引事项。

## IDX-018「删除疑似冗余索引」

**四个候选全部证伪，未执行任何 DROP。** idx_scan 实测：

```
idx_orders_create_time         idx_scan=0  → 不可删
idx_inventory_movements_reason idx_scan=0  → 不可删（分区索引 relkind='I'，且有 reason_code = ? 谓词）
idx_product_ratings_project_id idx_scan=0  → 判据第二项（与其它索引功能重复）不成立
idx_products_primary_category  idx_scan=0  → 不可删（BitmapOr 与 GIN 组合使用）
```

**`idx_orders_create_time` 最危险**：`order_model.go:273` 的超时订单扫描是全局谓词
`WHERE status = ? AND create_time < ? ORDER BY create_time ASC`，**没有 project_id**。
审计建议的替换索引 `(project_id, status, create_time)` 前导列在该查询里无值，即不可用 ——
照审计删就是全表扫加排序。这是「按 schema 猜测冗余」的典型翻车点。

### 方法论的错误（这条比单条 finding 更重要）

四个候选的 `idx_scan` 全为 0 **本身不构成删除依据**：开发库这几张表 `n_live_tup` 全为 0，
**空库上连唯一约束索引都是 0 次扫描**，统计计数没有区分度。
审计原文要求的是「生产库一个完整业务周期的增量快照」，本批不具备该条件。

**因此 IDX-018 不标记 resolved**：它要求的是「删除冗余索引」，而现有证据不支持删任何一个；
把「查过了、都不该删」当成「已完成」会掩盖一个事实 —— 这条 finding 真正需要的是一次生产库快照，
而不是一次代码改动。

## 后续

- 拿到生产库快照（或任意有真实数据的库）后重跑 `pg_stat_user_indexes` 对比，再决定 IDX-018 是否成立。
- 在那之前，本条保持 open，理由是「缺证据」而不是「缺工作」。
