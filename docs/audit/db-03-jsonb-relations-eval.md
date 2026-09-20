# DB-03 · JSONB 关系与工程边界评估

> 来源：[gpt-2026-09-19 审查报告](./gpt-2026-09-19/report.md) §DB-03（P2 · **待评估**）
> 范围：**只读评估**。本文档不新增迁移、不改表、不改任何 .go 文件。
> 结论一句话：**报告的问题成立，但需要收窄到具体列**；商品域真正值得动手的是 3 件低成本的事，
> 规范化成关系表在实测里只在「带 project_id 的反规范化形态」下才有收益，不建议整体改造。

---

## 1. 结论摘要

### 1.1 报告原文与核实结果

| 报告论断 | 核实结果 |
|---|---|
| 商品 category_ids/tag_ids 用 JSONB + GIN | **成立**。`081_product_tables.sql:95-96`，GIN 在 `:109-110`（`jsonb_path_ops`） |
| 品牌等标量引用有 FK | **成立**。`products.brand_id → product_brands(id)`（`081:99`），ON DELETE SET NULL |
| JSON 数组成员无法由标量 FK 保证存在性 | **成立**。5 个 JSONB 关系列没有任何数据库级引用完整性 |
| 单列对象 FK 也不保证 parent/child 的 project_id 相等 | **成立**。全库 161 个含 `project_id` 的对象里，只有 `pages` 有 `UNIQUE(id, project_id)` |
| 不能声称全库缺 FK | **成立**。当前 schema 有 **84 条外键约束**，其中声明在 `product*` 表上的 **13 条** |

### 1.2 本评估新增的三条实证

1. **写路径的缺口是具体的、只有一处**：`category_ids` / `tag_ids` / `attribute_ids` /
   `bundle_items.options[].variantId` 四条写入路径都有「存在性 + 同工程」服务层校验
   （`product_category.go:367`、`product_tag.go:431`、`product_attribute.go:294`、
   `product_bundle.go:311`）；**只有 `related_ids` 完全没有校验**，原样落库
   （`product_crud.go:158` 与 `:392-394`）。这是本次盘点里唯一确证的「进来就能写脏」的口子。
2. **删除守卫的失效形态是「工程作用域把守卫自己削弱了」**：分类 / 品牌 / 属性 / 捆绑变体四个守卫
   都写成 `rls.InProjectScope(ctx, m.db, projectID, …)`（`product_category_model.go:155`、
   `product_brand_model.go:137`、`product_attribute_model.go:200`、
   `product_model.go:591`），其中 `projectID` 是**发起删除的那个工程**。
   于是「另一个工程的商品引用了这个分类」对守卫不可见 ⇒ 守卫返回 0 行 ⇒ 删除放行 ⇒
   JSONB 数组里留下一辈子不会被清理的悬空 id（实测复现见 §3.2）。
   这是**语义上的自相矛盾**：守卫的目的是防悬空引用，而悬空引用恰恰最可能来自跨工程存量。
3. **规范化关系表的收益高度依赖形态，不能按「关系表更快」一概而论**（20 万商品 / 60 万关联实测）：
   - 带 `project_id` 反规范化的 junction（`(project_id, category_id)` 复合索引）：工程内反查
     **0.28ms vs JSONB 3.40ms（12 倍）**，全库反查 2.65ms vs 11.06ms（4.2 倍）；
   - **精简** junction（只有 `(product_id, category_id)`、工程靠 join products）：工程内反查
     **4.22ms，反而比 JSONB 的 3.40ms 更慢** —— planner 走 Hash Join + Seq Scan products。
   - 写侧代价：单行插入 1.7 倍；批量 20 万行 2.6 倍；体积 2.6～3 倍。

### 1.3 建议（按性价比排序）

| 优先级 | 动作 | 成本 | 说明 |
|---|---|---|---|
| **P0** | `related_ids` 写入加「存在性 + 同工程」校验 | 纯 service 改动，1 个方法 + 测试 | 与另外四列对齐，补上唯一确证的写入缺口 |
| **P0** | 四个删除守卫改成「不受工程作用域的引用存在性检查」 | 纯 model 改动 | 守卫的判据是「删了会不会留悬空」，本就不该被工程作用域限制 |
| **P1** | 把 §3.1 的审计 SQL 做成可重复执行的只读巡检（人工复核，**不自动清理**） | 零代码 | 报告验收要求的「必须先盘点存量」 |
| **P1** | `related_ids` 视需求决定：加 GIN 支持反查，或明确承认它没有反查需求 | 1 条迁移 / 0 | 当前无任何反查消费者（`fillRelated` 只解析分类/品牌/标签） |
| **P2** | 对 `products.brand_id` / `products.primary_category_id` / `product_categories.parent_id` 评估 `(project_id,id)` 复合唯一 + 复合 FK | 3 条迁移 + 存量校验 | 技术上可行且已实测（§3.4），但必须等存量盘点结论 |
| **不做** | 把 `category_ids`/`tag_ids`/`attribute_ids` 整体改成 junction 表 | — | 见 §4 的取舍理由 |
| **不做** | 把 Document / manifest / 快照类 JSONB 改成关系表 | — | 报告已明确排除；`209_blocks_id_uuid.sql:21-25` 列全了 10 个存储点 |

---

## 2. 现状盘点

### 2.1 JSONB 承载关系的列（商品域）

| 列 | DDL | 索引 | 目标实体 | 读侧用法 | 写侧 |
|---|---|---|---|---|---|
| `products.category_ids` | `081:95` | GIN `081:109` | `product_categories.id` | 集合筛选 `product_model.go:394`；调价筛选 `product_pricing_model.go:186`；**删除守卫** `product_category_model.go:157` | 整列替换 `product_crud.go:155`、`:379` |
| `products.tag_ids` | `081:96` | GIN `081:110` | `product_tags.id` | 筛选 `product_model.go:397,405,411`；反查 `product_tag_model.go:174,195` | 整列替换 + 按元素增删 `product_tag_model.go:218,231`；`product_crud.go:158,390` |
| `products.attribute_ids` | `086:19` | GIN `086:20` | `product_attributes.id` | **删除守卫** `product_attribute_model.go:199` | 整列替换 `product_crud.go:154,371` |
| `products.related_ids` | `081:97` | **无索引** | `products.id` | 仅读出到响应 `product_resp.go:85`；**没有任何反查** | **无校验**：`product_crud.go:158,393` |
| `products.bundle_items` 的 `options[].variantId` | 形状 114，索引 260 | GIN `260:20` | `product_variants.id` | **删除守卫** `product_model.go:594` | `product_bundle.go`（形状规范化 + 引用校验 `:285,387,625`） |

### 2.2 不是关系列、容易被误判的 JSONB

| 列 | 为什么不是关系列 | 证据 |
|---|---|---|
| `product_tags.rule_params` | 内置 3 种规则（`new_arrival` / `price_range` / `on_sale`）的参数全是标量与状态，不含任何实体 id | `enums/product_enums.go:152-156`、`service/product_tag_rule.go:53` 起 |
| `product_price_adjustments.filter` | 存 `categoryId`/`brandId`/`tagId`，但它是 **append-only 调价留痕**，语义是「当时按什么条件筛的」，不是当前关系 | `dto/product_pricing_req.go:25-27`、`095:24` |
| `product_price_adjustments.target_id` | 多态指向商品或变体（无 FK），同属历史快照 | `095:23`、`service/product_pricing.go:176` |
| `product_variants.option_values` | 属性 key → 展示值 的映射（GIN 用于按规格组合筛选） | `product_model.go:462` |
| `products.images` / `images_alt` | URL 字符串，非实体引用 | `081:94`、`094` |
| Document / manifest / 快照类 | 见 §5.3「不做」 | `209_blocks_id_uuid.sql:21-25` |

### 2.3 标量 FK 与工程边界

商品域里承载「商品 → 外部实体」的标量外键 3 条（全库 84 条 FK；声明在 `product*` 表上的共 13 条，其余是 `project_id → projects(id)`、`product_id → products(id)`、`adjustment_id`、`variant_id` 这类工程归属与内部父子）：

| 子列 | 目标 | DDL | ON DELETE | 问题 |
|---|---|---|---|---|
| `products.brand_id` | `product_brands.id` | `081:99` | SET NULL | 保证存在性，**不保证同工程** |
| `products.primary_category_id` | `product_categories.id` | `088:12` | SET NULL | 同上；且与 `category_ids` 的一致性只有服务层维护 |
| `product_categories.parent_id` | `product_categories.id` | `081:18` | SET NULL | **自引用，完全不检查工程** |

全库范围内（catalog 查询见 §3.3）「child 有 project_id ∧ parent 有 project_id」的外键共
**27 条**（按约束名去重后 **21 个**；多出来的 6 条是 `fk_inventory_movements_product` 在
父表与 6 个分区子表上的重复）。
全库现有 **8 条复合 FK，形态全是 `(目标id, 归属id) → (id, 归属id)`**（归属是
`page_id` 或 `presentation_instance_id`）；**没有一条把归属换成 `project_id`**。
商品域内除上表三条还有
`inventory_stocks.product_id`、`inventory_purchase_order_lines.product_id/variant_id`、
`inventory_bom_items.parent_variant_id/component_variant_id` 等（库存域的同类问题不在本票范围）。

### 2.4 读侧 / 写侧频率

| 关系 | 反向查找 | 删除限制 | 跨工程校验 | 写侧频率 |
|---|---|---|---|---|
| `category_ids` | 集合源按分类筛（`product_model.go:394`）+ 删除守卫 | 有（`ProductUsingCategory` `/`**限本工程**） | 有（`product_category.go:367`） | 每次保存商品整列重写；调价批改可批量更新整工程 |
| `tag_ids` | 标签列表「命中商品数」（`CountProductsByTag`）+ 反查清单 | 有（删除标签时**主动解绑**，`product_tag.go:331`） | 有（`product_tag.go:431`） | 自动标签重算 = 整工程 UPDATE（`product_tag_model.go:218,231`），**最高频的写路径** |
| `attribute_ids` | 仅删除守卫 | 有（`ProductUsingAttribute`） | 有（`product_attribute.go:294`） | 每次保存商品整列重写 |
| `related_ids` | **无** | **无** | **无** | 每次保存商品整列重写 |
| `bundle_items.options[].variantId` | 变体删除守卫（`VariantReferencedByBundleItems`） | 有 | 有（`product_bundle.go:311`） | 只在「保存捆绑配置」（SetBundleConfig）时写 |

> 关键不对称：**标签删除会主动解绑（补偿式清理），分类 / 属性 / 品牌删除只会「被引用即拒绝」**。
> 所以同一个守卫盲区在不同列上的后果不同 —— 标签删除后已按元素解绑（但只解绑标签自己工程下的
> 商品，`product_tag_model.go:237-248`），分类 / 属性删除后会永久留下悬空 id。

### 2.5 RLS 现状（决定「跨工程可见性」）

- 商品域带 `project_id` 的表（`products` / `product_categories` / `product_brands` /
  `product_tags` / `product_attributes` / `product_price_adjustments` / `product_ratings`）
  均为 `relrowsecurity=t`、`relforcerowsecurity=t`，策略谓词为
  `project_id = NULLIF(current_setting('app.project_id',true),'')::uuid`；
- **但应用连接用的是超级用户**（实测 `current_user=root, rolsuper=t`）—— PostgreSQL 的超级用户
  一律绕过 RLS，策略当前挡不住任何一行（与 AGENTS.md 的 DB-009 记录一致）；
- 直接后果：`ListCategoriesByIDs` / `ListTagsByIDs` / `ListBrandsByIDs` /
  `ListAttributesByIDs` / `ListVariantsByIDs` 当时**都没有包 `rls.InProjectScope`**
  （评估时的行号：`product_category_model.go:111`、`product_tag_model.go:124`、
  `product_brand_model.go:106`、`product_attribute_model.go:129`、`product_model.go:566`）。
  当时是「跨工程也读得到，靠 service 里的 `row.ProjectID != projectID` 判跨工程」；
  一旦按 DB-009 换成非超级角色而这些方法仍未包 scope，它们会**静默返回 0 行**，
  「跨工程」错误会退化成「不存在」（`ErrCategoryNotFound` 之类）。**换角色的顺序必须先补 scope。**
- **【DB-05 收口】上述五个方法已补 `rls.InProjectScope`**（`projectID` 为**必填形参**，
  与 `ListByIDs` / `ListForCollection` 同形；24 处调用点只做「多传一个已有工程变量」的
  机械透传，判定逻辑一行未改）。两条实测结论与原评估不同，按实测口径更正：
  1. **`ListVariantsByIDs` 那条推演不成立**：`product_variants` 自身没有 `project_id` 列、
     也不在迁移 215 的策略名单里（实测 `relrowsecurity=f`、`policies=0`），所以
     「换角色后会静默 0 行」是错的 —— 真实的风险方向**相反**：跨工程的变体在这里**读得到**。
     变体的跨工程拦截一直落在「按归属商品解析」那一步（`ListProductsByIDs(ctx, ids, projectID)`
     读 `products`，那是有策略的表；bundle 的三条路径都这么做）。该方法仍然包了作用域，
     理由是「将来给 `product_variants` 加策略时不至于突然退化成静默 0 行」，
     现状由测试钉住（见下）。
  2. **跨工程错误确实退化成「不存在」**：作用域下他工程的行不可见，校验路径上的
     `ErrCategoryProjectMismatch` 一类不再触发（`row.ProjectID != projectID` 成了第二道防线），
     行为是**仍拒绝、但提示从「工程不匹配」变成「不存在」**，可定位性下降。
     这是换角色的既定代价（本文件 §5.2 一行就是为此写的），**不接受为了保住原文案而绕开作用域**。
- **实测护栏**：`public/test/rls/nonsuperuser/`（真实 `NOSUPERUSER NOBYPASSRLS` 登录角色的
  第二条连接，不是 `SET ROLE`）。`nonsuperuser_test.go` 断言 fail closed / 只见本工程 /
  WITH CHECK 拒跨工程写 / `EnsureAhead` 新分区子表覆盖 / **每张带 `project_id` 的表
  ENABLE+FORCE+策略谓词读 `app.project_id` 的 catalog 全扫**（唯一豁免 `build_jobs`，
  理由写在测试里）；`product_byids_scope_test.go` 断言上面五个方法的
  「同一批 id 一起传、作用域 A 只拿得到 A」以及`ListVariantsByIDs` 的现状（含空串被
  `rls.ErrInvalidProjectID` 显式拒）。

---

## 3. 实测数据

### 3.1 开发库（wp）的直接测量：零样本，不具判别力

连接：`psql -h 127.0.0.1 -p 5432 -U root -d wp`（配置见 `config.yaml.example:33-44`）。
执行附录 A 的审计 SQL（**全部是 SELECT**）：

    == Q1 孤儿 products.category_ids ==          (0 行记录)
    == Q2 跨工程 products.category_ids ==        (0 行记录)
    == Q3 孤儿 products.tag_ids ==               (0 行记录)
    == Q4 跨工程 products.tag_ids ==             (0 行记录)
    == Q5 孤儿 products.attribute_ids ==         (0 行记录)
    == Q6 孤儿 products.related_ids ==           (0 行记录)
    == Q7 跨工程 products.related_ids ==         (0 行记录)
    == Q8 主分类不在 category_ids 里 ==          (0 行记录)
    == Q9 跨工程 products.brand_id ==            (0 行记录)
    == Q10 跨工程 product_categories.parent_id == (0 行记录)
    == Q11 孤儿 bundle_items.options[].variantId == (0 行记录)
    == Q12 跨工程 bundle_items.options[].variantId == (0 行记录)
    == Q13 跨工程 inventory_stocks.product_id == (0 行记录)

    基数快照：projects 1 / products 2 / product_variants 2 / product_categories 1 /
              product_brands 0 / product_tags 0 / product_attributes 1 / inventory_stocks 2

**这一组全 0 不能读成「没有存量问题」**：开发库只有 1 个工程、2 个商品、1 个分类，
连一个能构成跨工程场景的第二个工程都没有。它只证明**审计 SQL 在真实 schema 上语法正确、
可以只读执行**，证明不了数据质量。

### 3.2 夹具库验证：审计 SQL 的判别力（13/13 全部命中）

因为开发库零样本，另起**临时库 `db03_eval_tmp`**（用 `pg_dump --schema-only` 复制 `wp` 的
真实 DDL，10 张相关表），写入 2 个工程 + 故意构造的两类缺陷，再跑同一份审计 SQL。
**全程未触碰 `wp` 的任何一行**；夹具库在评估结束后已 DROP。

结果（13 项全部命中，节选）：

    == Q1 孤儿 category_ids ==
     工程A | a0000000-…-e1 | deadbeef-0000-4000-8000-000000000000        (1 行)
    == Q2 跨工程 category_ids ==
     A 的商品 → B 的分类；B 的商品 → A 的分类                            (2 行)
    == Q9 跨工程 brand_id ==    A 的商品挂 B 的品牌（外键只保证存在）     (1 行)
    == Q10 跨工程 parent_id ==  A 的分类父级在 B 工程                     (1 行)
    == Q11 孤儿 bundle variantId == (1 行)   == Q12 跨工程 bundle variantId == (1 行)
    == Q13 跨工程 inventory_stocks.product_id == (1 行)

**守卫盲区的实测复现**（同一夹具：工程 A 的分类 `a…0003` 只被工程 B 的商品引用）：

    == 在工程 A 里删这个分类：应用守卫的可见范围 ==
     守卫（project_id = A，ProductUsingCategory 谓词） | 0     ← 放行
     全局事实（同谓词，无工程过滤）                    | 1
    == 守卫返回 0 行 ⇒ DeleteCategory 放行 ⇒ 执行删除 ==
     DELETE 1
    == 结果：B 工程商品上留下的悬空分类 id ==
     22222222-…-e3 | a0000000-0000-4000-8000-000000000003   ← 悬空引用

同一夹具还实测了 **FK 只保护标量列、完全不碰 JSONB 数组**（在同一夹具库里
`DELETE FROM product_categories WHERE id = 'a0000000-0000-4000-8000-000000000001';` 即复现）：

    == 删除分类后 ==
     primary_category_id 被 FK SET NULL（对应商品该列变空）
     category_ids 里的悬空 id 原样保留（3 行）
     scalar_fk_intact = t

> 注：夹具里跨工程的 `category_ids` 是**直接 UPDATE 写进去的**，模拟历史存量 / 非 API 写入。
> 当前 service 校验会拒绝跨工程引用，因此这类行只可能来自：① 校验上线之前的存量；
> ② 绕过 service 的写入（插件 / 直连 / 数据导入）；③ 校验被后来的重构削弱。
> **存量到底有没有，必须用 §3.1 的 SQL 在真实库上回答**（本机答不了）。

### 3.3 全库 catalog 盘点（可复现）

    -- 含 project_id 的对象数 / 其中有 (id, project_id) 复合唯一的对象
    WITH pj AS (SELECT DISTINCT attrelid FROM pg_attribute
                WHERE attname='project_id' AND attnum>0 AND NOT attisdropped),
    uq AS (SELECT conrelid, conkey,
             (SELECT string_agg(a.attname, ',' ORDER BY a.attname) FROM pg_attribute a
               WHERE a.attrelid=conrelid AND a.attnum=ANY(conkey)) AS cols
           FROM pg_constraint WHERE contype IN ('u','p') AND array_length(conkey,1)=2)
    SELECT count(DISTINCT c.relname) AS tables_with_project_id,
           count(DISTINCT c.relname) FILTER (WHERE uq.cols='id,project_id') AS with_composite_uq,
           string_agg(DISTINCT c.relname, ', ') FILTER (WHERE uq.cols='id,project_id') AS which
    FROM pj JOIN pg_class c ON c.oid=pj.attrelid
            JOIN pg_namespace n ON n.oid=c.relnamespace
            LEFT JOIN uq ON uq.conrelid=pj.attrelid
    WHERE n.nspname = current_schema();

实测输出（wp 库）：

     tables_with_project_id | with_composite_uq | which
    ------------------------+-------------------+--------
                        161 |                 1 | pages

配套的「两侧都有 project_id 的外键有多少」查询（用于说明缺口规模）：

    WITH fk AS (SELECT con.conname, con.conrelid AS cid, con.confrelid AS pid
                FROM pg_constraint con JOIN pg_class c ON c.oid=con.conrelid
                JOIN pg_namespace n ON n.oid=c.relnamespace
                WHERE con.contype='f' AND n.nspname=current_schema()),
    hp AS (SELECT attrelid FROM pg_attribute
           WHERE attname='project_id' AND attnum>0 AND NOT attisdropped)
    SELECT count(*) AS both_sides_have_project,
           count(DISTINCT fk.conname) AS distinct_constraints
    FROM fk WHERE EXISTS (SELECT 1 FROM hp WHERE hp.attrelid=fk.cid)
              AND EXISTS (SELECT 1 FROM hp WHERE hp.attrelid=fk.pid);

实测：`both_sides_have_project = 27`、`distinct_constraints = 21`。

即 **`(id, project_id)` 复合唯一的先例只有 `pages` 一张表**；复合 FK 的先例则集中在
artifact / snapshot 父子关系上（`init_builder_schema.sql:227,261-262,286-288,311-312`、
`pages_active_artifact_fk` / `pages_staged_artifact_fk`）。
**这说明「复合 FK」在本库不是新发明，而是已有惯例，只是范围极窄。**

### 3.4 复合 FK 的技术可行性（已在临时库实测）

    CREATE TABLE t_brand (id uuid PRIMARY KEY, project_id uuid NOT NULL, name text,
                          UNIQUE (project_id, id));
    CREATE TABLE t_product (id uuid PRIMARY KEY, project_id uuid NOT NULL, brand_id uuid,
      CONSTRAINT t_product_brand_fk FOREIGN KEY (brand_id, project_id)
        REFERENCES t_brand (id, project_id) ON DELETE SET NULL (brand_id));

实测（PostgreSQL 18.6；CI 最低版本 `postgres:16`，该语法自 PG 15 起可用）：

| 场景 | 结果 |
|---|---|
| 同工程引用写入 | 成功 |
| 跨工程引用写入 | **被数据库拒绝**（`violates foreign key constraint`） |
| `brand_id` 为 NULL | 成功（MATCH SIMPLE：任一列为 NULL 即不校验） |
| 删除品牌 | **只把 `brand_id` 置 NULL，`project_id` 保持不变** |

> `ON DELETE SET NULL (brand_id)` 的**列清单形式是必需的**：不带列清单会把 `NOT NULL` 的
> `project_id` 一起置空而报错。这是复合 FK 落地时最容易踩的一步。

### 3.5 JSONB 数组 vs junction 表：写代价与查询收益

微基准，本地 PostgreSQL 18.6，**临时库**，合成数据（只有 id / 工程 / 关联三列，不含商品其余列与
业务索引），set-based 写入，`ANALYZE` 后取 `EXPLAIN (ANALYZE)` 的 Execution Time。
**这是设计取舍的比较，不是生产性能预测。**

规模：20 万商品 × 3 个分类 = 60 万关联，20 个工程（每工程 1 万商品），200 个分类。

| 维度 | JSONB（`category_ids` + GIN） | junction（`(project_id,category_id)` 复合索引） | 差异 |
|---|---|---|---|
| 反查「工程内某分类的商品数」 | 3.396 ms（`Index Scan using big_jsonb_proj`，1 万行里筛出 150 行） | 0.282 ms（`Index Only Scan`） | **junction 快 12 倍** |
| 反查「全库某分类的商品数」 | 11.060 ms（`Bitmap Index Scan on big_jsonb_gin`，GIN 生效） | 2.650 ms | junction 快 4.2 倍 |
| 删除守卫「工程内是否存在引用」LIMIT 1 | 0.073 ms | 0.089 ms | 持平（JSONB 略优） |
| 批量写入 20 万行 | 1623 ms | 918 + 3260 = 4178 ms | **junction 慢 2.6 倍** |
| 单行插入 5000 次（OLTP 路径，含 3 条关联） | 32.2 ms | 53.9 ms | junction 慢 1.7 倍 |
| 存储（20 万商品 / 60 万关联） | 33 MB | 99 MB | **junction 大 3.0 倍** |

**最反直觉的一条**：把 junction 精简成 `(product_id, category_id)` 两列（工程靠 join
`products` 得到）以后，工程内反查变成 **4.221 ms**（Hash Join + Seq Scan products），
**比 JSONB 的 3.396 ms 还慢**；体积 14 MB 也确实更小，但查询收益没有了。
⇒ **junction 的查询收益来自把 `project_id` 反规范化进关联表**，而不是来自「关系表」本身。
这条直接改变了本票的建议：如果规范化的理由是「反查性能」，就必须接受反规范化（以及它自己的
一致性成本）。

补充观察（同一基准、5 万行中等规模）：
- 只有一个工程时（工程过滤无选择性），JSONB 查询走 `(project_id)` btree + heap 过滤，
  **GIN 完全用不上**（0.292 ms vs junction 0.057 ms）；GIN 只在**无工程过滤的全库反查**上、
  且表足够大时才被 planner 选中（5 万行时仍是 Seq Scan，20 万行时才换成 Bitmap Index Scan）；
- 业务侧对应的既有事实：`194_p7_index_audit.sql:86-88` 记录的就是
  「`category_ids @> … OR primary_category_id = ?` 靠 BitmapOr 组合 GIN 与 btree」这一路径。

---

## 4. 候选方案对比

判据（按报告要求：先定关系基数，再看反向查找 / 删除限制 / 跨工程校验，最后比写负担与查询收益）：

| 方案 | 适用范围 | 迁移成本 | 写侧代价 | 查询收益 | 回滚难度 |
|---|---|---|---|---|---|
| **A. 保持 JSONB，补齐服务层校验** | `related_ids`（唯一确证缺口） | 0（不改 schema） | 0 | 0 | 无（纯代码，revert 即回滚） |
| **B. 保持 JSONB，修正删除守卫作用域** | 分类 / 品牌 / 属性 / 捆绑变体 4 个守卫 | 0（不改 schema） | 0（只在删除路径执行） | 删除路径从「静默放行」变成「发现跨工程引用并报出引用方」 | 无 |
| **C. 加 `(project_id,id)` 复合唯一 + 复合 FK** | `products.brand_id`、`products.primary_category_id`、`product_categories.parent_id` | **高**：3 张表加唯一索引（20 万级要重建索引）+ 迁移期必须验证存量无跨工程行，否则迁移直接失败 | 近 0（外键检查是行级索引查找）；代价主要在**删除**路径 | 跨工程引用从「靠 service 自觉」变成「数据库拒绝」；SET NULL 只作用于被引用列 | **中**：删约束容易，但期间靠约束才没写脏的数据回滚后会重新暴露，需保留审计 |
| **D. 规范化成 junction 表** | `category_ids` / `tag_ids` / `attribute_ids` | **最高**：建表 + 数据搬迁 + 双写回填 + 全部读路径改写（`product_model.go:374` 的集合投影、`product_pricing_model.go:171`、集合源解析、构建期取译文）+ 索引重建 | **1.7～2.6 倍**（实测）；自动标签重算（`product_tag_model.go:218,231`）要从「一条 UPDATE」变成「DELETE + INSERT 两段」 | 工程内反查快 12 倍（且必须反规范化 `project_id`）；**但商品列表「一次取回 3 个分类名」从列内联变成 join/聚合** | **高**：数据搬迁不可逆，必须双写回退窗口 |
| **E. 继续 JSONB 并加索引** | `related_ids`（如果将来需要反查） | 低（1 条迁移加 GIN） | 近 0 | 仅全库反查受益（GIN 对「工程内反查」帮助有限，见 §3.5） | 低（DROP INDEX） |

### 4.1 为什么 D（整体规范化）不建议做

1. **当前读路径是「列表页一次取回、内联解析」**：`product_model.go:372-381` 的集合投影
   直接把 `category_ids`/`tag_ids` 取出来，再由 `localizeRelatedFrom` 用批量索引解析名字
   （`collection_resolver.go:187-205`，注释明确写着「一次取好，零 N+1」）。
   换成 junction 后同一件事要么 join + 聚合回数组，要么再加一次查询 —— 这是**把当前的
   「1 条查询 + 1 次批量取关联实体」换成「1 条查询 + join/聚合 + 批量取」，服务的是
   一个当前并不存在的瓶颈**。
2. **写侧最高频的路径是自动标签重算**：`ReplaceTagProductsTx` 现在语义干净
   （「只动这一个 tag id 的元素」，`product_tag_model.go:200-233`）。换成 junction 后
   「按元素增删」要写成「按 tag 删除 + 重插」，还要在同一事务里保住「不碰其它标签」的语义。
3. **实测收益方向不唯一**：§3.5 已经证明换表不一定更快（精简 junction 反而更慢）。
4. **迁移不可逆**：60 万级关联的双写回填 + 回滚窗口，风险远大于收益。

### 4.2 什么情况下应该重新考虑 D

- 出现「按分类聚合商品数」这类**必须走索引**的高频查询（当前 `CountProductsByTag` 就是
  每行标签一次反查，`product_tag.go:277`；标签多时是 N 次），且 JSONB 的
  「工程 btree + heap 过滤」在真实数据上出现可观测的慢查询；
- 真实的**删除限制**成为产品强度需求：分类删除要在数据库层保证「全库无人引用」，
  且愿意为此先付一次跨工程守卫的语义修正。

---

## 5. 建议与前置条件

### 5.1 立刻可做（不需要任何存量确认）

1. **`related_ids` 补校验**：在 Create / Update 路径上复用 `ListProductsByIDs`
   （`product_model.go:257`，本身带工程作用域）做「存在 + 同工程 + 不能指向自己」校验，
   与 `resolveTagIDs` 同形。
2. **删除守卫改为「不受工程作用域的引用存在性检查」**：守卫要回答的是「全库还有谁引用它」，
   当前却把它限制在发起删除的工程内（`product_category_model.go:146-148` 的注释把
   「作用域 = 发起删除的工程」当成语义，实为 DB-009 接线时的牵连）。
   建议：分类 / 属性 / 捆绑变体守卫改成显式全库检查 + 命中时**报出引用方所在的工程**，
   由人决定；品牌守卫同理（它的后果是静默解绑，同样要报出跨工程引用）。
3. **把 §3.1 的审计 SQL 落成可重复执行的只读巡检**（附录 A 即完整 SQL）。

### 5.2 必须由人确认的前置（**报告明确禁止自动删数据**）

| 前置 | 为什么必须人工 |
|---|---|
| 孤儿 / 跨工程存量的实际数量 | 本机开发库零样本，答不了；必须在真实库跑附录 A |
| 处置口径 | 「保留并标记」「人工确认后解绑」「加约束但豁免历史行」是三种不同的产品决策，代码不能替人决定 |
| 若要做方案 C：历史行的处理 | 复合 FK 建约束时会**校验全表**，存在跨工程行则迁移失败。必须先把这些行处理掉（解绑并留痕）或明确放弃约束 |
| 若要做方案 D：回滚与双写窗口 | 数据搬迁不可逆，需要明确「双写多久、怎么判定可以停」 |
| RLS 换连接角色的顺序 | 先补 `ListXxxByIDs` 的 scope，再换角色；顺序反了会让校验静默退化成「不存在」（见 §2.5）。**商品域这五个方法已由 DB-05 补上**，全库仍有其它路径未接（换角色前仍需按 DB-009 逐模块确认） |

### 5.3 明确不做

| 不做 | 理由 |
|---|---|
| 把 Document / manifest / 快照类 JSONB 改成关系表 | 报告已排除；`209_blocks_id_uuid.sql:21-25` 列出的 10 个存储点说明这种改造的爆炸半径 |
| 为 `related_ids` 建 junction 表 | 当前没有任何反查消费者（`fillRelated` 只解析分类/品牌/标签，`product_resp.go:227-264`），建关联表只会让写变成 1+N |
| 把 `product_tags.rule_params` / `product_price_adjustments.filter` 当关系列处理 | 前者是标量规则参数；后者是 append-only 留痕快照 |
| 统一把 JSONB 关系列改成关系表 | 关系基数是 1:N 低基数、写侧整列替换、读侧内联解析 —— 三个特征都指向「留 JSONB」 |
| 自动清理 / 自动删除任何存量 | 报告验收原文：禁止自动删数据 |
| 借本票去动 RLS 换角色 | 超出本票范围，且顺序风险独立（属 DB-009 自己的票） |

---

## 6. 未验证 / 不确定的部分

1. **真实库的存量数据质量未验证**：本机 `wp` 库只有 1 个工程 / 2 个商品，§3.1 的 13 项全 0
   **不构成「无问题」的结论**。必须换到有真实数据的库上跑附录 A。
2. **真实负载下的收益未验证**：§3.5 是合成微基准（同表结构、无其余列、无业务索引、无并发）。
   生产上 `products` 的读路径会走 `idx_products_project_status_sort`
   （`167_index_foundation.sql:33`），本基准用的是单独的 `(project_id)` 索引，计划可能不同。
3. **`related_ids` 是否有产品侧的反查需求未确认**：如果前台有「相关商品」推荐位，
   就说明它需要索引与反向查询；本次源码盘点没有找到任何反查调用点，但**产品意图要人来确认**。
4. **跨工程存量到底怎么产生的未确认**：只能证明「校验上线后 API 路径写不进去」，
   不能证明历史上没有过不带校验的写入窗口（`related_ids` 至今仍可写任意 UUID）。
5. **复合 FK 对删除路径的实际开销未测量**：只验证了正确性与语法（§3.4），
   没有测「批量删除被引用实体」时的额外索引查找成本。
6. ~~**RLS 换角色后的行为推演未经实测**~~ **（DB-05 已实测，结论不完全成立）**：
   §2.5 关于「未包 scope 的 `ListXxxByIDs` 会返回 0 行」的推演，在**有策略的表**上成立
   （实测把 `ListCategoriesByIDs` 退回裸句柄形态 ⇒ 非超级角色下 0 行 ⇒ 新护栏转红），
   但在 `ListVariantsByIDs` 上**不成立** —— `product_variants` 没有策略，
   换角色后它仍然读得到跨工程的变体（风险方向与推演相反）。两处都由
   `public/test/rls/nonsuperuser/` 的断言固定下来。
7. **本评估没有覆盖库存域 / 内容域的同类问题**：§2.3 已看到 `inventory_stocks.product_id`、
   `inventory_purchase_order_lines.product_id/variant_id`、
   `inventory_bom_items.parent_variant_id/component_variant_id` 同属「child 与 parent 都有
   project_id 但没有复合唯一约束」，本次只做了枚举，没有逐条评估。

---

## 附录 A · 只读审计 SQL（可直接贴进 psql）

> 全部为 SELECT，不含任何写语句。查出的每一行都是「需要人工判断」的候选，
> **不要用它们自动清理**。`e.id ~ '^[0-9a-fA-F-]{36}$'` 是给脏数据留的守卫，
> 避免非 uuid 文本让整个查询报 22P02。

    -- Q1 孤儿：products.category_ids
    SELECT p.project_id, p.id AS product_id, e.id AS dangling_category_id
    FROM products p, jsonb_array_elements_text(p.category_ids) e(id)
    WHERE e.id ~ '^[0-9a-fA-F-]{36}$'
      AND NOT EXISTS (SELECT 1 FROM product_categories c WHERE c.id = e.id::uuid);

    -- Q2 跨工程：products.category_ids
    SELECT p.project_id AS product_project, c.project_id AS category_project, p.id, c.id
    FROM products p, jsonb_array_elements_text(p.category_ids) e(id)
    JOIN product_categories c ON c.id = e.id::uuid
    WHERE e.id ~ '^[0-9a-fA-F-]{36}$' AND c.project_id <> p.project_id;

    -- Q3 / Q4 孤儿与跨工程：products.tag_ids（目标表 product_tags）
    SELECT p.project_id, p.id, e.id AS dangling_tag_id
    FROM products p, jsonb_array_elements_text(p.tag_ids) e(id)
    WHERE e.id ~ '^[0-9a-fA-F-]{36}$'
      AND NOT EXISTS (SELECT 1 FROM product_tags t WHERE t.id = e.id::uuid);

    SELECT p.project_id AS product_project, t.project_id AS tag_project, p.id, t.id
    FROM products p, jsonb_array_elements_text(p.tag_ids) e(id)
    JOIN product_tags t ON t.id = e.id::uuid
    WHERE e.id ~ '^[0-9a-fA-F-]{36}$' AND t.project_id <> p.project_id;

    -- Q5 孤儿：products.attribute_ids（目标表 product_attributes）
    SELECT p.project_id, p.id, e.id AS dangling_attribute_id
    FROM products p, jsonb_array_elements_text(p.attribute_ids) e(id)
    WHERE e.id ~ '^[0-9a-fA-F-]{36}$'
      AND NOT EXISTS (SELECT 1 FROM product_attributes a WHERE a.id = e.id::uuid);

    -- Q6 / Q7 孤儿与跨工程：products.related_ids
    SELECT p.project_id, p.id, e.id AS dangling_related_id
    FROM products p, jsonb_array_elements_text(p.related_ids) e(id)
    WHERE e.id ~ '^[0-9a-fA-F-]{36}$'
      AND NOT EXISTS (SELECT 1 FROM products q WHERE q.id = e.id::uuid);

    SELECT p.project_id AS product_project, q.project_id AS related_project, p.id, q.id
    FROM products p, jsonb_array_elements_text(p.related_ids) e(id)
    JOIN products q ON q.id = e.id::uuid
    WHERE e.id ~ '^[0-9a-fA-F-]{36}$' AND q.project_id <> p.project_id;

    -- Q8 服务层不变量：主分类必须同时出现在 category_ids 里
    SELECT p.project_id, p.id, p.primary_category_id
    FROM products p
    WHERE p.primary_category_id IS NOT NULL
      AND NOT (p.category_ids @> jsonb_build_array(p.primary_category_id::text));

    -- Q9 跨工程：products.brand_id（外键只保证存在性）
    SELECT p.project_id AS product_project, b.project_id AS brand_project, p.id, b.id
    FROM products p JOIN product_brands b ON b.id = p.brand_id
    WHERE b.project_id <> p.project_id;

    -- Q10 跨工程：product_categories.parent_id
    SELECT c.project_id AS child_project, pc.project_id AS parent_project, c.id, pc.id
    FROM product_categories c JOIN product_categories pc ON pc.id = c.parent_id
    WHERE pc.project_id <> c.project_id;

    -- Q11 / Q12 孤儿与跨工程：products.bundle_items.options[].variantId
    SELECT p.project_id, p.id, o->>'variantId' AS dangling_variant_id
    FROM products p, jsonb_array_elements(p.bundle_items->'options') o
    WHERE NOT EXISTS (SELECT 1 FROM product_variants v WHERE v.id = (o->>'variantId')::uuid);

    SELECT p.project_id AS container_project, pp.project_id AS member_project, p.id, v.id
    FROM products p, jsonb_array_elements(p.bundle_items->'options') o
    JOIN product_variants v ON v.id = (o->>'variantId')::uuid
    JOIN products pp ON pp.id = v.product_id
    WHERE pp.project_id <> p.project_id;

    -- Q13 跨工程标量 FK：库存行与商品不同工程
    SELECT s.project_id AS stock_project, p.project_id AS product_project, s.id
    FROM inventory_stocks s JOIN products p ON p.id = s.product_id
    WHERE p.project_id <> s.project_id;

## 附录 B · 复现步骤（含夹具库）

    # 1) 开发库只读审计（不动数据）
    psql -h 127.0.0.1 -p 5432 -U root -d wp -f db03_audit.sql

    # 2) 判别力验证：用真实 DDL 建临时库（不触碰 wp 的数据）
    psql -h 127.0.0.1 -p 5432 -U root -d postgres -c "CREATE DATABASE db03_eval_tmp;"
    pg_dump -h 127.0.0.1 -p 5432 -U root -d wp --schema-only --no-owner --no-privileges \
      -t public.projects -t public.products -t public.product_categories \
      -t public.product_brands -t public.product_tags -t public.product_attributes \
      -t public.product_variants -t public.inventory_stocks -t public.inventory_bom_items \
      -t public.inventory_warehouses | psql -h 127.0.0.1 -p 5432 -U root -d db03_eval_tmp
    #   注意：products.name 的 trgm 索引依赖 ext_shared schema，缺它会报
    #   "schema ext_shared does not exist" —— 与本次评估无关，忽略即可。
    psql -h 127.0.0.1 -p 5432 -U root -d db03_eval_tmp -f db03_fixture.sql
    psql -h 127.0.0.1 -p 5432 -U root -d db03_eval_tmp -f db03_audit.sql    # 13 项应全部命中
    psql -h 127.0.0.1 -p 5432 -U root -d db03_eval_tmp -f db03_guard2.sql   # 守卫盲区复现

    # 3) 收尾：丢弃临时库（评估结束后已执行）
    psql -h 127.0.0.1 -p 5432 -U root -d postgres -c "DROP DATABASE IF EXISTS db03_eval_tmp;"

---

## 附录 C · 夹具脚本（§3.2 / §3.4 用；只对临时库执行）

```sql
-- DB-03 评估夹具（只写入临时库 db03_eval_tmp，绝不触碰开发库 wp）
\set ON_ERROR_STOP on
TRUNCATE products, product_variants, product_categories, product_brands, product_tags,
         product_attributes, inventory_stocks, inventory_bom_items, inventory_warehouses, projects CASCADE;

INSERT INTO projects (id, name, settings, create_time, update_time) VALUES
 ('11111111-1111-4111-8111-111111111111','工程A','{}',now(),now()),
 ('22222222-2222-4222-8222-222222222222','工程B','{}',now(),now());

INSERT INTO product_brands (id, project_id, name, slug) VALUES
 ('a0000000-0000-4000-8000-0000000000b1','11111111-1111-4111-8111-111111111111','A-品牌','a-brand'),
 ('22222222-2222-4222-8222-0000000000b2','22222222-2222-4222-8222-222222222222','B-品牌','b-brand');
INSERT INTO product_tags (id, project_id, name, slug, kind) VALUES
 ('a0000000-0000-4000-8000-0000000000c1','11111111-1111-4111-8111-111111111111','A-标签','a-tag','manual'),
 ('22222222-2222-4222-8222-0000000000c2','22222222-2222-4222-8222-222222222222','B-标签','b-tag','manual');
INSERT INTO product_attributes (id, project_id, key, name, values) VALUES
 ('a0000000-0000-4000-8000-0000000000d1','11111111-1111-4111-8111-111111111111','size','A-属性','[]');

-- 分类：先插 B 的根，再插 A 的根，最后插 A 的子级（parent 指向 B 的根 = 跨工程父子）
INSERT INTO product_categories (id, project_id, parent_id, name, slug) VALUES
 ('22222222-2222-4222-8222-000000000002','22222222-2222-4222-8222-222222222222',NULL,'B-根','b-root');
INSERT INTO product_categories (id, project_id, parent_id, name, slug) VALUES
 ('a0000000-0000-4000-8000-000000000001','11111111-1111-4111-8111-111111111111',NULL,'A-根','a-root');
INSERT INTO product_categories (id, project_id, parent_id, name, slug) VALUES
 ('a0000000-0000-4000-8000-000000000002','11111111-1111-4111-8111-111111111111','22222222-2222-4222-8222-000000000002','A-子(父在B工程)','a-child');

INSERT INTO products (id, project_id, name, slug, category_ids, tag_ids, attribute_ids, related_ids, brand_id, primary_category_id, bundle_items) VALUES
 ('a0000000-0000-4000-8000-0000000000e1','11111111-1111-4111-8111-111111111111','A-商品1','a-p1',
  '["a0000000-0000-4000-8000-000000000001","deadbeef-0000-4000-8000-000000000000","22222222-2222-4222-8222-000000000002"]',
  '["a0000000-0000-4000-8000-0000000000c1","deadbeef-0000-4000-8000-000000000001","22222222-2222-4222-8222-0000000000c2"]',
  '["deadbeef-0000-4000-8000-000000000002"]',
  '["deadbeef-0000-4000-8000-000000000003"]',
  '22222222-2222-4222-8222-0000000000b2',
  'a0000000-0000-4000-8000-000000000001',
  '{"maxOptions":20,"minTotalQty":0,"maxTotalQty":0,"options":[{"variantId":"deadbeef-0000-4000-8000-000000000004","required":true,"defaultQty":1,"minQty":1,"maxQty":0}]}'),
 ('a0000000-0000-4000-8000-0000000000e2','11111111-1111-4111-8111-111111111111','A-商品2','a-p2',
  '["a0000000-0000-4000-8000-000000000001"]','[]','[]','[]',NULL,NULL,'{}');

-- B 的商品引用 A 的分类（模拟历史存量 / 非 API 写入：当前 service 校验会拒绝跨工程）
INSERT INTO products (id, project_id, name, slug, category_ids, tag_ids, attribute_ids, related_ids, bundle_items) VALUES
 ('22222222-2222-4222-8222-0000000000e3','22222222-2222-4222-8222-222222222222','B-商品1','b-p1',
  '["a0000000-0000-4000-8000-000000000001"]','[]','[]','["a0000000-0000-4000-8000-0000000000e1"]','{}');

INSERT INTO product_variants (id, product_id, sku_code, price, option_values) VALUES
 ('a0000000-0000-4000-8000-0000000000f1','a0000000-0000-4000-8000-0000000000e1','A-SKU-1',10,'{}'),
 ('a0000000-0000-4000-8000-0000000000f2','a0000000-0000-4000-8000-0000000000e2','A-SKU-2',20,'{}'),
 ('22222222-2222-4222-8222-0000000000f3','22222222-2222-4222-8222-0000000000e3','B-SKU-1',30,'{}');

-- A-商品1 的 bundle 同时指向 B 的变体（跨工程）与一个不存在的变体（孤儿）
UPDATE products SET bundle_items = jsonb_build_object('maxOptions',20,'minTotalQty',0,'maxTotalQty',0,
  'options', jsonb_build_array(
     jsonb_build_object('variantId','22222222-2222-4222-8222-0000000000f3','required',true,'defaultQty',1,'minQty',1,'maxQty',0),
     jsonb_build_object('variantId','deadbeef-0000-4000-8000-000000000004','required',false,'defaultQty',1,'minQty',1,'maxQty',0)))
 WHERE id = 'a0000000-0000-4000-8000-0000000000e1';

INSERT INTO inventory_warehouses (id, project_id, code, name) VALUES
 ('44444444-4444-4444-8444-000000000001','22222222-2222-4222-8222-222222222222','WH-B','B仓');
INSERT INTO inventory_stocks (id, project_id, warehouse_id, product_id, variant_id, sku_code, quantity, track_quantity) VALUES
 ('33333333-3333-4333-8333-000000000001','22222222-2222-4222-8222-222222222222','44444444-4444-4444-8444-000000000001','a0000000-0000-4000-8000-0000000000e1','a0000000-0000-4000-8000-0000000000f1','A-SKU-1',5,true);

-- 守卫盲区用：A 工程的分类 a…0003，只被 B 工程的商品引用
INSERT INTO product_categories (id, project_id, parent_id, name, slug) VALUES
 ('a0000000-0000-4000-8000-000000000003','11111111-1111-4111-8111-111111111111',NULL,'A-仅被B引用','a-only-b');
UPDATE products SET category_ids = category_ids || '["a0000000-0000-4000-8000-000000000003"]'::jsonb
 WHERE id = '22222222-2222-4222-8222-0000000000e3';

-- 不变量破坏：primary_category_id 不在 category_ids 里
UPDATE products SET primary_category_id = 'a0000000-0000-4000-8000-000000000001',
                    category_ids = '[]'::jsonb
 WHERE id = 'a0000000-0000-4000-8000-0000000000e2';
\echo 夹具写入完成
```

守卫盲区复现脚本（`db03_guard2.sql`）：

```sql
-- 守卫盲区复现（只读 + 仅对临时库的一次 DELETE）
SELECT '守卫（project_id = A，ProductUsingCategory 谓词）' AS view, count(*) AS hits
  FROM products
 WHERE project_id = '11111111-1111-4111-8111-111111111111'
   AND (category_ids @> '["a0000000-0000-4000-8000-000000000003"]'::jsonb
        OR primary_category_id = 'a0000000-0000-4000-8000-000000000003');
SELECT '全局事实（同谓词，无工程过滤）' AS view, count(*) AS hits
  FROM products
 WHERE category_ids @> '["a0000000-0000-4000-8000-000000000003"]'::jsonb
    OR primary_category_id = 'a0000000-0000-4000-8000-000000000003';
DELETE FROM product_categories WHERE id = 'a0000000-0000-4000-8000-000000000003';
SELECT p.project_id AS product_project, p.id AS product_id,
       'a0000000-0000-4000-8000-000000000003' AS dangling_category_id
  FROM products p
 WHERE p.category_ids @> '["a0000000-0000-4000-8000-000000000003"]'::jsonb;
```
