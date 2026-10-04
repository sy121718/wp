# 开发库测试数据清理与回滚记录（2026-09-19 浏览器实测遗留数据）

> 内部过程文档（非对外使用）

> 执行时间：2026-09-19 21:56（+0800）
> 执行对象：本机开发库 PostgreSQL `host=127.0.0.1 port=5432 dbname=wp`（**仅此一库**）
> 依据文档：`docs/agents/interaction-verification-2026-09-19.md` §1（测试数据行）、§7（副作用清单）
> 目的：把上一轮真实浏览器交互实测写入开发库的 3 行测试数据回滚，并留下可复核的证据链。

## 0. 结论速览

| # | 对象 | 类型 | 处置 | 影响行数 |
|---|---|---|---|---|
| 1 | `navigations` `6e053460-d9a3-4627-ad5e-74c6cf387123` | **改动既有行** | 字段还原（不删行）：`panel_width` full → `auto`、`panel_block_id` 15dab1bd → `NULL` | UPDATE 1 |
| 2 | `content_templates` `adf26704-d61b-45c5-92a8-7467d0ef792b` | 新建行 | 删除（先删其版本子行，再删父行） | DELETE 1（+ 子行 DELETE 1） |
| 3 | `blocks` `03d6ca0f-57e4-4f8b-a78b-8f6bb049d02d` | 新建行 | 删除 | DELETE 1 |

回滚后全库已无这三行的引用残留（唯一命中是 `navigations.id` 自身，属预期）。

## 1. 依据与连接（先读配置，不照抄记忆）

`config.yaml` 的 `database` 节（原文，第 33–45 行）：

```yaml
database:
  driver: postgres  # mysql, postgres, sqlite, sqlserver
  host: 127.0.0.1
  port: 5432
  user: root
  password: "root"
  dbname: wp
  max_idle_conns: 10
  max_open_conns: 100
  log_level: ""  # 留空时按 server.mode 推导
  prepare_stmt: false
  skip_default_transaction: false
  slow_threshold: 200ms
```

→ 实际连接串：`PGPASSWORD=root psql -h 127.0.0.1 -p 5432 -U root -d wp`（与记忆一致，未走别的库）。

实测报告原文（`docs/agents/interaction-verification-2026-09-19.md`）：

- §1 第 29 行（测试数据行）：
  > `navigations 6e053460`：`panel_width` auto→**full**、`panel_block_id` → `15dab1bd`(页眉1)；`blocks` 新建 `03d6ca0f`(页眉菜单·home)；`content_templates` 新建 `adf26704`(页眉测试模板, entity_type=header，经 `POST /api/contenttemplate/create` 造数)
- §7 第 1 条：
  > 库里新增/修改了测试数据（§1 已列）：`navigations.panel_width/panel_block_id`、新建 block `03d6ca0f`、新建 header content_template `adf26704`（未“设为生效”）。需要回滚的话按这三条删除/还原即可。

**报告直接记录的原值只有 `panel_width = auto`**；`panel_block_id` 的原值报告没有直接写（§6 给出还原为 `NULL` 的证据链）。

## 2. 清理前：3 行完整快照

psql `-x` 输出逐字转写（`----+----` 分隔线的长度由 psql 按内容宽度生成，本文件按可读性整理；列名与值逐字保留）。

### 2.1 `navigations`（改动既有行）

```text
-[ RECORD 1 ]---+-------------------------------------
id             | 6e053460-d9a3-4627-ad5e-74c6cf387123
project_id     | 52935790-31b4-4bcd-8eb6-ccdc46b342c7
title          | home
path           | /home
kind           | header
parent_id      | (空/NULL)
sort_order     | 1
create_time    | 2026-09-19 00:25:13.665028+08
update_time    | 2026-09-19 20:19:12.112859+08
source_type    | custom
source_id      | (空/NULL)
target         | self
panel_block_id | 15dab1bd-dd3e-498a-8356-e511ee092371
panel_width    | full
```

### 2.2 `blocks`（新建行）

```text
-RECORD 1 --------------------------------------------------------------------------
project_id  | 52935790-31b4-4bcd-8eb6-ccdc46b342c7
name        | 页眉菜单·home
kind        | block
document    | {"root": [], "settings": {"seo": {}, "base": {}, "layout": {"mode": "full", "safePadding": {}}, "structure": {}}}
create_time | 2026-09-19 20:32:26.308894+08
update_time | 2026-09-19 20:32:26.308894+08
category    | general
reuse_mode  | global
id          | 03d6ca0f-57e4-4f8b-a78b-8f6bb049d02d
```

`document` 字段的 jsonb 文本长度 = **113 字符**（清理前实测 `length(document::text)`）。

### 2.3 `content_templates`（新建行）

```text
-[ RECORD 1 ]------+-----------------------------------------
id                 | adf26704-d61b-45c5-92a8-7467d0ef792b
project_id         | 52935790-31b4-4bcd-8eb6-ccdc46b342c7
name               | 页眉测试模板
entity_type        | header
draft_document     | <见下 JSON>
draft_version      | 1
current_version_id | bbeab489-ac0a-4108-b17c-4a28c51ffe32
create_time        | 2026-09-19 20:27:14.17514+08
update_time        | 2026-09-19 20:27:14.17514+08
is_default         | f
template_role      | detail
```

`draft_document` 原文（jsonb 文本长度 = **1202 字符**，清理前实测）：

```json
{"root": [{"id": "hdr-section", "type": "core.container", "props": {"tag": "header", "layout": {"flex": {"direction": "row"}, "engine": "flex"}}, "children": [{"id": "hdr-heading", "type": "core.heading", "props": {"text": "页眉结构模板", "level": 2}, "children": null}]}], "settings": {"seo": {}, "base": {}, "theme": {"button": {"color": "#ffffff", "radius": "8px", "paddingX": "20px", "paddingY": "10px", "background": "#3d444f", "fontWeight": "600", "hoverColor": "#ffffff", "hoverBackground": "#2f353d"}, "colors": {"text": "#1a1d21", "accent": "#6b7280", "border": "#e5e7eb", "danger": "#b34a4a", "heading": "#1a1d21", "primary": "#3d444f", "success": "#3f6b4f", "surface": "#f4f5f6", "warning": "#8a6d3b", "secondary": "#5b6572", "background": "#ffffff"}, "images": {}, "motion": {}, "surface": {"radius": "10px", "shadow": "sm", "borderColor": "#e5e7eb", "borderWidth": "1px"}, "typography": {"body": {"color": "#1a1d21", "fontSize": "16px", "lineHeight": "1.7"}, "link": {"color": "#3d444f", "underline": "hover", "hoverColor": "#2f353d"}, "heading": {"color": "#1a1d21", "spacing": "12px", "fontSize": "32px", "fontWeight": "600"}}}, "layout": {"mode": "full", "safePadding": {}}, "structure": {}}}
```

它连带的版本子行（`content_template_versions`，随模板一并清理）：

```text
id          | bbeab489-ac0a-4108-b17c-4a28c51ffe32
template_id | adf26704-d61b-45c5-92a8-7467d0ef792b
version     | 1
create_time | 2026-09-19 20:27:14.17514+08
```

## 3. 引用关系排查（先摸清再删）

### 3.1 全库逐列扫描

方法：对 `information_schema.columns` 里 `public` schema 的**每一列**（含分区子表 `*_2026_*` / `*_default`）执行 `col::text LIKE '%<id>%'`，任一 id 命中即输出。

```sql
SET client_min_messages=notice;
DO $$
DECLARE r record; n bigint;
  ids text[] := ARRAY['6e053460-...','03d6ca0f-...','adf26704-...'];
  pats text[];
BEGIN
  pats := ARRAY['%'||ids[1]||'%','%'||ids[2]||'%','%'||ids[3]||'%'];
  FOR r IN SELECT table_name, column_name, data_type FROM information_schema.columns
            WHERE table_schema='public' ORDER BY table_name, ordinal_position LOOP
    EXECUTE format('SELECT count(*) FROM %I WHERE %I::text LIKE ANY($1)', r.table_name, r.column_name) INTO n USING pats;
    IF n > 0 THEN RAISE NOTICE 'HIT % . % (%) => %', r.table_name, r.column_name, r.data_type, n; END IF;
  END LOOP;
END $$;
```

清理前命中（共 4 处，其中 3 处是三行自身，1 处是模板的版本子行）：

```text
HIT blocks . id (uuid) => 1
HIT content_template_versions . template_id (uuid) => 1
HIT content_templates . id (uuid) => 1
HIT navigations . id (uuid) => 1
```

### 3.2 任务点名的引用点逐表显式核对

| 表.列 | 全表行数 | 命中 | 结论 |
|---|---|---|---|
| `pages.draft_document` | 1 | 0 | 未引用 |
| `page_revisions.draft_document` | 1 | 0 | 未引用 |
| `page_dependencies.dependency_key` | 0 | 0 | 表为空 |
| `document_snapshots.document` | 0 | 0 | 表为空 |
| `page_artifacts.source_document` | 0 | 0 | 表为空 |
| `presentation_instances.template_id` | 0 | 0 | 表为空 |
| `presentation_dependencies` / `presentation_artifacts` | 0 | 0 | 表为空 |
| `page_component_pins` | 0 | 0 | 表为空 |
| `content_template_component_pins.template_id` | 0 | 0 | 表为空 |
| `page_stagings` | 0 | 0 | 表为空 |
| `blueprints` | 6 | 0 | 未引用 |
| `navigations.panel_block_id = 03d6ca0f…` | 2 | 0 | 未引用 |
| `navigations.parent_id = 6e053460…` | 2 | 0 | 无子行 |
| `content_template_versions.template_id = adf26704…` | 3 | **1** | 自身版本子行，删父前必须先删 |

相关外键（`pg_constraint`，`contype='f'`）：

```text
content_template_versions.template_id          -> content_templates(id)          NO ACTION
content_template_component_pins.template_id    -> content_templates(id)          NO ACTION
presentation_instances.template_id             -> content_templates(id)          NO ACTION
document_snapshots.source_template_version_id  -> content_template_versions(id)  NO ACTION
navigations.panel_block_id                     -> blocks(id)                     ON DELETE SET NULL
navigations.parent_id                          -> navigations(id)                ON DELETE CASCADE
```

### 3.3 结论

- `blocks 03d6ca0f`：**零引用**，删除不影响任何行（`navigations.panel_block_id` 指向的是既有块 `15dab1bd`，不是它）。
- `content_templates adf26704`：唯一引用是它自己的版本子行；无 page / instance / snapshot / dependency 引用 → 先删子行再删父行。
- `navigations 6e053460`：**既有行、无子行**，按报告记录还原字段，**不删行**。

## 4. 执行的 SQL（原样留档）

单事务执行（`psql -v ON_ERROR_STOP=1`，任一步失败整体回滚）：

```sql
BEGIN;

-- 操作前确认（命中行数）
SELECT 'BEFORE' AS phase, 'navigations' AS tbl, id::text, panel_width, panel_block_id::text
  FROM navigations WHERE id = '6e053460-d9a3-4627-ad5e-74c6cf387123';
SELECT 'BEFORE' AS phase, 'blocks' AS tbl, id::text, name
  FROM blocks WHERE id = '03d6ca0f-57e4-4f8b-a78b-8f6bb049d02d';
SELECT 'BEFORE' AS phase, 'content_templates' AS tbl, id::text, name
  FROM content_templates WHERE id = 'adf26704-d61b-45c5-92a8-7467d0ef792b';
SELECT 'BEFORE' AS phase, 'content_template_versions' AS tbl, id::text, version
  FROM content_template_versions WHERE template_id = 'adf26704-d61b-45c5-92a8-7467d0ef792b';

-- 1) navigations：改动既有行 -> 还原字段（不删行）
UPDATE navigations
   SET panel_width = 'auto', panel_block_id = NULL
 WHERE id = '6e053460-d9a3-4627-ad5e-74c6cf387123';

-- 2) content_template：删除子版本行，再删除模板行
DELETE FROM content_template_versions
 WHERE template_id = 'adf26704-d61b-45c5-92a8-7467d0ef792b';
DELETE FROM content_templates
 WHERE id = 'adf26704-d61b-45c5-92a8-7467d0ef792b';

-- 3) blocks：删除新建块
DELETE FROM blocks
 WHERE id = '03d6ca0f-57e4-4f8b-a78b-8f6bb049d02d';

-- 操作后确认
SELECT 'AFTER' AS phase, 'navigations' AS tbl, id::text, panel_width, coalesce(panel_block_id::text,'<NULL>') AS panel_block_id
  FROM navigations WHERE id = '6e053460-d9a3-4627-ad5e-74c6cf387123';
SELECT 'AFTER' AS phase, 'blocks_remain' AS k, count(*)::text AS v FROM blocks WHERE id = '03d6ca0f-57e4-4f8b-a78b-8f6bb049d02d';
SELECT 'AFTER' AS phase, 'ct_remain' AS k, count(*)::text AS v FROM content_templates WHERE id = 'adf26704-d61b-45c5-92a8-7467d0ef792b';
SELECT 'AFTER' AS phase, 'ct_versions_remain' AS k, count(*)::text AS v FROM content_template_versions WHERE template_id = 'adf26704-d61b-45c5-92a8-7467d0ef792b';

COMMIT;
```

语句级影响行数（psql 原样输出）：`UPDATE 1` → `DELETE 1` → `DELETE 1` → `DELETE 1`。

> 未使用任何宽泛条件：全部是 `WHERE id = '<完整 uuid>'`（子表用 `WHERE template_id = '<完整 uuid>'`）；
> 未使用 `DROP` / `TRUNCATE` / 无 WHERE 的 `DELETE`；未触碰 `wp` 以外的任何库。

## 5. 清理后的确认结果

事务提交后**重新连接**执行的后置查询（原样输出）：

```text
--- 后置确认：三个 id 还剩多少行 ---
navigations       | 1   ← 行本身保留（按设计，只还原字段）
blocks            | 0
content_templates | 0

--- 各表现存行 ---
id                                   | title | kind   | panel_width | panel_block_id | create_time                    | update_time
6e053460-d9a3-4627-ad5e-74c6cf387123 | home  | header | auto        | <NULL>         | 2026-09-19 00:25:13.665028+08   | 2026-09-19 20:19:12.112859+08
94597613-7d26-4a4c-b57d-343a8f4e02d9 | /test | header | auto        | <NULL>         | 2026-09-19 00:25:24.233902+08   | 2026-09-19 00:25:24.233902+08

blocks 现存：15dab1bd-dd3e-498a-8356-e511ee092371 | 页眉1 | header   （只剩既有的 1 块）

content_templates 现存：
2c10758a-549c-4f8f-b810-517501e39ee6 | 商品详情页 | product | is_default=t
c8a97d15-7af7-444e-9c10-7bcdc3d04e31 | 文章详情页 | article | is_default=t

content_template_versions 现存：
8f9283af-e1bf-4078-b375-bb19bdd1862f | 2c10758a-… | v1
88563554-978c-4de9-9d65-530407d949a8 | c8a97d15-… | v1
```

再次跑全库逐列扫描（同 3.1 的方法），残留命中只剩 1 列：

```text
RESIDUAL navigations . id => 1        ← 该行本身（预期保留）
RESIDUAL_TOTAL_COLUMNS_WITH_HIT=1
```

即：`blocks` 与 `content_templates` 的两个 id 在全库**零残留**，`navigations` 只剩行本身（字段已还原）。

## 6. 原值为 `NULL` 的认定依据（`panel_block_id` 报告未直接记录）

报告 §1 只写下 `panel_width` 的旧值 `auto`，没有写 `panel_block_id` 的旧值。本次还原为 `NULL`，依据是四条互相印证的证据：

1. **这两列是实测当天才引入的**：`public/migrations/285_navigation_mobile_panel.sql` 是全库唯一新增这两列的地方（`grep -rn "panel_block_id|panel_width" public/migrations/` 只命中 285 与登记它的 `register_core.go`），该文件提交时间 `24a90105 2026-09-19 19:03:04 +0800`；285 里是 `panel_block_id uuid NULL`（无默认值）与 `panel_width text NOT NULL DEFAULT 'auto'`。
2. **该行创建于 00:25:13**（`create_time = 2026-09-19 00:25:13.665028+08`），早于 285 引入这两列 → 加列后该行只能取列默认值：`panel_block_id = NULL`、`panel_width = 'auto'`。
3. **报告实测时库里只有两个块**：`15dab1bd`（页眉1，00:18:49 创建）与 `03d6ca0f`（本次新建，20:32:26 创建）。报告明说本次把 `panel_block_id` **改成** `15dab1bd`，即改动前 ≠ `15dab1bd`；而 `03d6ca0f` 直到 20:32 才存在，晚于面板保存（20:19:12）→ 改动前只能是 `NULL`。
4. **同批创建的兄弟行** `94597613`（/test，00:25:24 创建、从未被实测触碰）至今仍是 `panel_block_id = NULL` + `panel_width = 'auto'`，与推断形态一致。

结论：`panel_width` 的还原值有报告直接记录；`panel_block_id` 的还原值由上述时间线与库内事实唯一确定。

## 7. 这份记录什么时候失效 / 这些行若再出现说明什么

**判断本记录是否仍然适用：**

- 库变了：`config.yaml` 的 `database.dbname` 不再是 `wp`（或主机/端口变了）→ 本记录指向的已不是当前开发库，需重新核对。
- 列变了：285 之后的新迁移若改名 / 改默认值 / 删除 `navigations.panel_width` 或 `panel_block_id` → §6 的证据链前提失效。
- 行以**相同完整 id** 再次出现（`6e053460-d9a3-4627-ad5e-74c6cf387123` / `03d6ca0f-57e4-4f8b-a78b-8f6bb049d02d` / `adf26704-d61b-45c5-92a8-7467d0ef792b`）→ 只可能是从备份/快照恢复或数据回放，**不是回滚没生效**（新建行的 uuid v4 不可能重复）。
- 若只有 `navigations` 那行的 `panel_width` 又变回 `full` / `panel_block_id` 非空，而 `blocks`、`content_templates` 里没有对应的测试夹具行 → 说明有人没重建夹具就改了该行，与本记录无关。

**三行各自“又出现”意味着什么：**

- `blocks` 里再次出现 `name='页眉菜单·home'`、`kind='block'`、`category='general'`、`reuse_mode='global'`、`document.root=[]`：有人在重跑实测报告的场景 4（块编辑），**id 会不同**（uuid v4 新造）。
- `content_templates` 里再次出现 `name='页眉测试模板'`、`entity_type='header'`、`is_default=f`：有人在重跑场景 5（结构模板）。
- 因此判据是「id 是否还是这三个完整 id」：**同 id = 备份恢复/回放；不同 id = 新一轮造数**。两种都不代表本回滚失败。

## 8. 已知取舍与不确定项

1. **未改 `update_time`**：`navigations 6e053460` 的 `update_time` 仍是实测写入时间 `2026-09-19 20:19:12.112859+08`（本次 UPDATE 未触碰该列）。刻意如此——报告没有记录该列的“原值”，改它属于发挥；代价是 **不能靠 `update_time` 判断本回滚是否执行过**，只能看 `panel_width='auto'` + `panel_block_id IS NULL`。
2. **`panel_block_id` 的原值在报告里没有直接记录**，本次按 §6 的证据链还原为 `NULL`。若存在更权威的记录（实测时的截图 / 日志）与此不符，以那份记录为准并修正本文件。
3. **快照的分隔线长度非逐字**：psql `-x` 的 `----+----` 线由 psql 按内容宽度生成，本文件按可读性整理；列名与值是逐字保留的。jsonb 文本长度（113 / 1202 字符）是清理前的机器读数，可用于校验转写。
4. **`content_template_versions` 的子行属连带清理**（模板的 v1 版本快照；外键 NO ACTION，不先删子行则删父行会失败），单列于此以免被误认为误删。
5. 本次只新建本文件，并只对 `wp` 库执行了 §4 的语句；未 `git add` / `git commit`，未改任何代码、脚本或其它文档。
