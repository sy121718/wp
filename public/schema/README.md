# public/schema

数据库结构的唯一来源。一表一文件，改结构直接改表文件，不再追加逐版本迁移。

## 结构

- `base.sql`：扩展（`ext_shared` / `pg_trgm`）、schema、4 个触发器函数、`sys_baseline` 水位表。
- `defaults.sql`：启动期补齐脚本，给每个工程补 product / article 默认内容模板（等价旧 seed 085/159）。不是表数据段，不参与 order.txt。
- `tables/<表>.sql`：一张表的全部 DDL 与系统数据，按 `-- @phase` 分段：
  `pre`（序列）→ `ddl`（建表）→ `constr`（约束/触发器/策略）→ `idx`（索引）→
  `fk`（外键）→ `comment`（注释）→ `data`（系统数据）。
- `order.txt`：系统数据装载顺序；只列有 data 段的表，父表在前（`sys_menus` 先于 `sys_menu_permission`）。

`schema.Apply` 按固定段序执行：全部表的 pre → ddl → constr → idx → fk → comment，再按
`order.txt` 装数据，最后写 `sys_baseline` 水位。系统数据装载期 `SET LOCAL session_replication_role = replica`，
避免插入触发业务写入；该设置需要管理连接权限。

## 水位

- 空库：一次事务建完并写 `sys_baseline(version, manifest_sha256, note)`。
- 同摘要：不重放结构与系统数据，但仍执行 `defaults.sql`（给新工程补默认模板）。
- 摘要不符或已有表但没有水位：**拒绝执行**，提示走 `scripts/schema-adopt.sh`。历史结构不可可靠回退，不能在存量库上重放。
- 现有库接入：`bash scripts/schema-adopt.sh --db <库> [--refresh] --apply`，默认只核验（混库、结构、必需业务键）。

## 系统数据范围

只放启动与界面必需的系统数据：`sys_i18n` / `sys_permission` / `sys_menus` / `sys_menu_permission` /
`sys_casbin_rule` / `sys_dict` / `sys_area` / `sys_config` / `sys_admin` / `sys_i18n_revision`、
字典 `inventory_change_reasons`、系统邮件模板 `mail_templates`、showcase `blueprints` / `blueprint_versions`。
业务数据（订单、商品、会员、邮件联系人、访问流水等）不在这里；对应表只保留 DDL。

## 重新生成

生成器只用于首次切分与漂移核对，日常改结构直接改表文件。

```bash
# 1. 在临时空库载入当前 public/schema
createdb wp_schema_tmp
GOWP_DATABASE_DBNAME=wp_schema_tmp go run ./cmd -migrate-only

# 2. 从临时库切分（必须显式 -db；不要指向业务库）
go run ./cmd/schema-split -db wp_schema_tmp

# 3. 校验与接入
go test ./public/schema/
go run ./cmd/schema-digest
bash scripts/schema-adopt.sh --db wp --refresh --apply
dropdb wp_schema_tmp
```

生成物含时间戳与序列当前值，重生成后摘要会变；确认差异后刷新业务库水位。测试不跑迁移链：
`support.NewMigratedPGTestDB` 克隆的模板库由 `schema.Apply` 建。
