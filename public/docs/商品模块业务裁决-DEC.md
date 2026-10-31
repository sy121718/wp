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
词条 `public/migrations/698_product_stock_unknown_i18n.sql`（zh-CN / en-US）。

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
2. **有状态发布记录**（PRD-011）：跨模块过滤（发布记录 → 商品状态）无法压进一条语句，
   口径明确为：**items 与 total 允许来自不同快照**，但不得出现幻影行；本页因过滤为空时
   返回空 items 与过滤后的 total。该路径的规模问题（PRD-011 全量读取）仍未解决，不因本裁决关闭。
3. 上线索引仍是候选全集（PRD-007 成本主体未变）；本裁决只消除 count/list 之间的不一致，
   不声称深分页成本已优化。

**落地**：`model.ListForCollectionPageWithTotal` + `orderCollectionQuery`（排序只一份）；
`service/product_catalog.go` 改单查询。

**验证**：`product_catalog_page_budget_test.go`（首页 1 次读、越界 2 次、返回总数相等）、
`product_catalog_price_test.go`（同一条语句同时给 total 与本页行）。

## DEC-08 错误键命名与分域

**裁决**：分两类，不合并。

1. **主错误键**（`ErrInvalidParam` / `ErrNotFound` / `ErrSlugTaken` …）保持**短 key**，
   不迁 `product.err.*`。它们是跨模块共享的错误词汇（pkg/facing 与多个模块共用同一批 key，
   迁移等于给每个模块复制一份同义词条，取词链变长、翻译与运营维护成本 × 模块数）。
2. **商品域私有明细键**（`Detail*`）必须 `product.err.*`，带声明处中文兜底与占位符，
   i18n 值由迁移复制历史 key 的全部语言（697b），保留旧 key 一版。
3. 两个域不得互相穿插：主 key 与明细 key 的兜底表必须不相交。

**落地**：`enums/product_err_detail.go`（明细）、`enums/product_err_fallbacks.go`（主键）、
`public/migrations/697b_product_error_detail_keys.sql`。

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
