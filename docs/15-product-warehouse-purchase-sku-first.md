# 商品 · 仓库 · 采购 —— 从一个 SKU 出发（现状交接文档）

> 用途：把**当前实际运行**的商品 / 仓库 / 采购逻辑完整交接出去，供外部团队做优化。
> 本文只写现状（含已核实的缺口），不写目标态；每条结论都标注了权威来源（迁移编号 / 文件路径），可逐条复核。
> 基线：迁移编号截至 `264`；商品代码在 `internal/module/product/**`，库存与采购代码在 `internal/module/inventory/**`。
> 阅读顺序建议：§1 → §2 → §3（查表用）→ §4（流程）→ §5（不变量）→ §7（缺口）。
>
> **权威优先级**（本文与代码冲突时以此为准）：线上数据库实际结构 → `public/migrations/*.sql` → `internal/module/{product,inventory}/**/model/*.go` → 服务层注释 → 文档。
> 注意：`docs/13-module-inventory.md`、`docs/14-product-sku-and-cost-model.md` 是设计意图与迁移记录，**个别条目描述的是计划而非现状**（本文 §7 标出了其中已确认的落差）。

---

## 1. 十分钟速览

### 1.1 三个域、四类实体

```text
商品域（product）                    库存域（inventory）                      采购域（同在 inventory 模块）
────────────────────────────────      ───────────────────────────────────     ─────────────────────────────
products            商品容器           inventory_warehouses      仓库          inventory_sources          货源
product_variants    可售规格(SKU)      inventory_stocks          库存真源      inventory_purchase_orders  采购单头
product_attributes  属性组/值          inventory_stock_movements 库存流水      inventory_purchase_order_lines 采购行
product_categories  分类               inventory_change_reasons  原因字典      inventory_purchase_receipts     入库单头
product_brands      品牌               inventory_bom_items       物料清单      inventory_purchase_receipt_items 入库单行
product_tags        标签(手工/自动)
product_pricing_*   定价规则与留痕
```

**关键结构事实**：商品域与库存域是两个独立 Go 业务模块（`internal/module/product/` 与 `internal/module/inventory/`）。商品侧只依赖库存模块的 `contract` 与不可变 DTO，不导入库存 `service/model`；库存域对外边界在 `internal/module/inventory/contract/`。HTTP 路由、权限码、菜单、模板与数据库表名未随目录迁移改变。

### 1.2 一个 SKU 的一生（端到端）

```text
① 建仓                      inventory_warehouses             工程内第一个仓自动成为默认仓
      │
② 建商品 + 认领 SKU          products.sku_code                "SZ_DRAWERSMOKE_001"（认领仓前缀）
      │                      product_variants.sku_code        "SZ_DRAWERSMOKE_001"[首个无规格变体]
      │                      inventory_stocks.sku_code        "DRAWERSMOKE_001"（仓库侧裸码，逐仓一行）
      │                      ── 一个事务：商品 + 变体 + 各仓库存行 + 变更记录
      │
③ 生成规格变体（可选）       product_variants.sku_code        "SZ_DRAWERSMOKE_001_RED_V"
      │                      inventory_stocks                 （新变体逐仓建行，初始无限）
      │
④ 维护 BOM（可选）           inventory_bom_items              父 SKU → 子项 SKU × 用量
      │
⑤ 下单采购                  inventory_purchase_orders/lines   单号 + 货源 + 收货仓 + 行(SKU×数量×单价)
      │
⑥ 登记收货                  inventory_purchase_receipts/items  幂等键 + 已入库数量原子递增
      │                      inventory_stocks.quantity += n   同事务
      │                      inventory_stocks.cost_price = 本次到货价（该仓该 SKU 的当前成本）
      │                      inventory_stock_movements        in / purchase_in / source=purchase_order:单号
      │                      product_variants.cost_price       事务外 best-effort 兼容回写
      │
⑦ 销售出库                  inventory_stocks.quantity -= n   订单事务内（DeductStockTx）
      │                      inventory_stock_movements        out / sale_out / source=order:订单号
      │                      order_items.unit_price/cost_price  下单时刻快照（成本取自归属仓当前成本）
      │
⑧ 取消 / 退货入库            inventory_stocks.quantity += n   in / return_in（退货门闩 approved→received）
      │
⑨ 盘点 / 报损                inventory_stocks（adjust 到目标值） 只写流水，不改成本
      │
⑩ 全过程的字段级留痕          master_data_changes              商品 / 变体 / 货源的字段级 append-only 审计
```

每一步的**事务边界、锁、幂等**见 §4；每条不可违反的规则见 §5。

---

## 2. SKU 身份模型

这是整套系统最容易误解的部分：**同一个 SKU 在系统里有三个名字，各属不同层**。规则定于 2026-09-19（`docs/14-product-sku-and-cost-model.md` §1.1），代码落点：`product/service/product_variant.go`、`inventory/service/inventory_stock_sku.go`。

### 2.1 三层名字

| 层 | 列 | 形态示例 | 谁决定 | 含义 |
|---|---|---|---|---|
| 商品容器主体 | `products.sku_code` | `SZ_DRAWERSMOKE_001` | 系统拼（认领仓短码 + 编码本体） | 商品的**对外身份**，工程内唯一 |
| 可售规格 | `product_variants.sku_code` | `SZ_DRAWERSMOKE_001_RED_V` | 系统拼（主体 + 属性值段 + `_V`） | 一个可售单元；商品内唯一 |
| 仓库里这条货 | `inventory_stocks.sku_code` | `DRAWERSMOKE_001` | 运营输入的**裸码** | 这条货在**那个仓**的名字；仓内唯一 |
| 仓库（第三方） | `inventory_stocks.external_sku` | `HS-RD-SM` | 对方系统 | 这条货在**对方系统**里的编码；空串 = 用我们的码 |

- **仓码前缀只属于商品侧**。仓库里永远看不到 `SZ_` 开头的东西（早期实现把带前缀的整串写进库存行，导致「仓库那行」与「商品主体 SKU」同名相撞；迁移 `262` 已做存量清理）。
- **剥离前缀是幂等、大小写不敏感、只剥一次**：`SZ_SZ_X` → `SZ_X`。落点两处（必须同时改）：商品侧 `stripWarehousePrefix`（`product_variant.go`）与入库侧 `stripWarehousePrefix`/`normalizeStockSKU`（`inventory_stock_sku.go`）。
- 入库入口（`RegisterReceipt` / `RegisterProductionInbound` / 采购建行）收到的编码来自操作者或外部系统，**带前缀是常态**，因此按目标仓短码剥一次；**空串一律拒绝**（`ErrStockSKURequired`）。出库、调整、退货路径不改编码。

### 2.2 唯一性：三条约束，各管一层

| 约束 | 位置 | 说明 |
|---|---|---|
| `uq_products_project_sku_code` UNIQUE (project_id, sku_code) WHERE sku_code <> '' | 迁移 `246` | **主体容器唯一**，工程内。这是唯一的强身份约束 |
| `UNIQUE (product_id, sku_code)` | 迁移 `081` | 变体在**商品内**唯一，不额外收紧，「变体不管」 |
| `UNIQUE (product_id, option_values) WHERE option_values <> '{}'` | 迁移 `083` | 规格组合不重复生成（空组合允许重复：手工变体、首个无规格变体） |
| `uq_inventory_stocks_warehouse_sku` UNIQUE (warehouse_id, sku_code) | 迁移 `244` | **仓库内唯一**：同一仓不能有两条同码的货 |
| `UNIQUE (variant_id, warehouse_id)` | 迁移 `099` | 库存真源的行标识（维度 = SKU × 仓库） |

- **不要求全局唯一**：同一段编码文本允许出现在不同仓库、不同工程。任何按 SKU 字符串做的跨仓聚合都是错的（见 §5 纪律 1）。
- 存量商品主体编码为空串（偏索引排除空串），**存量编码一律不重写**，新规则只约束新建商品与新生成的变体。

### 2.3 编码规则细节

- **变体**：`<容器主体SKU>_<属性值段…>_V`
  - 属性段顺序取**商品引用属性组的先后**（`products.attribute_ids`），组内按属性值排序字段 —— 与运营点选顺序无关，因此同一组合恒得同一 SKU（`attributeGroupOrder` / `variantSKUCode`，`product_variant_generate.go`）。
  - 属性值段用**属性值 key**（不是属性组 key，也不是中文展示名）：非 ASCII 或含非法字符时取确定性短码，保证生成的 SKU 全 ASCII（`skuSegment`）。
- **捆绑（套餐）**：主体由运营自定义、**恒以 `_B` 结尾**（缺后缀服务端补齐，大小写不敏感）；**留空即拒绝** `ErrBundleSKURequired`（不再按 URL 段静默派生）。新建抽屉会给建议值，但编码必须被运营看见并确认。
- **变体商品主体**：留空即按 `slug` 的 ASCII 段派生；派生不出来（纯中文 URL 段）明确报 `ErrSkuContainerMissing`，**不退回随机码**。
- **多仓与认领**：新建商品可勾多个仓，勾中的仓各建一行（**该仓已有同裸码则复用不新建**，即「认领」）；**认领仓 = 显式指定的第一个仓**（未指定则默认仓），**只有它决定主体 SKU 的仓码前缀**。其余仓只建行、不参与命名（`resolveWarehouseTargets` / `claimWarehouse` / `createStockRowsTx`）。
- **捆绑商品不存在于仓库**：不生成首个变体、不建库存行、不分销自己的 SKU；「从仓库选」对 bundle 在服务端**硬拒**（`ErrWarehouseSKUBundleNotAllowed`）。

### 2.4 外部编码映射（N:1，弱校验）

- 场景：一个商品的多个变体（十几个口味）在仓库侧**共用同一个编码 / 同一条成本记录**（第三方仓与平台铺货的常见形态）。
- 因此 `inventory_stocks.external_sku` **没有唯一索引**（迁移 `251` 明确论证过），只有**写入时的弱校验**：同一仓内同一外码必须指向**同一个 `product_id`**（多口味共用合法；两个不同商品共用报 `ErrExternalSKUProductConflict`，错误里带冲突商品 id）。
- 空串是**合法状态**（该仓用我们自己的 SKU）。归一规则唯一入口 `NormalizeExternalSKU`：去首尾空白、≤128 字符、拒绝控制字符。
- 「仓库 SKU」**不是新实体**：它就是库存真源上已有的 `(warehouse_id, sku_code)`；不另建 SKU 目录（`docs/14` §4）。

### 2.5 六条身份纪律（优化时不要破坏）

1. **内部身份永远是 uuid**（`product_id` / `variant_id`），不是 SKU 字符串。跨仓汇总、订单、报表、促销命中一律按 id 聚合；SKU 串只用于展示、拣货、与仓库/渠道对账。
2. **历史单据必须快照 SKU 文本**（流水、采购行、入库行、订单行都冗余存了 `sku_code`）：仓库换码、商品换仓都会让 SKU 串变化。
3. **价格挂主体/变体，成本挂 (仓库, SKU)**：多仓同款的价格天然一致；成本才按仓分别记。
4. **属性属于商品**：规格的真源在 `product_variants.option_values`；**不要从仓库编码解析属性**（遇到 `HS-RD-SM`、`RED_BIG` 这类对方命名会崩；同一 variant 在多仓多码，按编码聚合会把一件货拆成几份）。
5. **销售统计按 `variant_id` 聚合**，不按仓库 SKU 聚合。
6. **共享口径不复制**：容器主体 SKU 只有一个拼接入口 `buildProductContainerSKU`（商品新建 / 变体生成 / 捆绑三处共用）；剥前缀的规则也各域只留一份实现（商品侧一份、入库侧一份，两处等价性有测试钉住）。

---

## 3. 数据模型

列名口径（全局约定，迁移 `205`/`199`）：时间列一律 `create_time` / `update_time`（`timestamptz`）；`inventory_change_reasons.id` 是 `bigint identity`（199 由 uuid 改来），其余商品/库存主键都是 `uuid`（应用层生成，`uuid.NewString()`；流水与变更记录用时间有序 ID `utils.NewTimeOrderedID()` 即 UUIDv7）。

### 3.1 商品侧

**`products`（商品容器）** — 建表 `081`，扩展 `088`/`091`/`094`/`238`/`246`

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | uuid PK | 应用层生成 |
| `project_id` | uuid NOT NULL | 工程（RLS 名单内） |
| `name` / `subtitle` / `slug` / `status` | text | `slug` 工程内唯一（`uq_products_project_slug`）；`status` ∈ draft/published |
| `type` | text NOT NULL DEFAULT 'variant' | `variant` 常规变体商品 / `bundle` 捆绑容器（迁移 `238`）；**创建后不可改** |
| `sku_code` | text NOT NULL DEFAULT '' | **容器主体 SKU**（迁移 `246`），工程内唯一（偏索引）；存量空串 |
| `published_at` | timestamptz NULL | 最近一次进入 published（自动标签「新品」的判定基准，迁移 `091`） |
| `description` / `images` / `images_alt` | jsonb | 富文本（服务端白名单清洗）与图集（alt 逐位对应，可翻译） |
| `attribute_ids` / `category_ids` / `tag_ids` / `related_ids` | jsonb 数组 | **只存引用**；同一属性组可被多商品复用 |
| `primary_category_id` | uuid NULL FK→product_categories | 主分类；不变量：必属于 `category_ids`（service 维护） |
| `brand_id` | uuid NULL FK→product_brands ON DELETE SET NULL | 一个商品一个品牌 |
| `bundle_items` | jsonb | 捆绑配置（成员/选项，对象 + CHECK 形状约束，迁移 `114`） |
| `default_price` / `default_compare_price` / `default_cost_price` | numeric(12,2) NULL | 商品级**默认值模板**（新增变体时逐字段填充）；`default_cost_price` 已被 `docs/14` 标注为退场候选（§7-C3） |
| `default_image` / `metadata` | text / jsonb | |
| `seo_title` / `seo_description` / `unit` / `weight` / `sort` | | |

**`product_variants`（可售规格，一行一个 SKU）** — 建表 `081`

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | uuid PK | |
| `product_id` | uuid NOT NULL FK→products ON DELETE CASCADE | |
| `sku_code` | text NOT NULL | 商品内唯一 |
| `barcode` | text | |
| `price` / `compare_price` / `cost_price` | numeric(12,2) | 售价 / 划线价 / **变体级成本（兼容列，见 §7-C3）** |
| `option_values` | jsonb | `{属性组key: 属性值key}`，规格组合的唯一真源 |
| `enabled` | bool | 下架规格不参与前台筛选与计价 |
| `sort` / `image` / `metadata` | | |

> `stock_total` / `stock_synced_at` 两列**已在迁移 `121` 删除**。商品侧不再有任何库存副本，展示值一律是查询期投影（`product_resp.go` 的 `fillProductStock`）。

商品侧其余表：`product_attributes`（属性组 + 值，JSONB 合一行；`is_variation` 决定是否参与变体）、`product_categories`（父子树 + slug 唯一 + SEO）、`product_brands`、`product_tags`（手工 + 内置规则自动）、`product_price_adjustments` + 明细（定价留痕）、`product_ratings`（评分明细，均分是投影）。

### 3.2 库存侧

**`inventory_warehouses`（仓库）** — 迁移 `099`，扩展 `202`/`240`

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | uuid PK | |
| `project_id` | uuid NOT NULL | |
| `code` | text NOT NULL | **短码**：工程内 `upper(code)` 唯一；只允许 `A-Z0-9`、≤8 位（下划线是 SKU 分隔符，短码里禁止出现）；**它是商品侧 SKU 的前缀来源** |
| `name` / `sort` | | |
| `type` | text CHECK(self/third_party/virtual) DEFAULT 'self' | 虚拟仓不能设为默认仓（迁移 `240`，`ErrWarehouseTypeVirtualDefault`） |
| `status` | text CHECK(active/disabled) DEFAULT 'active' | 默认仓**不能停用**（迁移 `202`） |
| `is_default` | bool | **每工程至多一行**：部分唯一索引 `uq_inventory_warehouses_project_default` |
| `config` | jsonb | 第三方仓对接配置（凭据加密，读出时脱敏） |

**默认仓不变量**（service 层维护 + 索引兜底）：工程内第一个仓自动成为默认仓；默认仓不能删、不能停用、不能取消默认；「未指定仓库」时**必须**解析到它，缺默认仓直接报 `ErrWarehouseDefaultMissing`（**不静默挑仓**）。

**`inventory_stocks`（库存真源）** — 迁移 `099`，扩展 `244`/`251`/`261`

| 列 | 类型 | 说明 |
|---|---|---|
| `id` | uuid PK | |
| `project_id` | uuid NOT NULL | |
| `warehouse_id` | uuid NOT NULL FK→inventory_warehouses ON DELETE CASCADE | 删仓连带删库存行（service 先拦「仓内有非零库存」） |
| `product_id` | uuid NOT NULL（**无外键**） | 冗余快照列，见 §7-C2 |
| `variant_id` | uuid NOT NULL FK→product_variants ON DELETE CASCADE | 变体被删即无货可存 |
| `sku_code` | text NOT NULL DEFAULT '' | **仓库侧裸码**；仓内唯一 |
| `external_sku` | text NOT NULL DEFAULT '' | 外部/第三方编码（N:1，普通索引） |
| `track_quantity` | bool NOT NULL DEFAULT false | **false = 无限**（不跟踪）；`CHECK (track_quantity OR quantity = 0)` |
| `quantity` | int NOT NULL DEFAULT 0 | 可用量真源 |
| `cost_price` | numeric(12,2) NULL | **(仓库, SKU) 的当前成本**；NULL = 尚未核算 |

三条语义（优化时必须保住）：
- **唯一真源**：一切影响可用量的判断只读本表并**加行锁**；`product_variants` 上已无任何库存缓存列。
- **`quantity = 0` 有两义**（跟踪且卖光 / 不跟踪无限），区分它们的唯一依据是 `track_quantity`，**任何投影都必须把开关一起带出去**。
- **成本 NULL ≠ 0**：0 是合法的显式成本（赠品 / 内部划拨），未核算必须留 NULL。

索引：`(project_id, sku_code)`、`(warehouse_id)`、`(product_id)`、`(warehouse_id, external_sku)`。

**`inventory_stock_movements`（库存流水，分区表）** — 迁移 `102`，扩展 `199`/`256`

| 列 | 说明 |
|---|---|
| `id` | uuid（时间有序 ID） |
| `project_id` / `warehouse_id` / `product_id` / `variant_id` / `sku_code` | 行标识与快照 |
| `direction` | `in` / `out` / `adjust`（CHECK） |
| `quantity` / `delta` / `quantity_before` / `quantity_after` | 绝对量（>0，CHECK）/ 带符号量 / 变动前后值（after ≥ 0，CHECK） |
| `reason_id` (bigint NULL FK ON DELETE SET NULL) / `reason_code` | 原因字典引用 + **code 快照**（字典删了历史仍可读） |
| `parent_variant_id` | BOM 展开时记录是哪个父 SKU 引出这条子项流水 |
| `source_type` / `source_ref` | 来源引用（`order`+订单号 / `purchase_order`+单号 / `production`+入库单号 / `inventory_page`…） |
| `unit_cost` | 本次变动时刻的成本留痕（迁移 `256`）：出库复制当时库存行成本；入库记显式成本 |
| `batch_id` | 一次变动（含展开的全部子项）共用，便于整体回溯 |

分区：按月 + `_default`；RLS 策略装在父表，**分区子表由 `internal/partition.EnsureAhead` 单独安装**（PG 的 ENABLE/FORCE 不递归到分区）。

**`inventory_change_reasons`（变动原因字典）** — 迁移 `102`/`103`/`241`

- `project_id IS NULL` = **内置原因**（全工程可见、只读、不可改名）；非 NULL = 工程自定义（工程内 `code` 唯一、可改名/停用）。
- `name` 存的是 **i18n key**（`inventory.reason.<code>`，迁移 `241` 收口），不是文案。
- 内置 code（迁移 `103`）：`purchase_in` / `return_in` / `transfer_in` / `production_in`（in）；`sale_out` / `damage_out` / `transfer_out`（out）；`stocktake_adjust` / `manual_adjust`（adjust）。
- **方向必须与本次变动一致**；停用的原因不能再被新变动引用（历史流水照旧可读）。

**`inventory_bom_items`（物料清单）** — 迁移 `102`

- `parent_variant_id` + `component_variant_id` + `quantity`（`CHECK quantity > 0`、`CHECK parent <> component`、`UNIQUE(parent, component)`），冗余 `parent_sku_code` / `component_sku_code` 快照。
- 全量替换（先删后写）；空清单 = 清空；成环在维护入口拒绝（`assertBOMNoCycle`），展开时另有 `maxBOMDepth = 8` 兜底。

### 3.3 采购侧

**`inventory_sources`（货源：供应商 / 集团内关联公司 / 自家工厂）** — 迁移 `105`

| 列 | 说明 |
|---|---|
| `type` | `external` / `internal`（CHECK）—— 决定是否按内部交易口径出报表 |
| `related_party` | **关联方标志**，独立于类型；`CHECK (type = 'external' OR related_party)` 兜住「内部即关联方」 |
| `settle_price` | numeric(12,2) NULL，**只属于内部货源**（`CHECK` 兜住）+ 非负 |
| `status` | active / disabled（停用 = 不再选用；历史口径不动） |
| `config` | jsonb，**必须是对象**（异构对接扩展信息） |
| `code` | 工程内 `upper(code)` 唯一 |

统计出口：`SourceSummary` 按「类型 × 关联方」分组（报表区分口径）。**不建「货源 ↔ SKU」映射表**（货源货号记在采购行上，历史采购价从流水/入库行查）。

**`inventory_purchase_orders`（采购单头）** — 迁移 `108`

`code`（工程内 upper 唯一）、`source_id` FK→inventory_sources、`warehouse_id` FK→inventory_warehouses（收货仓）、`status` CHECK(pending/partial/received)、`ordered_at` / `expected_at` / `remark` / `operator_id`。

**状态是推导值**：由「已入库数量 vs 采购数量」推出，**没有任何人工置位入口**；建单、改单、每次收货后在同一事务内重算写回（`derivePurchaseStatus`）。**没有「已取消」状态**（见 §7-B5）。

**`inventory_purchase_order_lines`（采购行）** — 迁移 `108`

`order_id` FK ON DELETE CASCADE、`product_id` / `variant_id` / `sku_code` 快照、`quantity > 0`、`received_quantity`（`CHECK between 0 and quantity`）、`unit_price >= 0`（单一金额，无币种/税额/折扣列）、`sort` / `remark`。
唯一：`uq_inventory_purchase_lines_order_variant`（一单一 SKU 一行 —— 已入库数量的归属必须不含糊）。

**`inventory_purchase_receipts`（入库单头）** — 迁移 `108`

`kind` CHECK(`purchase` / `production`)、`order_id` NULL FK、`source_id`、`warehouse_id`、`request_id`（**幂等键**，`uq_..._project_request` 偏唯一索引，空串不参与）、`status` CHECK(`pending` / `posted`)、`movement_batch_id`（指向本批流水）、`received_at` / `operator_id`。
`CHECK ((kind = 'purchase') = (order_id IS NOT NULL))` —— 采购收货必有采购单，生产入库必无。

**`inventory_purchase_receipt_items`（入库单行）** — 迁移 `108`

`receipt_id` FK ON DELETE CASCADE、`line_id` NULL FK→采购行、`product_id` / `variant_id` / `sku_code` / `quantity > 0` / `unit_price >= 0`（**当时单价快照**）、`cost_updated` + `cost_error`（商品侧成本回写的留痕）。

### 3.4 审计侧

**`master_data_changes`（字段级变更记录，append-only + 分区）** — 迁移 `111`/`112`/`113`

一行 = **一个字段**：`entity_type` / `entity_id` / 展示名快照 / `action` / `field` / `old_value` / `new_value` / `origin`（来源：product / variant / source…）/ `operator_id` / 时间。只写真正变化的字段（`old == new` 不落行）；新增与删除也逐字段落行（old 或 new 为空）。**append-only 由 `BEFORE UPDATE OR DELETE` 触发器在数据库层兜底**。
写入方：`product` 与 `inventory` 通过 `masterdatacontract.MasterDataService.RecordChanges(Tx)` 递进「改前/改后」快照。**职责分离**：流水记数量变动，本表记字段级配置变更（价格 / SKU 编码 / 上下架 / 归属仓 / 货源资料），各记一处、互不替代。

---

## 4. 核心流程

### 4.1 建仓（`inventory_warehouse.go`）

- 校验：名称必填；短码归一为 `upper` 且只允许 `A-Z0-9`、≤8；状态 / 类型归一；工程内短码唯一预检。
- **工程内第一个仓自动成为默认仓**（`asDefault = req.IsDefault || len(existing) == 0`）；虚拟仓不能当默认仓。
- 切换默认仓：同一事务内先清旧标记再置新标记（`CreateWarehouse(asDefault)` / `SetDefaultWarehouse`），并发由部分唯一索引兜底。
- 删仓守卫：默认仓拒绝；**仓内有非零库存拒绝**（`CountNonZeroStocks`），否则外键级联会把货一起删掉（静默丢账）。

### 4.2 建商品与建 SKU（`product_crud.go` Create）

**一个事务 = 商品 + 首个变体 + 各仓库存行 + 变更记录**（`s.m.Transaction` + `rls.ScopeTx`）。

流程（variant 类型）：
1. 归一 slug（工程内唯一预检）、校验属性 / 分类 / 品牌 / 标签 / 相关商品引用（存在 + 同工程 + 不自引用）。
2. 解析目标仓：`WarehouseIDs`（多仓）> `WarehouseID`（单值兼容）> 空（默认仓）；**保持顺序、按仓 id 去重**；任一仓不可用（不存在 / 停用 / 跨工程）即整体失败。
3. 定**认领仓**（第一个），决定主体 SKU 前缀。
4. SKU 来源二选一（`skuSource`）：
   - `warehouse`（从仓库选）：服务端**不信任前端提交的编码**，用 `pickWarehouseSKU` 在认领仓复核那条货确实存在，取它的裸码；外码取「显式填的 > 那条货已登记的 > 它自己的 SKU」。
   - `custom`（自己创建）：编码原样 + 认领仓前缀；只在显式填了外码时才写。
   - 归一外码（`NormalizeExternalSKU`）。
5. 生成容器主体 SKU（`buildProductContainerSKU`）+ **工程内唯一预检**（`ensureContainerSKUFree`，落库侧另有 23505 → 业务错误的兜底映射）。
6. 首个变体由商品级默认值填充（`newVariantFromDefaults`），其 SKU == 主体 SKU（无规格变体就是容器主体）。
7. 事务内：写商品 + 变体 → 逐仓建库存行（**裸码按认领仓剥一次**；该仓已有同裸码则**复用不新建**；外码只写在认领仓那一次，写前做 N:1 弱校验）→ 记静态产物失效事件 → 写变更记录（商品一条 + 变体一条，逐字段落行）。
8. 事务外：重算自动标签（时机「商品写操作后」）。

bundle 类型分支：**不生成变体、不建库存行**；主体 SKU 必填且以 `_B` 结尾；容器价必填且 > 0（`ErrBundlePriceRequired`）；事务内只有「商品 + 产物失效 + 变更记录」三处写入。

### 4.3 变体生成与「预览—保存」模型（`product_variant_generate.go`）

- 勾选属性值 → 笛卡尔积 → **SKU 由系统生成、可逐行编辑**；维度/数量超限整体拒绝。
- **预览不落库**：勾选生成、删除/取消勾选都只改前端清单；点「保存」才以清单为准落库（新增缺失的、更新改过 SKU 的、删除清单外的既有变体）。
- 抽屉打开时的清单种子 = **库里已有的变体**（不是重算笛卡尔积），否则上次删掉的组合会自己冒出来。
- 保存守卫：清单外要删的既有变体命中**四个引用面**之一时**逐条跳过并在结果里列明**（不静默、也不因一行拦不住就整批失败）：
  ① 库存非零；② BOM 子项引用；③ 捆绑成员引用（`products.bundle_items.options[].variantId`，**跨工程可发现**）；④ 有过任何库存流水（≈ 被订单用过）。
- 整个保存（新增 / 更新 / 建库存行 / 删除 / 留痕）在**同一个事务**里。

### 4.4 库存变动契约（`inventory_change.go`）—— 全系统最关键的一段

两条公开能力：
- `ChangeStock`：`in`（入库）/ `out`（出库）/ `adjust`（盘点调整到**目标绝对量**）。
- `DeductStock`：出库，**不足即整体拒绝**；`ExpandBOM` 为真时先按 BOM 展开成叶子 SKU。
- 各有 `…Tx` 版本：**不自己开事务、错误原样返回**，专供同库跨模块调用方（订单建单、取消归还、退货入库）把库存变动纳入自己的事务。

入参归一：方向必须合法；行数上限 `maxBatchLines = 200`；仓库逐级兜底（行指定 → 请求级 → 默认仓）；**变动原因必须是字典条目且方向一致**；显式成本可为空（= 本次不碰成本），给了就必须是 ≥ 0 的有限数。

事务内三段式（顺序不可合并、不可重排）：
1. **幂等建行**：目标 `(variant, warehouse)` 不存在时 `INSERT … ON CONFLICT (variant_id, warehouse_id) DO NOTHING`，按标识升序。用 `DO NOTHING` 而不是让它报错 —— 一句报错的 INSERT 会把调用方的整个事务标记为 aborted。
2. **按序加锁**：全部目标行按 `(variant_id, warehouse_id)` **升序**逐一 `SELECT … FOR UPDATE`（与入参顺序无关 ⇒ 全局同一锁序 ⇒ 等待链不可能成环）。
3. **按序应用**：锁内算 delta 与前后值；`after < 0` → `ErrStockInsufficient`（整体回滚）；**调整到当前值不写流水**；随后逐行写回数量 + 跟踪开关，写成本（仅显式传了成本的行，覆盖式），批量写流水（同 `batch_id`）。

特殊语义：
- **无限库存（`track_quantity = false`）**：出库**不校验可用量、不扣减、不写流水**（`continue`）；入库/调整只要显式给了数量就把该行切成跟踪。
- **成本留痕 `unit_cost`**：本次带显式成本就记它；否则复制该库存行**变动前**的当前成本（出库由此固定「实际发出那批货的成本」）；未核算留 NULL。

### 4.5 采购单（`inventory_purchase.go`）

- 建单：单号归一为大写（工程内唯一）；货源必须存在、同工程、启用（停用 = 不再选用）；收货仓为空兜底默认仓；行构造时**逐行归一仓库侧 SKU 编码**（剥前缀 + 空串拒绝）并拒绝同单重复 SKU；单头 + 行同一事务写入；状态由行推导。
- 改单：先**锁单头**（与收货共用同一把锁），行全量替换**只允许在「一行都还没入库」时**（`ErrPurchaseLinesLocked`）；换收货仓时会按新仓重新归一行的 SKU 快照。

### 4.6 登记收货入库（`inventory_receipt.go` RegisterReceipt）—— 事务形状是刻意的

```text
① 幂等预检：request_id 命中既有入库单 → 原样返回（顺带补做商品侧成本回写），不第二次动库存
② 一个事务内：
   锁采购单头（FOR UPDATE）
   → 解析货源 / 收货仓（本次指定 > 单上收货仓 > 默认仓）
   → 读行；全部已收满则拒绝（ErrReceiptOrderDone）
   → 逐行：校验（行存在、数量 > 0、无重复行、SKU 按**本次收货仓**归一）
           + 已入库数量**原子递增**：UPDATE … SET received_quantity = received_quantity + n
             WHERE id = ? AND received_quantity + n <= quantity（受影响行数 0 即超收 →
             ErrReceiptOverReceive，整批回滚）
   → 用最新行重算采购单状态并写回
   → 写入库单头 + 入库行（单价 = 本次到货价优先，否则采购行单价）
   → 库存变动（复用 4.4 的同一路径）：in + purchase_in + source=purchase_order:单号，
     每行带 CostPrice = 本次单价 —— **仓库侧成本与数量同一事务生效**
   → 入库单置 posted 并写回 movement_batch_id
③ 提交后（事务外）：经 VariantCostPort 回写商品侧 product_variants.cost_price
   （幂等 + 留痕 cost_updated/cost_error + 可由同一 request_id 重放补做）
```

要点：**先记账再动库存**（超收守卫必须在库存变动之前）；**跨模块 DB 补偿已删除**（同库同事务，失败即整体回滚）；并发重复提交在 `request_id` 唯一键上撞车后回退为「命中既有单」返回。

### 4.7 生产入库（同文件 RegisterProductionInbound）

无采购单（`order_id` 为 NULL，DDL 钉死）、来源必须是**内部**货源、**成本价手工填写**（缺即拒绝）、SKU 编码由调用方给出（同一条归一入口）。事务形状与采购收货一致（建单 + 库存变动 + 置 posted），提交后同样 best-effort 回写商品侧成本。

### 4.8 BOM 展开（`inventory_bom.go`）

- 只在 `DeductStock` 且 `ExpandBOM = true` 时发生（逐层 BFS，最多 8 层）。有清单的 SKU 被换成子项（用量 = 子项用量 × 请求量），无清单的即叶子。
- **子项沿用父项解析出的仓库**（`child.warehouseID = parent.warehouseID`）—— 一次扣减的仓库口径唯一，也意味着**没有跨仓寻源**（§7-B4）。
- 祖先链随节点携带，出现环即拒绝（维护入口已拒绝成环，这是第二道防线）。
- 展开后的子项商品 id 由真源快照解析（`item.productID = ""`，建行时从既有库存行取）。

### 4.9 销售出库与退货入库（边界，`order` 模块）

- 建单：订单 + 明细 + 流水 + 券核销 + **扣库存**全在订单事务内，扣减走 `DeductStockTx`（原因 `sale_out`，来源 `order:订单号`）；**不传 `WarehouseID`** → 全部落到该工程默认仓（§7-B1）。
- 出库流的成本快照：`order_items.cost_price` 取自「该变体在**行归属仓**的当前成本」（`ResolveVariantWarehouseCosts`），未核算留 NULL（迁移 `256` 去掉了 `NOT NULL DEFAULT 0`）。
- 取消订单 → `ChangeStockTx`（方向恒 `in`，原因 `return_in`）；退货入库走 RMA 的 `approved → received` 门闩，只有跨过它的那一次调用执行入库（`ChangeStock` 没有幂等键，这道门闩是唯一护栏）。

### 4.10 商品侧库存展示（`product_resp.go`）

商品列表的「库存」列是**查询期投影的虚拟字段**：一次批量读 `WarehouseStocksByProducts`，按三态归并 ——
`StockStateNone`（无任何库存行 → 未入库）/ `StockStateInfinite`（**任一仓不跟踪 → ∞，绝不求和**）/ `StockStateTracked`（全部跟踪 → 求和）。
分仓明细逐仓套用同一条 `aggregateStockState` 口径（避免「列表求和、明细显示 ∞」的自相矛盾）。**投影永不参与扣减。**

---

## 5. 不变量清单（可直接当验收表）

| # | 不变量 | 落点 |
|---|---|---|
| 1 | 库存只有一个真源：`inventory_stocks.quantity`；影响可用量的判断只读它并加行锁 | 全库；商品侧无库存列 |
| 2 | 有变动必有流水，数量写回与流水同一事务；无数量变动不写流水 | `applyStockChangesTx` |
| 3 | 多行变动按 `(variant_id, warehouse_id)` 标识升序加锁，与入参顺序无关（全局锁序） | `uniqueSortedKeys` / `sortItems` |
| 4 | 「建行」与「加锁」必须分两趟，不能边建边锁（否则真成环） | `applyStockChangesTx` ①② |
| 5 | 一次用户可感知的写操作涉及 ≥2 处持久化写入时必须同事务；跨模块只传 `*gorm.DB` 句柄给对端 `…Tx` 方法 | 商品创建 / 变体保存 / 采购收货 / 入库 / 货源增删改 |
| 6 | 读-改-写必须有行锁或守卫写进 WHERE 的原子 SQL（禁「先读后算再写回」） | 已入库数量递增、默认仓切换、货源改删 |
| 7 | 冲突与数据不一致**打回给人**，不自动加后缀 / 静默合并 / 丢行 | 迁移 244/262 的撞码预检、引用校验一次列全 |
| 8 | 每工程恰有一个默认仓；默认仓不可删 / 不可停用 / 不可取消默认；「未指定仓库」必解析到它 | `resolveWarehouse` + 部分唯一索引 |
| 9 | 仓库侧 SKU 恒为裸码；剥前缀幂等、大小写不敏感、只剥一次；入库空串一律拒绝 | `normalizeStockSKU` |
| 10 | 商品侧 SKU 前缀恒取**认领仓**；多仓各建一行、已有同裸码即复用 | `claimWarehouse` / `createStockRowsTx` |
| 11 | 主体 SKU 工程内唯一（预检 + 索引兜底，绝不把 23505 抛给页面）；变体商品内唯一；仓内唯一 | 迁移 246/081/244 |
| 12 | 外部编码 N:1（同一仓同一外码 → 同一商品），无唯一索引、靠弱校验 | 迁移 251 |
| 13 | `quantity = 0` 两义，任何投影都必须带 `track_quantity`；混合状态绝不求和 | 迁移 261、`aggregateStockState` |
| 14 | 成本 `NULL = 未核算`，绝不用 0 冒充；成本不做流水（只记当前值），历史由 `unit_cost` 留痕 | 迁移 244/256 |
| 15 | 采购单状态是推导值（无人工置位）；已入库数量恒 ≤ 采购数量（DDL + 原子守卫双保险） | `derivePurchaseStatus` / `IncrPurchaseLineReceivedTx` |
| 16 | 入库幂等：`request_id` 工程内唯一，重复提交原样返回、不第二次动库存 | 迁移 108 偏唯一索引 |
| 17 | 一切的写操作都要带**工程作用域**（`rls.InProjectScope` / `ScopeTx`）；缺作用域在非超级角色下是**静默 0 行** | 全模块 model 层 |
| 18 | 变体删除的四个引用守卫（库存非零 / BOM 子项 / 捆绑成员 / 有过流水） | `variantDeleteBlockReason` |
| 19 | 字段级审计 append-only（触发器兜 UPDATE/DELETE） | 迁移 111 |
| 20 | 只有 `inventory_stocks` 的写路径是变动契约；**任何**数量变化都不得绕过它写库 | 库存页行内编辑也走 `adjust` |

---

## 6. 接口与页面入口

### 6.1 API（前缀 `/api`，三层链 Session + CSRF + Casbin；权限点常量见 `internal/permission/codes.go`）

| 域 | 端点 | 权限点 |
|---|---|---|
| 仓库 | `GET /inventory/warehouse/{list,get}`、`POST /inventory/warehouse/{create,update,delete}` | `inventory:warehouse_*` |
| 库存 | `GET /inventory/stock/{list,sku,get}`、`POST /inventory/stock/{ensure,change,deduct}` | `inventory:stock_*` |
| 流水 | `GET /inventory/movement/list` | `inventory:movement_list` |
| 原因 | `GET /inventory/reason/list`、`POST /inventory/reason/{create,update}` | `inventory:reason_*` |
| BOM | `GET /inventory/bom/get`、`POST /inventory/bom/set` | `inventory:bom_*` |
| 货源 | `GET /inventory/source/{list,get,summary}`、`POST /inventory/source/{create,update,delete}` | `inventory:source_*` |
| 采购 | `GET /inventory/purchase/{list,get,history}`、`POST /inventory/purchase/{create,update,receipt,production}` | `inventory:purchase_*` |
| 商品 | `/api/product/{list,get,create,update,delete,...}`、`/api/product/variant/*`、`/api/product/tag/*`、`/api/product/pricing/*`、`/api/product/bundle/*` | `product:*` |

> 库存页的三条行内写入口（外码 `POST /admin/inventory/external-sku`、跟踪开关 `POST /admin/inventory/stock/tracking`、数量调整 `POST /admin/inventory/stock/change`）**复用同一个权限点** `inventory:stock_change` —— 登记外码与切开关不是新能力，而是同一张库存行属性的就地维护。

### 6.2 后台页面（前缀 `/admin`）

- 商品：`/admin/products`、`/admin/products/translations`、`/admin/products/template`、`/admin/products/bundle`、`/admin/product-categories`、`/admin/product-brands`、`/admin/product-tags`、`/admin/product-pricing`
- 仓库与库存：`/admin/inventory`（库存调整 / 某 SKU 各仓库存 / 流水）、`/admin/inventory/warehouses`、`/admin/inventory/reasons`
- 采购与货源：`/admin/inventory/purchases`（建单 + 逐行收货 + 生产入库 + 进货历史）、`/admin/inventory/sources`

**库存侧的写入口刻意只剩「库存调整（盘点 / 报损）」**：入库 / 出库一律来自单据（采购入库 / 生产入库在采购页，销售出库走发货，退货入库走退货单），商品页每个变体有「各仓库存」入口。

---

## 7. 已知缺口与优化观察点

> 分级：**A = 正确性/一致性风险**（会造成错账或守卫失效，优先处理）；**B = 能力缺失**（对照主流电商的功能落差）；**C = 结构性 / 遗留**（不影响当前正确性，但会限制演进）。
> 每条都给出可复核的证据位置。**未列出的部分不要当成已完善** —— 本文只写已核实项。

### A1. 无限库存的销售出库不留流水 → 变体删除守卫 ④ 失效（组合缺陷）

- `applyStockChangesTx` 对 `track_quantity = false` 的行在 `out` 方向直接 `continue`：**不扣减、不写流水**。
- 变体删除守卫 ④ 用「**有过任何库存流水**」等价「被订单用过」（`VariantHasStockMovement`）。
- 于是「该行始终 `track_quantity = false`（新建商品时不填数量即此形态、之后再没走入库把行切成跟踪）+ 已有订单成交」的变体会被判定为「没被用过」→ 允许硬删 → 订单行的 `variant_id` 追溯断链。
- 影响面同时包括销售报表：「这个 SKU 没卖过」与「这个 SKU 有销量但没记账」在流水上看不出差别。
- 证据：`inventory_change.go`（`if !e.TrackQuantity && it.direction == DirectionOut { continue }`）、`inventory_change_model.go`（`VariantHasStockMovement`）。

### A2. 货源删除没有引用守卫 → 撞外键，页面拿到原始 SQL 错误

- `DeleteSource` 只做「锁行 → 硬删 → 留痕」，**没有任何「被采购单 / 入库单引用」的检查**。
- 采购单与入库单的 `source_id` 是 `NOT NULL REFERENCES inventory_sources(id)`，**无 ON DELETE**（默认 NO ACTION）→ 删除被引用的货源会以 23503 失败。
- 代码注释写的是「被采购单引用的守卫由 issue #18 在服务层补」，实际未补（全仓 grep 不到任何 `ErrSourceReferenced` 类错误）。
- 证据：`inventory_source.go` 的 `DeleteSource`、迁移 `108` 的 FK 定义。

### A3. RLS 策略已铺满，但**当前一行都拦不住**

- 迁移 `215` 给带 `project_id` 的对象装了 `ENABLE` + `FORCE ROW LEVEL SECURITY`（覆盖面以 `pkg/rls` 的连接身份探针读数 `Identity.RLSTables` 为准，文档不写死数字），策略谓词读 `app.project_id`（未设置即行不可见，fail closed）。
- 但应用连接用的是**超级用户**，PostgreSQL 的超级用户恒绕过 RLS（`FORCE` 只约束到表属主）。**「策略铺好了」≠「隔离生效」**，判据只能从库里读（启动期探针 `database.CheckRLSIdentity` 会打 INFO/WARN）。
- 结果：代码里遍布的 `rls.InProjectScope` 当前只是「为将来切角色做好准备」；现在真正起隔离作用的是 `WHERE project_id = ?` 这类普通过滤。
- **切角色的顺序不能反**：先把各读写路径都包上作用域，再换非超级角色（`docs/rls-role-cutover.md`）。反过来会出现**静默 0 行**（功能「查不到数据」且无错误日志）。
- 另有已知缺口：`build_jobs` 与 `product_outbox_events` 两张表有 `project_id` 却没有策略（都是按状态跨工程领取的队列，豁免见 `docs/rules/database.md`）；`internal/pipeline` 与 `internal/builder/core` 不 import `pkg/rls`。

### A4. `product_variants` 没有 RLS 策略，也没有 `project_id` 列

- 变体表的跨工程可见性**完全靠调用方解析归属商品兜底**；`ListVariantsByIDs` 的 `projectID` 参数「当前不对本表构成过滤」（代码注释自述）。
- 变体级查询（`GetVariant` 等）不带作用域。若将来给变体表加策略，`ListVariantsByIDs` 等入口会突然从「能读到」变成「静默 0 行」—— 迁移时必须一并处理。

### A5. 变体删除守卫 ①②④ 只有单工程作用域

- 库存非零 / BOM 子项 / 有过流水三条查询都按**当前工程**作用域（代码注释自述属 DB-03 §6 第 7 条的已知面）。
- 只有 ③（捆绑成员引用）做到了跨工程可发现（`ProductRefsByBundleVariant` 逐工程枚举）。
- 含义：若库存行 / BOM / 流水来自另一工程（跨工程数据），守卫发现不了 → 放行删除 → 悬空引用。

### B1. 销售出库**不指定仓库**，恒扣默认仓

- 订单建单的 `StockLine` 只填 `ProductID / VariantID / SKUCode / Quantity`，`WarehouseID` 留空 → 库存侧按归属仓解析规则落到**该工程默认仓**。
- 多仓运营因此没有「发货仓选择 / 就近仓 / 按仓分单 / 缺货自动改仓」的能力；各仓库存的销售占用也区分不出来（流水里能看到 warehouse_id，但那是默认仓）。
- 对照主流：Shopify 的 location、Magento MSI 的 source selection 都把这层做成了可配置/可算法化的能力。
- 证据：`order_create_persist.go` 的 `deductStockTx`、`ordercontract.StockLine`（`WarehouseID` 注释：「为空表示按该 SKU 的归属仓由库存域解析」）。

### B2. 没有调拨单据能力

- 原因字典里有 `transfer_in` / `transfer_out`（迁移 `103`），但**没有调拨单表、接口、页面**（全仓无 `transfer` 业务实现）。
- 目前仓间移货只能靠「盘点/手工调整」两条流水拼出来（无原子跨仓事务、无「在途」概念）。
- 后果：多仓的库存准确性完全依赖人工；`DeleteWarehouse` 的提示文案（「必须先清货 / 调拨」）里提到的调拨尚无入口。

### B3. 成本只有「当前值」，没有计价方法与成本流水

- 口径（`docs/14` §4 明确）：`(仓库, SKU)` **只记一个当前成本**，覆盖式，最近一次入库或显式写入为准；**不做成本流水**。
- 唯一的历史痕迹是 `inventory_stock_movements.unit_cost`（出库 / 入库时刻的快照）。没有移动加权、FIFO、批次成本、成本重估。
- 影响：无法回答「这批货的成本是多少」「上月毛利按当时成本算 vs 按当前成本算差多少」；毛利分析只能按出库流水的 `unit_cost` 近似。

### B4. BOM 展开在单仓内进行，没有跨仓寻源

- `expandBOM` 把子项的 `warehouseID` 设为父项的仓库；若子件实际存放在另一仓，本次扣减直接以「本仓不足」整体失败。
- 同时 BOM 用量是**整数**（`quantity int > 0`，DDL CHECK），不支持小数用量（0.5kg 这类）。

### B5. 采购单没有取消 / 作废，也没有采购退货

- `status` CHECK 只有 `pending / partial / received` —— 建错的单子无法作废，只能留着。
- 没有「退货给供应商 / 红字入库」能力；入错库只能靠盘点调整冲回（在流水上表现为 `stocktake_adjust`，与真实采购退回混在一起）。
- 采购行金额只有单一 `unit_price`：无币种、税额、运费、折扣列；单头无审批人 / 交期管理（只有 `expected_at` 与 `remark`）。

### B6. 捆绑（bundle）商品的成交链路未接通

- `products.type = 'bundle'` 的商品**不生成变体行、不建库存行**（`product_crud.Create` 的 bundle 分支）。
- 购物车加购只接受 `VariantID`（`cart/dto/cart_req.go`），`order` 与 `cart` 两个模块内 grep `bundle|Bundle` **无任何命中**。前台 `bundleConfigurator` / `bundleConfiguratorCheck` 片段是**纯计算、不写库、不预占库存**。
- 因此：捆绑商品目前能展示、能配置校验，但**没有把它变成订单项的路径**；设计意图「捆绑的库存按成员 BOM 扣减」在订单链上也不成立 —— `ExpandBOM` 只出现在库存域的 dto / service / contract 里，唯一可能打开它的是 `POST /api/inventory/stock/deduct` 的 `expandBom` 参数，**商品与订单链路从不传 true**。
- 这条对优化方最关键：**要么补订单侧展开，要么明确不在本期范围**。

### C1. 商品侧 `cost_price` 与仓库侧 `cost_price` 双写（两个真源）

- 主载体是 `inventory_stocks.cost_price`（随库存变动同事务写入）；`product_variants.cost_price` 是**事务外的兼容回写**（跨模块端口无 Tx 形态，失败记在入库单行 `cost_error` 上，可由幂等重放补做）。
- 读路径已切到仓库侧（订单成本快照 `ResolveVariantWarehouseCosts`、定价工具），但**商品编辑表单仍可写变体级 `cost_price`** → 两处可能长期不一致。
- `docs/14` §9.3 记载：`product_variants.cost_price` 与 `products.default_cost_price` 本批未删，删列需另开一批并先做数据搬迁。

### C2. `inventory_stocks.product_id` 无外键

- 该列是快照（`NOT NULL` 但无 `REFERENCES products`），商品删除是由「商品→变体→库存行」的级联间接生效的。
- 一旦有路径直接改 `product_id`（或商品与变体归属不同步），会静默留下指向错误商品的库存行；`WarehouseStocksByProducts`、`CountNonZeroStocksByVariant` 等按 `product_id` / `variant_id` 的统计会给出矛盾结果。

### C3. 存量与新建的 `track_quantity` 默认值**相反**

- 迁移 `261` 把存量行一律置 `true`（保守：存量 0 分不清「占位」还是「卖光」，把卖光判成无限会直接超卖）；**新建行默认 `false`（无限）**。
- 同一张表因此混着两套默认语义：任何新增写路径都必须显式决定开关（`normalizeStockTracking` 只在「不跟踪却带数量」时兜底改判成跟踪）。
- 优化方若调整默认值，必须先决定存量数据怎么解释。

### C4. 剥前缀规则的固有边界

- 规则无法区分「带仓码前缀」与「裸码恰好以本仓短码 + `_` 开头」（例如 `SZ` 仓里真有一条 `SZ_001` 的裸码）。
- 迁移 `262` 只跑一次（`CheckSQL` 判「还有带前缀的行」即跳过），因此不会反复剥同一行；运维新增的脏数据不会被自动修正。

### C5. 少量遗留物与死代码

- `model.GetWithoutScope`（按 id 读商品不带作用域）**当前无生产调用方**，注释自述「不要加新调用方」。
- `StockTotals(ctx, projectID, ...)` 的 `projectID` 为空表示「不限工程」，在非超级角色下会静默 0 行。
- `StockChangeResp` 的 `CacheTotals / CacheSynced / CacheFailures` 三个字段是缓存时代的残骸（值恒为空 / true），**不要读成「同步确实成功了」**。
- 外部编码的 N:1 弱校验只在写入时生效，没有数据库约束兜底：**并发**下两个不同商品写同一外码可以同时通过预检（各自读到的都是「无冲突」）。

---

## 8. 复现与验证

```bash
# 表结构（权威）：本地库按迁移全量建表后直接读 information_schema
# 迁移清单与幂等条件：public/migrations/register*.go
# 当前 schema 说明文档：docs/schema-snapshot.md

# 商品 / 库存模块测试（真实 PostgreSQL）
go test ./internal/module/product/... ./internal/module/inventory/...
go test ./public/test/inventory/... -v

# 库存并发的关键回归（锁序 / 无死锁 / 超扣不存在）
go test -race ./internal/module/inventory/...

# 事务边界门禁（静态扫「一个 service 函数里 ≥2 处写却没有事务标记」）
go test ./public/test/architecture/...
```

**优化方最该先看的四处代码**（按信息密度排序）：
1. `internal/module/inventory/service/inventory_change.go` —— 变动契约（锁序、三段式、流水、成本留痕）。
2. `internal/module/inventory/service/inventory_receipt.go` —— 入库的事务形状与幂等。
3. `internal/module/product/service/product_crud.go` —— 建商品 → 建 SKU → 多仓认领建行。
4. `public/migrations/244_inventory_stock_cost.sql`、`251`、`261`、`262` —— 成本 / 外码 / 无限 / 裸码四条口径的原始论证（含**为什么**这么定，以及被否掉的替代方案）。
