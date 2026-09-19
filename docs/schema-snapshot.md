# Schema 快照与枚举约束说明

> 最后核对：2026-09-13

## 1. 读 schema 的正确方式

`public/migrations/init_builder_schema.sql` 是**历史初始快照**，不是当前 schema 的权威描述。后续编号迁移（`020`、`071`、`080` 等）会超越 init 里的 CHECK、列与约束；新库必须 **先跑 init、再按序跑全部编号迁移** 才正确。

**不要**单独阅读 init 推断线上结构。当前 schema 应以下列方式之一为准：

| 方式 | 适用场景 |
|---|---|
| 本地 PostgreSQL 跑完迁移后 `pg_dump --schema-only` | 生成可 diff 的结构快照（建议输出到 `docs/schema.sql`，CI 可选对比） |
| dbx MCP `dbx_describe_table` / `dbx_get_schema_context` | 开发/审计查表（连接名与库名以本机 DBX 配置为准；运行时库名见 `config.yaml` 的 `database.dbname`） |
| 编号迁移 SQL + 本文件 §2 | 理解「为何某处没有 DDL CHECK」 |

在 `init_builder_schema.sql` 文件头已标注：此为历史快照，当前结构见本文。

## 2. 枚举约束：DDL 兜底 vs Go 注册表

新增枚举列时，优先 **Go 白名单 + DDL CHECK 双份**（见 `AGENTS.md` 数据库章节）。下列为**有意只留 Go 侧**或**历史取舍**，不是遗漏：

### 2.1 仅 Go 注册表（可扩展类型，DDL 无法穷举）

| 列 / 场景 | 迁移 | 原因 |
|---|---|---|
| `content_templates.entity_type` | `080` DROP CHECK | 实体类型由模块注册（`product`、`product_category` 等），扩展时不改 DDL |
| `product_tags` 自动规则 `rule_type` | `091` 只 CHECK JSON 形状 | 规则类型可扩展（`new_arrival` / `price_range` / `on_sale`），新规则走 Go 注册表 |
| `product_price_adjustments` 等定价规则类型 | 同类 | 四种内置规则 + 白名单参数，Go 侧严格校验 |

**风险**：直接写库或修复脚本可写入未注册值 → 构建/重算时静默失效或报错。**正常 API 路径**有 Go 校验。

**运维建议**：启动或巡检时对比 `distinct entity_type` 与注册表（未来可加 warn 列表）；未知规则类型在重算入口拒绝。

### 2.2 有 DDL CHECK 兜底（固定闭集）

槽位键、导航位置、优惠券口径、库存流水方向、货源 `type`、订单归因字段形状等 —— 见各迁移文件头部 COMMENT 与 CHECK。

### 2.3 仅 Go 侧、尚无 DDL CHECK（待补，见 DB-015）

`orders.status`、`inventory_warehouses.status`、`blocks.kind` 等 —— 审计项 DB-015 跟踪补 CHECK，与 DB-022/023 的「可扩展」类不同。

### 2.4 双份真源（常量 + DDL，需一致性测试）

系统页面槽位十个键：`page/enums`、迁移 `138` CHECK、`builder/core` 各一份（builder 不 import page 模块）。新增槽位须改多处；DB-011 类测试应对齐。

### 2.5 商品页双轨两列（迁移 281 / 282）

`presentation_instances` 上有两个容易漏看的列，语义由迁移头部注释承担：

| 列 | 迁移 | 语义 |
|---|---|---|
| `override_document` | 281 | jsonb，**NULL = 跟随模板**；非空 = 该实例自己的文档。发布/重建按它取底稿，binding 仍照常解析实体数据（补数据不丢自定义） |
| `render_mode` | 282 | text，DDL CHECK 闭集 `template` / `document`，默认 `template`。**它是模式的唯一判定依据** —— 不要用 `override_document` 空/非空推断（「改了又改回去」「重新套用预设」两种状态会漂移）；模板换代的 stale 传播只标 `template` 模式的实例 |

两列的写路径都收在同一次发布的事务里（模式/文档 + 快照 + 产物行 + 指针），换模板 = 放弃自定义（同事务清 `render_mode` 与文档）。

### 2.6 导航位置与悬浮面板（迁移 285）

`navigations.kind` 的 DDL CHECK 由 `(header, footer)` 放宽为 **`(header, header_mobile, footer, footer_mobile)`**：桌面与移动端是**两份独立数据**（WP 式两个位置各绑一条菜单），不是一套数据两种呈现。走"位置扩展"而非给记录加 `device` 维度的理由：`kind` 本就是位置维度，解析（`Tree` / `ResolveMenu`）、依赖键（`menu:{projectID}:{kind}`）、适配器缓存都按它天然区分，扩展只需放宽一处 CHECK + Go 校验；加 `device` 列要动契约签名与全部调用点，还要处理「同一位置两条 device 冲突」。存量 `header`/`footer` 记录与绑定零回归（语义等同桌面端）。

同批新增两列：

| 列 | 类型 | 语义 |
|---|---|---|
| `panel_block_id` | uuid NULL → `blocks(id) ON DELETE SET NULL` | 菜单项悬浮面板引用的全局块（超级菜单）：构建期渲染进产物，并登记 `block:{id}` 依赖 |
| `panel_width` | text NOT NULL DEFAULT 'auto'，CHECK `auto|full` | 面板展示宽度 —— 内容在块里、展示属性在菜单项上，避免同一块被多个菜单项复用时互相打架 |

**为什么不复用 `source_type='block'`**：那是"点这一项跳到哪"的**链接来源**语义，面板是"悬停展开显示什么" —— 一个字段两种含义会在渲染与依赖登记上互相干扰。

### 2.7 依赖 kind 放宽：按菜单项引用（迁移 289）

`page_dependencies` 与 `presentation_dependencies` 两张表的 `dependency_kind` CHECK 放行 **`navigation`**。语义：`core.nav` 支持两种引用方式，各登记一条依赖 ——

| 引用方式 | 依赖键 |
|---|---|
| 按位置（`Props.Menu`） | `menu:{projectID}:{kind}`（`menu` 已由迁移 071 放行） |
| 按具体菜单项（`Props.Navigation`） | `navigation:{itemID}`（本次新增） |

不放行的表现是**构建期直接失败**（依赖行插入被 CHECK 拒绝），不是静默失效 —— 但也意味着「按项引用」这个能力完全不可用，故必须与 Go 侧同批落地；约束按名先删后建（`IF EXISTS` + 新定义），重复执行安全。

### 2.8 本会话的 seed 批次（无结构变更）

| 迁移 | 内容 | 进本文档？ |
|---|---|---|
| 286 | 商品页双轨 4 条业务错误文案词条（`presentation.err.*`） | 已随 §2.5 记语义，不另列 |
| 287 | 系统预置「展示页」Blueprint ×6（首页 / 商店 / 关于我们 / 联系我们 / 政策条款 / FAQ） | 否（纯数据） |
| 288 | `contenttemplate:activate` 权限点（多套存着、单套生效的切换入口） | 否（权限数据） |
| 290（待落地） | 导航乐观锁的冲突文案词条 | 否（纯词条） |

判断口径：**本文档只记结构与枚举约束**（新增列 / DDL CHECK / 约束放宽）；纯词条与权限 seed 由迁移头部注释 + `public/test/pkg/i18n/i18n_seed_functional_test.go` 的行数基线记账。

## 3. 历史表与注释漂移

部分早期迁移注释描述的能力已被后续迁移删除（例如迁移 `121` 去掉商品侧 `stock_total` 与 `inventory_stock_cache_syncs`）。**已执行的迁移 SQL 语句不改**；注释会在文档/迁移头中标注「已被 NNN 取代」，避免按注释维护已删除的缓存体系。
