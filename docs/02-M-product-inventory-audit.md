# 商品与库存域 操作逻辑审计

> 审计对象：`/admin` 下商品域（product）与库存域（product/inventory）全部后台页面。
> 判据来源：`docs/02-J-admin-ui-structure-checklist.md` §A–§H、`docs/02-L-admin-rework-worklist.md`、
> `admin-ui-logic` 技能（信息架构 / 布局效率 / 操作逻辑 / 认知负担）。
> **只读审计**：全程无任何写操作，只发 GET；未提交任何表单、未改数据库、未改代码。
> 运行时验证用 `curl` + 独立 cookie jar（`/tmp/pi-audit`），**未使用** ego 浏览器；登录经
> `GET /admin/dev-login?to=…`（debug 模式）建立独立会话，不触碰其它会话的登录态。
> 证据口径：每条标注「实测」（有 HTTP/DOM 读数）或「静态」（仅读代码/模板）。

---

## 1. 审计范围（页面 / 路由 / handler / 模板）

| # | 页面 | 路由（GET） | handler | 模板 |
|---|---|---|---|---|
| 1 | 商品列表 | `/admin/products` | `product_page_handle.go:86` | `products.html` |
| 2 | 商品新建 | `/admin/products/new` | `product_new_page.go:20` | `products_new.html`（+ `partials/product_create_form.html`） |
| 3 | 商品详情（只读） | `/admin/products/detail` | `product_page_handle.go:368` | `product_detail.html` |
| 4 | 商品编辑 | `/admin/products/edit` | `product_edit_page.go:35` | `product_edit.html` |
| 5 | 商品多语言 | `/admin/products/translations` | `product_translation_page.go:90` | `product_translations.html` |
| 6 | 商品详情模板 | `/admin/products/template` | `product_detail_template_page.go:98` | `product_detail_template.html` |
| 7 | 捆绑配置 | `/admin/products/bundle` | `product_bundle_page.go:28` | `product_bundle.html` |
| 8 | 商品属性 | `/admin/product-attributes` | `product_attribute_page.go:31` | `product_attributes.html` |
| 9 | 商品分类 | `/admin/product-categories` | `product_taxonomy_page.go:25` | `product_categories.html` |
| 10 | 商品品牌 | `/admin/product-brands` | `product_taxonomy_page.go:154` | `product_brands.html` |
| 11 | 商品标签 | `/admin/product-tags` | `product_tag_page.go:48` | `product_tags.html` |
| 12 | 定价工具 | `/admin/product-pricing` | `product_pricing_page.go:55` | `product_pricing.html` |
| 13 | 库存管理 | `/admin/inventory` | `inventory_page_handle.go:77` | `inventory.html` |
| 14 | 仓库管理 | `/admin/inventory/warehouses` | `inventory_page_handle.go:220` | `inventory_warehouses.html` |
| 15 | 变动原因字典 | `/admin/inventory/reasons` | `inventory_page_handle.go:256` | `inventory_reasons.html` |
| 16 | 货源管理 | `/admin/inventory/sources` | `inventory_source_page_handle.go:57` | `inventory_sources.html` |
| 17 | 采购入库 | `/admin/inventory/purchases` | `inventory_purchase_page_handle.go:80` | `inventory_purchases.html` |

路由注册：`inbound/http/product_page_router.go:57-177`、`inventory/inbound/http/inventory_router.go:115-156`。
共享片段：`partials/bulk_bar.html`（批量条）、`partials/pagination.html`、`partials/product_create_form.html`、
`partials/product_attribute_rows.html`、`partials/pricing_rule_fields.html`。

实测抓取（工程 `52935790-…`，商品 `630e11b2-…`）：17 个页面全部 200；空态另用
`?project=00000000-0000-0000-0000-000000000000` 与 `?keyword=zzzznomatch` 两种方式构造。

---

## 2. 确定缺陷

**合计 13 条：P0 × 2、P1 × 6、P2 × 5**（每条均可从代码或渲染产物复现；空态与筛选类结论有 HTTP 实测）。

| # | 严重度 | 页面 | 文件:行 | 问题 | 用户会怎么撞上 | 修复方向 |
|---|---|---|---|---|---|---|
| D1 | **P0** | 10 个列表页 | 见下表 | **空数据 / 筛选无结果时整张表不渲染 → 表头连同列名一起消失** | 筛一个不存在的关键词后页面只剩一句话，看不到有哪些列、也无法确认自己是不是筛错了列 | `<table>` 移出 `{{if len(...) == 0}}` 的 `{{else}}` 分支，空态进 `tbody` 的 `colspan` 行（照抄管理域批 D 已修的 `permissions.html`） |
| D2 | **P0** | 商品详情模板 | `product_detail_template_page.go:120-122` | **菜单死链**：侧栏菜单项 `href="/admin/products/template"` 不带 `product`，handler 静默 `302` 回商品列表且**不带 `?err=`** | 点「内容 › 商品详情模板」→ 页面闪回商品列表，无任何提示，用户以为菜单坏了 | 菜单改指 `/admin/products`，或在缺参分支加 `?err=请先从商品列表选择商品，再配置它的详情页模板`（同型已修样板：mail 域 `/admin/mail/campaign`，`02-L` §0.5.1 S-2） |
| D3 | P1 | 库存/属性列表（4 页） | `product_attribute_page.go:44`、`inventory_page_handle.go:51/691`、`inventory_source_page_handle.go:42/87`、`inventory_purchase_page_handle.go:42` + `inventory_purchase_page_view.go:31` | **硬编码上限 + 无分页**：属性组 `Size:200`(maxPageSize=200)、库存流水 50 条、货源 200、采购单 100；四页均无分页条（实测 `class="pagination"` 计数 = 0），也没有「共 N 条」 | 流水按时间倒序，第 51 条之后的老记录**永远看不到**；第 201 个属性组静默消失，页面不提示还有更多 | 接 `partials/pagination.html`（样板 `product_page_handle.go:213` 的 `shell.BuildPagination`，基址带当前筛选） |
| D4 | P1 | 商品新建（整页） | `products_new.html:8-12` + `partials/product_create_form.html:144-147` + `product_page_handle.go:695` | **长表单提交失败回列表页 → 15 个字段 + 属性组多选 + 多仓多选全部丢失**（抽屉才渲染「取消」，整页不设 `InDrawer`） | 填完名称/SKU 来源/属性组/多仓/数量后保存失败（如捆绑价缺失、SKU 重复），页面跳回列表，全部输入蒸发，只能重填 | 失败留在原页并回填表单（或改 `hx-post` + 片段错误槽），错误逐字段就近提示 |
| D5 | P1 | 8 组抽屉/行内表单 | `product_attribute_page.go:220/239/253`（属性组/属性值）、`product_taxonomy_page.go:80/104/114`（分类）、`:195/218/228`（品牌）、`product_tag_page.go:172/194/204`（标签）、`inventory_page_handle.go:287/307/544/565`（仓库/原因）、`inventory_source_page_handle.go:127/153`、`inventory_purchase_page_handle.go:135+` | **写失败一律 `302` 回列表 + `?err=`，抽屉里已填内容全丢**（模板是原生 `<form method="post">`，无 `hx-post`，无输入回填位） | 属性值抽屉填了 8 行值、名称已改，保存被拒（重名 / 被引用）→ 回到列表只剩一句错误，8 行值要重敲 | 抽屉表单改 htmx 局部提交（错误在抽屉内呈现且输入保留），或失败回跳时用服务端回填的表单值重开抽屉 |
| D6 | P1 | 商品列表 | `products.html:36` vs `product_detail.html:6-15`、`product_page_handle.go:354` | **悬浮说明与功能实际位置矛盾**：列表说明写「变体与评分属于单个商品，在商品的**详情**里维护（点名称进入）」，而详情页明确写着「本页是**只读**的……改动都在『编辑』页」 | 用户照说明点商品名进详情想加变体 → 发现一个字都改不了 → 退回列表再找「编辑」，白走两步 | 把该段文案改成「变体 / 评分 / 属性 / 分类品牌 / 标签在**编辑**页维护」，或让详情页给一个「去编辑」按钮（现在刻意不给） |
| D7 | P1 | 商品新建 | `products_new.html:8-12`、`partials/product_create_form.html:145` | **整页新建没有返回 / 取消入口**：页头无 `.page-actions`（实测 `page-actions=0`、无 `href="/admin/products"`），片段只在 `InDrawer` 时才渲染「取消」 | 打开「新建商品」想放弃 → 页面上找不到任何出口，只能点侧栏或按浏览器后退 | 页头 `.page-actions` 加「← 返回商品列表」（样板：`product_edit_page.go:55` 的 `BackURL`） |
| D8 | P1 | 库存管理空态 | `inventory.html:200-204` | **空态承诺的入口不可点 / 位置说错**：文案「入库请从**采购入库页**收货，盘点/报损请走**右上角的库存调整**」，但「采购入库页」是纯文本（同页其它地方都有 `<a>`），而「库存调整」按钮**就在这个空态里**，不在右上角 | 新用户照文案去找「采购入库页」，既点不动也不知道在哪；再按「右上角」找按钮，位置对不上（按钮在空态里） | 「采购入库页」加锚点指向 `/admin/inventory/purchases`；位置描述改为与实际一致（或把按钮保留在空态并删掉「右上角」） |
| D9 | P2 | 货源管理空态 | `inventory_sources.html:105-110` | 空态文案「没有符合条件的货源。**用上面的表单建一个**」—— 上面的表单是**筛选栏**，新建走页头抽屉（`:32` 的 `page-actions`）；且「工程没有货源」与「筛选无结果」共用同一句话 | 用户照着「用上面的表单」在筛选框里敲货源名，越敲越没有结果 | 分档文案（照 `products.html:95-111` 的 filtered / 非 filtered 双档）+ 空态给 `.empty-actions` 指向新建抽屉 |
| D10 | P2 | 库存域 4 处空态 | `inventory_warehouses.html:58-63`、`inventory_reasons.html:49-54`、`inventory_sources.html:105-110`、`inventory_purchases.html:120` | **空态三段式不齐**（只有 title + desc，无 `.empty-actions`），与商品域 5 页（都带 `.empty-actions`）不一致 | 空数据时用户只能自己去页头找新建按钮，空态不说话 | 补 `.empty-actions`（样板 `products.html:100-110`） |
| D11 | P2 | 商品新建 + 商品列表抽屉 | `partials/product_create_form.html:5` | **字面 `\n` 泄漏到页面**：注释闭合 `*}` 之后紧跟字面反斜杠 n（注释内的换行有多处也是字面 `\n`），实测渲染输出在 `<form action="/admin/products/create">` 之前多出一个 `\n` 文本节点 | 「新建商品」页表单上方多出一小段 `\n` 字样（抽屉形态时打开抽屉同样可见） | 把该片段注释里的字面 `\n` 换成真实换行 |
| D12 | P2 | 商品域 4 个列表页 | `product_attributes.html`、`product_categories.html`、`product_brands.html`、`product_tags.html`（`filter-bar` 计数 = 0；handler `product_attribute_page.go:38`、`product_taxonomy_page.go:32/161`、`product_tag_page.go:55` 只读 `project`） | **无筛选栏、无搜索**：这四页没有任何关键词入口；对照库存域 5 页全部有 `.filter-bar` 且服务端筛选实测生效 | 属性组 / 分类 / 品牌 / 标签变多后只能肉眼看 + 浏览器 Ctrl+F | 抄 `inventory_sources.html:57-103` 的筛选栏；service 侧 `ListAttributes/ListCategories/ListBrands/ListTags` 已支持 `Keyword`（`product_category.go:231`、`product_brand.go:155`、`product_tag.go:281`），只差 handler 读 query + 回显 |
| D13 | P2 | 分类 / 品牌 / 标签列表 | `product_taxonomy_page.go:295-313`、`product_tag_page.go:277` → `service/product_tag.go:281` | **全量渲染、无分页**（查询不带 Size，service 也不分页）：不是静默截断，但页高随数据量无上限，且与 D3 的截断口径不一致 | 数据上千时页面越来越长（标签页还要额外批量聚合计数） | 与其他列表统一分页口径，或明确写「本页最多 N 条」并给出计数 |

### D1 位置清单（10 处，全部实测「空态 + `thead` 缺失」）

| 页面 | 模板行（`if len==0` / `else` / `<table>`） | 实测 |
|---|---|---|
| 商品列表 | `products.html:93` / `:113` / `:139` | `?keyword=zzzznomatch` → `thead=0`、`empty-state=1` |
| 商品属性 | `product_attributes.html:55` / `:65` / `:77` | 不存在工程 → `thead=0`、`empty=1` |
| 商品分类 | `product_categories.html:61` / `:71` / `:83` | 同上 |
| 商品品牌 | `product_brands.html:59` / `:69` / `:81` | 同上 |
| 商品标签 | `product_tags.html:69` / `:79` / `:92` | 同上（空态 + 规则表 `thead=1`，主表头仍缺） |
| 商品详情模板 | `product_detail_template.html:138` / `:148` / `:150` | 静态 |
| 库存管理（流水区） | `inventory.html:198` / `:206` / `:208` | 真实工程无流水 → `thead=0`、`empty=1` |
| 仓库管理 | `inventory_warehouses.html:58` / `:63` / `:76` | 不存在工程 → `thead=0`、`empty=1` |
| 货源管理 | `inventory_sources.html:105` / `:110` / `:123` | 真实工程无货源 → `thead=0`、`empty=1` |
| 采购入库 | `inventory_purchases.html:109` / `:120` / `:122` | 无货源 / 筛选无结果 → `thead=0` |

> 与 `02-L` §P0-3 同型（该项目把「`<table>` 被包进 `{{else}}`」定为 P0），管理域四页已修，本域**未修**。

---

## 3. 疑点（需人工确认）

| # | 位置 | 观察 | 为什么要人工确认 |
|---|---|---|---|
| Q1 | `/admin/products/translations`（无 `product` 参数） | 实测 200 且渲染**整工程 120 组表格**（376 KB、`thead=120`、无分页/无折叠） | 设计上任一入口都带 `product`（编辑页 `product_edit.html:36`）；无参路径只有手改 URL 才会撞上。是否要限制为「先选商品」需要产品决策 |
| Q2 | `product_pricing.html:77/84` | `<span class="hint">范围</span>` / `<span class="hint">筛选集</span>` 当字段标签用（`02-L` P1-21 已记） | 可能刻意的紧凑分组；是否改用 `.form-label` 属视觉/语义取舍 |
| Q3 | `inventory_reasons.html:49-54` | 空态「这个工程还没有任何变动原因」**不可达** —— 内置原因是全局行（`project_id IS NULL`），任何工程都有（实测空工程 `thead=1`、`empty=0`） | 属死分支而非用户缺陷；要不要保留待定 |
| Q4 | `/admin/inventory/purchases`（无货源） | 实测 HTML **215 KB**（同域其它页 26–58 KB），未在本次审计中定位体积来源 | 体积是否来自 create / 生产入库抽屉的重复内联，需要单独看渲染差异 |
| Q5 | `/admin/products` 分页链接 | 链接带 `&limit=20`，但 handler 不读 `limit`（`product_page_handle.go:103` 写死 `productListPageSize`） | 参数冗余、当前无功能影响；是否要与分页组件口径对齐？ |
| Q6 | `product_tags.html:150-158` | 「命中的商品」区在未展开时是一张只有一行 `.hint` 的空白卡（`02-L` P1-7 记录「首屏空壳卡」） | 现在已有引导语，比记录时好；是否够用取决于产品判断 |

---

## 4. 已验证为「无问题」的项（避免后续重复审）

1. **内部错误直出（判据 7）**：`bash scripts/check-no-internal-error-leak.sh` → `✓ 未发现直出内部错误的调用点（已扫描 24 个 inbound/http 目录）`，豁免清单为空（实测）。
   - 商品域：`product_page_handle.go:1216-1247`（`productInternalText` → 只给 `shell.MsgInternalError` 归口文案 + 结构化日志）、读侧 `product_err.go`（`?err=` 两层白名单，未命中落归口）；
   - 库存域：`inventory_page_handle.go:926-985`（`inventoryErrText`）、`:1034`（`shell.FacingQueryText`）。
   - 三形态（响应写入 / 重定向 query / 模板数据）均被门禁覆盖且全绿。
2. **破坏性动作的视觉权重与确认**：主行动全是「＋ 新建 X」`btn-primary`（`products.html:52`），删除一律 `btn-danger` 且在 `.col-actions` 最右（`products.html:222`）；`product_pricing.html:100-103` 的「应用调价（落库）」刻意用 `.btn`（非 primary）+ `data-confirm`（实测页面确认属性存在）。
3. **批量操作**：`partials/bulk_bar.html` 提供「已选 {n} 项」计数 + `data-confirm` + `data-confirm-danger`，七个页面共用（products / attributes / categories / brands / tags / warehouses / sources，实测模板与渲染均有）；批量删除逐条执行、单条失败不整批回滚，结论按「已删 N / 跳过 M」回带（`product_attribute_page.go:281-300`）。
4. **全选作用域（H3）**：`data-check-all` 与 `data-check-item` 同处一个 `<form>`（`products.html:119` 的 form 包住整张表）→ `admin.js` 的 `closest('form')` 作用域成立。
5. **「客户端过滤 + 全选」两方向（H8）不适用**：商品 / 库存域列表的筛选全部是**服务端筛选**（整页重载，`product_page_handle.go:100-102`、`inventory_page_handle.go:174-180`），不存在「筛选后隐藏行仍被勾选」的场景。
6. **服务端筛选非死控件（H9）**，实测：
   - `/admin/inventory/sources?keyword=zzzznomatch` → 命中行 0、输入框回显 `value="zzzznomatch"`；
   - `/admin/inventory/purchases?keyword=zzzznomatch` → 同上；
   - `/admin/products?keyword=zzzznomatch` → 0 行 + 回显；`keyword/status` 同时进 `List` 与 `CountProducts`（`:135-148`），分页基址保留筛选（`productListFilterURL`）。
7. **缺参页面的降级**：`/admin/products/detail`、`/admin/products/edit`（无 `product`）→ 渲染「商品不存在」空态 + 返回列表（实测 200）；`/admin/products/bundle`（无 `product`）→ 带 `productBundleNoProductText` 提示。（`/admin/products/template` 的例外见 D2。）
8. **批量 id 入口**：全部经 `shell.BulkIDs`（去空白 / 去重 / 上限整批拒绝，`product_attribute_page.go:283-288`），无 `c.PostFormArray("ids")` 直取。
9. **CSRF**：抽屉与整页表单都带 `csrf_token` 隐藏域（实测 `/admin/products/new` 输出含 64 位 token；`products.html:120/237/243`）。H12 提醒的「模板未展开读不到 token」不适用于本域页面。
10. **详情页职责**：`product_detail.html` 只读且写动作全在编辑页 —— 职责划分本身清楚（仅文案对不上，见 D6）。
11. **标签命中数按需加载**：`product_tags.html` 走 `hx-get /admin/product-tags/hits` 片段，首屏 SQL 与标签数无关（`product_tag_page.go:105-108`），符合 PERF-02 的收口方向。
12. **商品列表的列表形态**：表格 + 末列 `.col-actions` + 表格外 `.hidden-form` + `form="<id>"` 关联（`products.html:236-250`），无 `td form` 嵌套、无每行模板；库存区用 `<details>` 展开分仓（触屏/键盘均可开，`:178-202`）。

---

## 5. 建议修复批次

| 批 | 内容 | 涉及文件 | 并行性 |
|---|---|---|---|
| **批 1（低成本高收益，可立即做）** | D2 菜单死链补 `?err=` 或改指向；D7 新建页加返回按钮；D8 库存空态文案加链接并去掉错的「右上角」 | `product_detail_template_page.go`、`products_new.html`、`inventory.html` | 三处互不冲突，**可并行** |
| **批 2（空态收口，机械但面广）** | D1 十处 `<table>` 移出 `{{else}}` + 空态行 `colspan`；D10 补 `.empty-actions`；D9 空态文案分档 | `products/attributes/categories/brands/tags/detail_template/inventory/warehouses/sources/purchases.html` | 与批 1、批 3 并行（模板文件互不重叠） |
| **批 3（列表可用性统一）** | D3 四页接分页（属性组 / 流水 / 货源 / 采购）；D12 四页补筛选栏（service 已支持 `Keyword`）；D13 分类品牌标签分页口径统一 | Go：`product_attribute_page.go`、`inventory_page_handle.go`、`inventory_source_page_handle.go`、`inventory_purchase_page_*`、`product_taxonomy_page.go`、`product_tag_page.go` + 对应模板 | Go 侧与批 2 的模板改动**需串行**（同一批模板文件），与本域其它页面可并行 |
| **批 4（写失败不丢输入，成本最高）** | D4 + D5：抽屉/整页表单失败改 htmx 局部提交或服务端回填 | `partials/product_create_form.html` + 8 组抽屉表单 + 各 `*_page.go` 失败出口 | 建议按「商品 → 分类/品牌/标签 → 库存/货源/采购」三段串行，段间可并行 |
| **批 5（打磨）** | D6 文案对齐（列表 help vs 只读详情页）；D11 字面 `\n`；Q1–Q6 逐条确认后处理 | `products.html`、`product_detail.html`、`partials/product_create_form.html` | 批内各项可并行（文件不重叠） |

**最该先修的三条**：D2（一行改动即可恢复菜单可用性）→ D7（新建页唯一出口，两行改动）→ D1（十处机械改动，消除「筛选后误判」这一类最高频的困惑）。

---

## 附：本次审计的验证方法

- 抓取：`curl -c/-b <独立 jar> -o` 17 个页面（HTTP 200 ×17），空态用「不存在的工程 id」与「不可能命中的关键词」两种方式构造；
- 统计：`rg -c '<thead' / 'empty-state' / 'data-check-all' / 'data-bulk-bar' / 'class="pagination"' / 'data-confirm'` 等对渲染产物直接计数；
- 静态：模板 `include/import` 跟随（H2）、`<form>` 边界解析（H1）、handler 失败分支逐条读（H10）、`c.Query` 全量清点（H9）；
- 未做的验证：浏览器级量测（页高 / 首屏行数 / hint 占比）与写路径复现（只读审计约束），相关结论已标注「静态」。
