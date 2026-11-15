# 商品模块业务裁决（DEC-01/02/04/05/06/08、PRD-021）

本文件是任务书第 5 节「待确认的业务决策」的**裁决记录**，逐条给出结论、理由、落地位置与验证方式。
未被本文件覆盖的口径仍按任务书待定，不因本文件被推定。

## DEC-01 公开价格启用范围与划线价归属

**裁决**

1. 公开价格只算**启用**变体（enabled = true）。停用变体的价格不进售价、不进价格区间、不进价格筛选。
2. 售价 = 最便宜启用变体；同价时 id 小者胜（确定性构建要求）。
3. 最高价 = 启用变体的最大价；没有启用变体时三者（售价 / 划线价 / 最高价）都不产出。
4. 划线价 = **胜出的最低价变体自己的** compare_price，不跨变体取最大值。

**理由**：旧实现里目录取「最低价变体自己的划线价」，集合源取「全部变体里最大的划线价」——
后者会把「10 元卖 + 另一个变体 99 元划线」拼成 89% 折，页面上的折扣是假的；同一个商品在
商品卡与目录里还会给出不同划线价。停用规格进入公开价格区间则会让「已下架的高价规格」继续
影响展示。

**落地**：`internal/module/product/service/product_variant_price.go`（删 MaxCompare，
新增 `ComparePrice()`，口径唯一实现）；`product_source.go` 改 `onlyEnabled=true`；
两个消费点共用 `summarizeVariantPrices`。

**验证**：`product_variant_price_test.go`（含「最高价变体的划线价不冒充最低价折扣」）、
`product_catalog_price_test.go`（目录真实读路径：停用 1 元 / 启用 10 元无划线 / 启用 20 元有
999 划线 → 售价 1000 分、无划线价、最高价 2000 分）。

## DEC-02 库存故障上抛还是 unknown

**裁决**：**显式 unknown，不上抛**，且只用于运营读取路径。

- 后台列表 / 详情 / MCP 工具：读取故障时该行标 `StockUnknown`，页面显示「库存读取失败」，
  MCP 文本如实说读取失败；不拿零值冒充「未入库」。
- 公开访问面与构建产物：不使用库存投影。库存数量不是公开字段，产物里也没有它；
  可售量判断一律走库存真源的带行锁路径，不看商品侧投影。
- 单测缺依赖（`invSvc == nil`）与运行时故障必须区分：前者不置 unknown（没有能力，
  不是「读失败」），后者置 unknown。

**落地**：`internal/module/product/service/product.go` 的 `fillProductStock`；
模板 `internal/templates/admin/product/products.html`；`inbound/mcp/product_tools.go`；
词条 `public/schema/tables/sys_i18n.sql` 的 data 段（原 698 迁移，zh-CN / en-US）。

**验证**：`product_stock_failure_test.go`、`product_warehouse_multi_stock_test.go`
（页面故障显示）、MCP `product_tools_test.go`。
装配判据：`StockUnknown` 只由 service 的 `fillProductStock` 产生 —— 测试必须注入
`f.products.SetInventoryService(stockFailurePort{ProductStockPort: f.inventory, err: …})`；
`handle.SetInventoryDeps` 只换页面装配的仓库清单依赖，改它不会触发 unknown。

## DEC-04 关联目标删除策略

**裁决**：**同事务清理**（不改「阻止删除」、不做读取剔除）。

删除商品时，同一事务内把其他商品 `related_ids` 里对它的引用移除；清理本身的失败让整个
删除回滚。被动作废的引用者各记一条 `related_ids` 的 before/after 变更，并发产物失效事件。

**理由**

- 阻止删除：`related_ids` 的引用在后台不可见、也没有逐条摘除入口，阻止等于把「有人顺手
  挂了它」变成运营删不掉的硬墙。
- 读取剔除：脏 id 永远留在库里，编辑页打开时它还在，产物里的相关商品卡指向 404，无自愈时机。
- 清理：把「引用一个被删商品」的唯一正确结果（引用消失）写进删除事务，没有中间态。

**落地**：`internal/module/product/model/product_ref_batch.go` 新增
`ListRelatedReferrersTx`（同事务、按 id 升序 FOR UPDATE、显式工程谓词）与
`UpdateRelatedIDsTx`；`service/product.go` 的 `cleanRelatedRefsTx` 接入单删，
`product_bulk.go` 接入批删。工程级写顾问锁先于行锁，集合内部互引不会形成等待环。

**验证**：`public/test/product/feature/product_related_delete_cleanup_test.go`
（单删 / 批删 / 互引整体成功 / 引用者失效事件）。

## DEC-05 公开分页上限、越界行为与搜索特殊字符

**裁决**

1. 公开目录上限：`Page ≤ 100`、`PageSize ≤ 60`；越界即参数错误（不是静默夹取）。
2. 页码越过总页数：返回**空列表 + 真实总数**，不夹到末页 —— 同一个 URL 不能对应两份内容。
3. 后台列表仍夹到末页（运营翻页体验），与公开面策略不同是刻意的，两处各有测试。
4. 搜索特殊字符：**用户输入按字面量**处理 —— `%`、`_`、`\` 都是普通字符（`ESCAPE '\'`）。
   通配档（`productKeywordPattern`）只服务内部固定模式，两者不得混用。
5. 关键词的两种构造点各有唯一实现：`literalKeywordPattern` / `productKeywordPattern`。
   三处字面量入口的 ESCAPE 写法看起来不同（`'\\'` 与反引号里的 `'\'`），**是同一个数据库字符**，
   不要「统一」。

**落地**：`service/product_catalog.go` 的上限校验与越界返回；`model/product_model.go`
的两个关键字构造点。

**验证**：`product_catalog_bounds_test.go`（上限、边界允许、越界空列表与总数）、
`product_catalog_page_budget_test.go`（越界不夹页）、`product_keyword_pattern_test.go`
（三处字面量入口共用转义）。

## DEC-06 total/items 同快照与跨模块一致性

**裁决**

1. **公开目录**：总数与本页必须同快照 —— 用一条语句完成（`COUNT(*) OVER()`），
   不再「先 count 再 list」。越界页查不到行时窗口计数无值，退回一次 count 只为补「共 N 条」；
   第 1 页空页 = 过滤后 0 条，不再计数。
   **候选资格由 SQL 直接给**（2026-11-01 用户批准跨模块只读关联例外之一）：
   `model/product_public_catalog.go` 的 `publicCatalogQuery` 在 `collectionQuery` 上 join
   一个「一实体一行」的发布子查询（`presentation_instances` + `presentation_publications`，
   条件为同工程、同语言、`artifact_id IS NOT NULL`、`btrim(active_path, 空白集) <> ''`），
   列表与计数共用这一个 builder。同一实体的多个发布实例在子查询内按 `entity_id` 分组去重，
   total 不随发布实例数放大；同一实体有多条合法路径时取 `MIN(active_path)`（旧全集 map 也无顺序保证，
   候选资格不变）。路径按原值随本页返回，不在 Go 侧二次裁剪。
   **语言**：请求未带语言时按 `pipeline.DefaultLocale` 的既有语义取工程默认语言，只用于候选资格；
   真实业务译文仍按请求语言取。
2. **有状态发布记录**（PRD-011）：发布记录页同样用**一条只读语句**给出「精确 total + 本页明细」，
   与本页行出自同一份语句快照（2026-11-01 批准的第二处跨模块只读关联例外，范围只到这一条读查询，
   不推广到别的读路径）—— `model/publish_record_page.go` 只读关联 `presentation_instances` /
   `presentation_publications` / `presentation_artifacts` 三表。
   语句结构固定五段，全部在同一条 SQL 里：
   ①**商品级筛选**（工程 + 关键词 + 状态 `EXISTS` 谓词；状态三档互斥：`active` = 存在
   `active_artifact_id IS NOT NULL` 的实例、`draft` = 存在实例但都不 Online、`none` = 一实例都没有）；
   ②`capped` 求过滤后的 `count(*)`；③`page_no` 把请求页夹到末页（`total = 0` 时页号为 0，
   避免负 OFFSET）；④`page` 按 `(sort ASC, create_time DESC, id ASC)` 取本页；
   ⑤`LEFT JOIN page` 后 `LATERAL` 聚合本页每个商品的实例 × 语言明细，`jsonb_agg` 交业务事实。
   于是**一商品一行**，total 不随实例数 / 语言数放大；页内语言顺序每实例按 `pub.lang ASC` 取、
   行内再稳定排序，明细字段口径沿用原端口的 `PublishRecords`：实例没有发布行时仍占一行、
   语言为空；`Online` 只看 `active_artifact_id` 非空；发布行为空则 `ArtifactHash` 为空、
   发布时间为零值 —— 由 SQL 的 NULL 交 Go 零值，不在 SQL 里造时间。
   **不再是跨批扫描**：没有 keyset 游标、没有逐批 `Langs` 异快照、没有页窗口状态机、
   没有跨连接长事务、不保存已见 id 集合，内存只留本页行。
3. 公开目录不再先取候选全集（PRD-007 的这一部分已由第 1 条的关联查询消除）；
   分页仍走数据库 `LIMIT/OFFSET`，深分页的偏移成本与以前一致，本裁决不声称深分页已优化。
   发布记录侧要给出**精确 total**，仍必须对当前工程与关键词过滤后的**全部商品**计数一次，
   **成本仍是 O(N)**（`capped` 里的 `count(*)`），本裁决不声称它是 O(1)：
   省掉的是「多条语句 + 多次端口往返 + 跨批异快照 + 长事务占连接」，不是计数本身的复杂度。

**落地**：公开目录：`model/product_public_catalog.go` 的 `publicCatalogQuery` /
`ListPublicCatalogPageWithTotal` / `CountPublicCatalog`（共用 builder + `orderCollectionQuery`），
`service/product_catalog.go` 单查询取本页、总数与路径（`SetPublicCatalogIndex` 与全集 map 已删）。
发布记录侧：`model/publish_record_page.go` 的 `QueryPublishRecordPage`（外层 `rls.InProjectScope`、
`publishRecordCandidatesQuery` 交候选、`publishRecordPageSQL` 一条语句）与
`service/product_publish_record_page.go` 的 `QueryPublishRecords`（`publishRowsOf` 折叠行级事实）。
跨批扫描的 `model/publish_record_scan.go` 与 `service/product_publish_record_scan.go` 已删，不保留旧形状。

**验证**（本轮由主代理真实复跑 8 个功能包：63 个顶层 + 56 个子用例 pass、0 skip、0 fail，`go build -o /tmp/wp-followup-verify ./cmd` exit 0，本批路径 `git diff --check` exit 0；功能限定口径，非全包、非全量测试）：
`product_catalog_page_budget_test.go`（首页 1 次读、
越界 2 次、无候选时首页仍 1 次读且 total 为 0、本页不超过页大小）、`product_catalog_price_test.go`
（价格仍取启用变体最低价）、`product_catalog_bounds_test.go`（页上限与缺省页大小）、
`feature/product_catalog_publication_test.go`（无产物 / 全空白路径 / 草稿被排除，
同一实体多发布实例 total 不翻倍）、
`service/product_publish_record_page_sql_count_test.go`（整页只发一条 products 读取语句）、
`service/product_publish_record_page_status_sql_test.go`（状态筛选先于分页、越界夹末页、
无发布行时零值事实与有发布行时真实时刻）、
`feature/product_publish_record_scan_test.go`（精确 total、夹末页、active/draft/none 三档互斥、工程隔离）、
`feature/product_publish_record_page_test.go`（不接发布端口也能读）。

## DEC-08 错误键命名与分域

**裁决**：分两类，不合并。

1. **主错误键**（`ErrInvalidParam` / `ErrNotFound` / `ErrSlugTaken` …）保持**短 key**，
   不迁 `product.err.*`。它们是跨模块共享的错误词汇（pkg/facing 与多个模块共用同一批 key，
   迁移等于给每个模块复制一份同义词条，取词链变长、翻译与运营维护成本 × 模块数）。
2. **商品域私有明细键**（`Detail*`）必须 `product.err.*`，带声明处中文兜底与占位符，
   i18n 值由迁移复制历史 key 的全部语言（697b），保留旧 key 一版。
3. 两个域不得互相穿插：主 key 与明细 key 的兜底表必须不相交。

**落地**：`enums/product_err_detail.go`（明细）、`enums/product_err_fallbacks.go`（主键）、
`public/schema/tables/sys_i18n.sql` 的 data 段（原 697b 迁移）。

**验证**：`TestErrorDetailKeysStayInProductDomain`（Detail 常量域前缀 + 兜底一一对应）、
`TestFallbackTablesAreDisjoint`（两表不相交）、`TestEveryErrorKeyHasFallback`。

## PRD-021 动态词典

**裁决**：**不建**全局注入层与前端语言版本缓存，维持现有链路。

- 现状：界面文案走模板 `t / tf`（请求级语言），`data-i18n` 只携带词条 key，
  由同一请求的语言上下文解析；库存未知这类新文案已按此链路落地。
- 不建的理由：全局 `text.Inject` 命名空间 + 前端小容量缓存在多语言并发下需要额外的
  失效协议（语言切换 / 词条热更新 / 多站点同进程），收益只是省一次模板取词；
  而现有链路已经做到「word source 唯一、请求级解析」。

**触发重建的条件**（写在这里，避免以后重新讨论）：出现「非模板消费方（纯静态 SPA / 独立
前端服务）需要同一批词条」且无法走模板渲染时，再评估按需下发接口，而不是全局注入。

**验证**：无新增代码；以现有模板词条测试与 `check-i18n-*` 门禁为准。
