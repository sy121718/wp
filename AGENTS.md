# AGENTS.md

本文件描述 go_wp 仓库的实际开发约定，是 DSH 会话的最高项目级规则。
兼容说明：旧 `CLAUDE.md` 内容已并入本文；子目录规则见 `internal/module/CLAUDE.md`、`pkg/CLAUDE.md`、`public/CLAUDE.md`、`docs/agents/`。
强制内容：本系统开发阶段，不需要兼容任何老的代码，有问题直接重构

## 语言要求（最高优先级）

- 所有回复、分析、总结、计划、报告一律使用简体中文
- 推理/思考过程也使用简体中文
- 工具输出、代码、上游数据即使是英文，回复仍必须是中文；代码标识符、专有名词、命令保留原文

## 项目概览

go_wp 是 `CMS + Visual Website Builder + Static Publishing Engine`。

控制面（CMS + Builder + Build Worker）把可编辑的 Page Document 和 CMS 内容编译为不可变静态 Artifact；访问面（Static Server / CDN + Runtime Fragment Endpoint）只读取已激活的 HTML/CSS/JS。Go + Jet 只在 Preview/Publish 构建阶段运行，访客请求不执行任何模板或数据库查询。

**技术栈**（当前实际状态，2025-09 核对）：

| 层 | 选型 | 职责 |
|---|---|---|
| Web 框架 | Gin | HTTP 路由、中间件链、请求绑定 |
| 后端与构建器 | Go | CMS、BuildContext、Publish Compiler、版本与发布状态机 |
| 构建期模板 | Jet v6（`github.com/CloudyKit/jet/v6`） | 发布阶段把受限 Fragment 与 BuildContext 渲染为最终 HTML；后台页面 SSR |
| Admin 交互 | HTMX（CDN） | 草稿、预览、构建、发布、回滚请求 |
| 认证 | Session + Cookie（gin-contrib/sessions + Cookie 存储） | 替代旧 JWT 方案，HTMX 请求自动携带 Cookie |
| 鉴权 | Casbin（自研 persist.Adapter） | Enforce(user_id, path, method)；业务 API 已挂载 |
| 公开动态片段 | HTMX + Go Handler | 按 Registry capability 返回受控 HTML Fragment（`runtimefragment` 已落地，挂载 /fragment 类路由） |
| 富文本编辑器 | Trix 2.x（本地 vendor：/static/vendor/trix/） | 文章内容编辑；白名单清洗 + h1 降级 h2 |
| 数据库 | PostgreSQL（主库） | CMS 内容、Page 草稿、Artifact 元数据和依赖索引；MySQL 为历史兼容；SQLite/SQL Server 驱动已移除 |
| 会话存储 | Redis（pkg/cache） | 用户会话、封禁标记、在线心跳（**Critical 组件，配置必须启用**） |
| Artifact 存储 | 本地文件系统 / 对象存储 | 不可变构建文件与内容寻址资源 |
| 访问（公开站点） | Static Server / CDN | 直接提供激活后的 Artifact |

> ~~Vue 3 / vue-pure-admin~~ 已废弃并移除。所有后台界面由 Go 渲染 Jet 模板 + HTMX 片段实现。

## 常用命令

```bash
# 后端
go run cmd/main.go
go build -o app ./cmd        # 生产构建用「包路径」形式，且在 git 工作区内执行（见「组件更新与重建」）
go test ./...
go test -race ./...          # 并发回归
go vet ./...
```


### 组件更新与重建

组件（Go 实现 + `internal/templates/components/*.jet` 模板）编译进二进制，**部署新组件后已发布的
产物仍然是旧组件渲染的字节**。系统不会自动重建，但会在启动时给出准确的影响面：

```text
启动 → builder.RegistryVersion() 与 page_artifacts.registry_version 比对
     → 差异页面标记 stale（只标记、不重建，避免拖住启动链）
     → 日志：检测到组件已更新：相关页面已标记待重建（count / registryVersion）
     → 运维经 page.RebuildStale 重建，或由后续编辑/发布自然覆盖
```

`RegistryVersion` = 构建指纹（`vcs.revision`+`vcs.modified`）+ 组件清单指纹（类型 + Props 的
json/ct 标签结构 + 可翻译白名单）。两者的分辨力互补：

| 构建方式 | vcs.revision | Go 代码改动（BuildView/CompileCSS） | Props/模板改动 |
|---|---|---|---|
| `go build -o app ./cmd`（git 工作区内） | ✅ | ✅ | ✅ |
| `go build -o app cmd/main.go`（单文件） | ✅ | ✅ | ✅ |
| `go run …` / 无 git 环境 | ❌ | ❌ | ✅ |

无 VCS 信息时退化为「组件清单指纹」单独生效：**能发现字段与控件声明变化，发现不了只有 Go 代码
变了的改动**。生产环境请确保二进制带 VCS 信息（在 git 工作区内构建即可，Go 1.18+ 默认嵌入）。

> 坑：不要用 `bi.Main.Path` 之类的构建期变量给指纹兜底 —— 它随构建方式变化（`go run cmd/main.go`
> 是 `command-line-arguments`，包方式是模块路径），会让同一个 commit 因构建命令不同算出不同版本，
> 表现为「每次切换构建方式就误判全站待重建」。

> 产物文件丢失（误删/磁盘损坏）不属于重建范畴：用 `POST /api/page/artifact/rebuild` 按元数据里的
> `source_document` 重建，并以返回的 `hashMatched` 判断是否原样恢复（组件已更新时会为 false）；
> `GET /api/page/publication/audit` 可巡检 active 目录里的悬空链接。

## 架构约束（核心不变量）

以下不变量贯穿全系统，违反任意一条即为设计缺陷。详细论证见 `docs/01-overview.md` 等文档。

1. **控制面与访问面分离**：访客请求不查询数据库、不执行 Jet、不解释 AST。URL → 文件映射由 PublicationStore 文件系统状态决定，不由数据库指针决定。
2. **两条发布路径共享同一管线**：Page（手工）与 PresentationInstance（自动）均已实现，走同一 Publish Compiler → ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃，ContentTemplate 每次构建参与**（0-B/0-A2 不变式，blueprint/content/contenttemplate 已落地）：Blueprint 只初始化 Page Document，后续修改不传播。
4. **Binding 不是 Query DSL**：Document 只保存白名单 FieldBinding / CollectionSource / MediaBinding，不能保存 SQL、过滤表达式或任意 endpoint。
5. **确定性构建**：同一 Page Document + BuildContext + Registry + Compiler 产生相同 Artifact 字节（有 determinism/fuzz 测试背书）。
6. **冻结边界不可越权**：每个模块、组件、协议都有明确的「负责 / 禁止」边界，详见 `docs/01-overview.md` §5 冻结边界速查。

### 控制面与访问面

```text
控制面：Database + CMS + Builder + Build Worker + ArtifactStore
访问面：PublicationStore 激活结果 + Static Server/CDN + Runtime Fragment Endpoint
```

- 普通访客请求不得查询 `pages.active_artifact_id` 后再选择模板
- 数据库指针用于控制、审计和故障恢复；实际 URL 必须由 PublicationStore 映射到已激活的静态文件
- 库存、购物车、登录状态等实时能力优先通过 HTMX Runtime Fragment 提供；只有纯客户端状态才使用 Client Enhancement

### 关键协议辨析

```text
CMS 内容实例      ≠ DocumentSnapshot
Page              ≠ CMS 展示模板
PresentationInstance ≠ Page（前者自动，后者手工）
Blueprint         = Page Document 初始化工具（用完即弃）
ContentTemplate   = PresentationInstance DocumentSnapshot 的版本化结构来源（仅参与构建期）
Blueprint         ≠ 构建期或运行时模板
Page Document     ≠ CMS Content
Artifact          ≠ 可编辑源码
```

## 模块现状（2026-09 核对）

### 已实现模块

| 模块 | 职责 | 不负责 |
|---|---|---|
| `admin` | 管理控制面大模块：管理员、角色、权限点、菜单、部门、数据权限（六领域已合并，同包直调）。对外经 `contract.AuthzContextService` 暴露只读权限上下文查询（IsSuperAdmin/角色码/权限码/路由树），供外部模块与插件消费 | CMS 内容、公开站点用户 |
| `common` | 公共业务入口（当前为验证码：标准库自绘 PNG 图片化，答案绝不下发） | 通用基础设施 |
| `dashboard` | 需要后端逻辑的后台页面入口（仪表盘、可视化工作台 Workbench、媒体库、主题管理） | — |
| `media` | 附件与文件分类（LIKE 通配符转义、软删除过滤） | — |
| `project` | 站点工程、SiteSettings、多主题 Theme（list/activate/delete/settings） | — |
| `page` | 手工 Page 与 Page Document：草稿/构建/发布/回滚/改 URL | — |
| `block` | 复用资产（全局块）：16 种 kind + reuse_mode（global 引用/template 一次性复制）、stale 传播编排、删除引用拦截、CloneAST | — |
| `artifact` | Artifact 元数据与内容对象闭包（不可变写入、同版本重构建原地替换） | — |
| `publication` | URL 占用、激活（两段式回执 pending→committed/rolled_back）、回滚 | — |
| `content` | 固定 CMS 内容 | — |
| `contenttemplate` | PresentationInstance DocumentSnapshot 的版本化结构模板 | — |
| `presentation` | 自动发布实例（依赖 content/contenttemplate 契约，走同一发布管线） | 手工 Page |
| `blueprint` | Page Document 初始化工具（用完即弃） | 构建期/运行时模板 |
| `navigation` | 公开站点菜单（与后台 `menu` 严格隔离） | 管理后台权限菜单 |
| `plugin` | 插件体系（组件注册、能力分层） | — |
| `product` | 商品域（issue #5/#6/#7/#8/#9/#10/#11/#12/#13）：商品与变体（价格在变体上）、商品属性组与属性值（组值合一走 JSONB、`is_variation` 决定是否参与变体、商品侧只存引用故可跨商品复用）、变体组合生成（勾选属性值 → 笛卡尔积，幂等跳过已存在组合，维度/数量上限保护，新变体逐字段继承商品级默认值；单变体商品前台不输出规格选择器）、分类树与品牌（分类父子层级 + 排序 + 工程内唯一 slug + SEO 字段；品牌为独立实体含 logo/描述/slug/SEO；商品挂多个分类并指定主分类 —— 「主分类必属于附属分类」的不变量由服务端维持 —— 并可指定品牌；有子级或被商品引用的分类、被商品引用的品牌一律拒绝删除）、标签与自动规则（#11：手工标签手工挂载；自动标签只接受内置规则类型 + 白名单参数 —— `new_arrival`（上架 N 天内，以 `products.published_at` 判定）/ `price_range` / `on_sale`，未知类型、未知键、类型不符、越界一律拒绝，不接受自由表达式；重算时机明确 —— 商品写操作后、变体写操作后、标签定义变更后、显式调用（接口 / 后台按钮），重算只替换自己那一个 tag id，绝不覆盖手工标签；迁移 091 补 `products.published_at` / `product_tags.recalc_at` + 规则形状约束；后台页 `/admin/product-tags` 可查看每个标签命中哪些商品）、实体类型注册（`product` 字段白名单 + 构建期字段解析器；#12 扩到 `product_category` / `product_brand` / `product_tag` / `product_attribute` 五个类型，分类 / 品牌 / 标签 / 属性组可各自作为数据源绑定）、集合源注册（`content:product`：字段白名单 + 过滤维度 `status` / `categoryId` / `brandId` / `tagId`（#21 补齐分类 / 品牌 / 标签三条**等值**维度，逐个下推 SQL —— 分类与标签走 081 既有 GIN 索引的 JSONB 包含判断、品牌走迁移 117 补的 `idx_products_brand_id`，三条维度彼此 AND 且可与 status 叠加；非法 id 形状在解析期报错而不是「匹配不到任何行」——后者会把配置错误伪装成空集合），**外加属性值维度**（#25：前缀维度 `option.<属性组key>=<属性值key>` —— 属性组由用户自建、键无法穷举，所以白名单放的是**命名空间**而非固定键，子键形状由解析器校验、值是否存在由 SQL 决定；值在**变体**上（`product_variants.option_values`）而不是商品行，故逐属性下推 `EXISTS (… v.option_values @> jsonb_build_object(key, value))` 且只认启用变体，迁移 118 补 GIN(jsonb_path_ops) 索引；商品列表组件可固定筛一个属性值，访客可交互的多属性筛选走 #27）+ 排序键，按构建上下文的工程取数，集合类组件可直接绑定；商品展示组件（#22 `core.productCard` 商品卡 / #23 `core.productList` 商品列表，代码在 `internal/builder/components/`，不属本模块）：字段槽位写 `item.<字段>`（集合卡模板，集合组件逐项注入作用域）或 `product.<字段>`（绑当前实体），两种前缀都由 FieldBindings 翻译成 `product.<字段>` 交保存期注册表校验；列表的筛选维度在**构建期下推**到集合源（状态 / 分类 / 品牌 / 标签），排序在截断之前生效，**不分页**（用「取几条」截断，翻页属数据驱动页面能力）；商品详情规格选择器的**实时可用量**（#24）：构建期只把变体 id 烘进产物，可用量由访问面片段 `productVariantAvailability`（GET anonymous，参数 `variantIds`）每次现读 `inventory` 真源 —— 把库存烘进静态产物等于发布一份过期库存；结论四态（库存充足 / 仅剩 N 件 / 暂时缺货 / 以结算时库存为准），「未知（查不到 / 端口未接入）」与「为 0」严格区分，不把未知说成缺货；**降级**：无 JS / HTMX 不可用时页面保留构建期兜底文案、规格选择器仍是原生 radio（键盘可达、无脚本也能选），片段层端口未接入时整体降级为「以结算时库存为准」而非 500；片段参数来自 URL，非 uuid 形状的 id 在服务侧直接丢弃（否则 PostgreSQL 的 uuid 解析报 22P02、页面 500））、商品域多语言（#12：商品名 / 副标题 / 描述 / 图片 alt、分类名与描述、品牌名、标签名、属性组名与属性值展示文本按语言翻译，译文与原文分离存于 `sys_translation`（语境 = 实体类型.字段名），改原文后旧译文按 hash 自动失效；slug / SKU / 条码 / 价格与数字 / 属性值 key 不参与翻译 —— 属性值只翻展示文本，筛选参数与 URL 段原样；无译文逐字节回退原文；入口 `/admin/products/translations`（商品列表行内「多语言」按钮）；译文变更同时标记手工页面与受影响自动发布实例待重建）、定价工具（#13：四种内置定价规则 —— 成本乘倍数 / 成本加价 / 目标毛利率（售价 = 成本 ÷ (1 - margin)）/ 统一售价，各带白名单参数，未知类型 / 未知键 / 类型不符 / 越界一律拒绝；四种尾数处理 —— 不舍入 / 向上取整到元 / 尾数 9 / 尾数 99，一律向上取整（只抬不降）；作用范围三种 —— 单个 SKU、单个商品的全部变体、筛选集（工程 + 状态 / 关键词 / 分类 / 品牌 / 标签，空筛选即拒绝），单批商品数有上限；应用前可预览（预览与落库共用同一份算价逻辑，预览不写库），应用落库并留痕 —— `product_price_adjustments` 批次 + 逐变体「原价 → 新价」明细（含规则 / 尾数 / 范围 / 操作人 / 备注），无改动时拒绝写空批次；售价写回 `product_variants.price`，构建期（实体类型解析器 / 集合源）读到的就是落库值，定价不依赖 builder / pipeline / artifact（不进构建管线）；落库后按「变体写操作后」重算本工程自动标签；迁移 095/096/097；后台页 `/admin/product-pricing`）；详情页模板可选与预览（#14：同一商品类型下可建**多套命名模板**，各自的 `content_templates` 行 + 不可变版本快照独立演进（改一套只推进它自己的版本号）；发布实例的 `template_id` 从「只写不读的身份列」变成**可切换的绑定** —— 发布/预览显式指定就用指定那套，缺省仍按实体类型取默认模板（既有口径与行为逐字不变）；实例重建（含内容变更触发的 `RebuildStale` 自动重建）读**绑定**模板而不是「同类型最新」，否则切换过的模板会被下一次内容更新悄悄换回去；发布前可预览 —— `presentation.PreviewInstance` 只读渲染（不写快照/产物/指针、不落盘、不激活 URL），预览与发布共用同一份渲染，预览看到的字节 == 发布会产出的字节；跨实体类型的模板在入口拒绝（`ErrTemplateTypeMismatch`）；模板解析新增按 ID 入口 `contenttemplate.ResolveTemplateByID`（不存在即报错，不静默回落到类型默认）；后台页 `/admin/products/template`（商品列表行内「详情页模板」入口：列多套命名模板与版本、新建命名模板、预览渲染效果、发布、切换模板并重新发布）；迁移 098 补 `presentation:preview` / `presentation:get_by_entity` 权限点，接口 `/api/presentation/{preview,get-by-entity}`；捆绑品可配置选项与整单校验（#20：套餐的**选项集跨商品挑选已存在 SKU**（spec 第 53/54 条），每项带必选 / 可选与默认、最小、最大数量 —— 最大填 0 即「留空 = 不设上限，只受库存约束」；主体可配选项数量上限（服务端护栏 1..20）与整单最小 / 最大**总件数**（按总件数而非单项判定）。存储沿用既有决定：规则落在 `products.bundle_items` 这一个 JSON 列（迁移 114 把裸数组收紧为对象 + CHECK 形状约束，不新建关联表）。保存时拒绝自相矛盾的配置（最大 < 最小、默认 < 最小、整单最大 < 最小）与**永远无法满足的整单下限**（下限高于所有选项能加到的上限 —— 存进去等于把商品锁死），并逐个校验 SKU 的存在性、同工程、非自引用、不重复；跨工程 / 已删除 / 自引用 SKU 一律拒绝。**整单校验是同一份纯读逻辑服务两个出口**（后台 API `/api/product/bundle/validate` 与前台片段`/_fragments/bundleConfiguratorCheck`），因此构造请求绕过前端也一律被拒：漏必选、单项超上下限、低于整单下限、数量非法（负数 / 非整数 / 超硬上限）、配置外 SKU、重复 SKU 逐条给出具体错误。**数量上限与整单下限同时受库存可用量约束，且可用量只读 `inventory` 真源** ——经 `VariantAvailabilityPort`（inventory 实现、装配注入），端口未注入即 fail-closed；把 `product_variants.stock_total` 缓存改成 999 也照样拦得住（缓存永不参与判断）。套餐价 = 主体自定价（商品级默认价，缺失时取最低启用变体价）：子项价格不参与前台展示、子项成本仍在后台保留，校验结论同时给出展开后的子项快照与成本合计（订单域将来必须快照这份结果，不得引用商品当前的 BOM）。前台配置器走 **runtimefragment**（`bundleConfigurator` GET 渲染 +`bundleConfiguratorCheck` POST 校验，均为 anonymous 的纯计算能力，不写库、不预占库存；片段端点自 #20 起支持 POST 表单并行数组，并按 `Spec.Method` 做方法匹配）；产出适配桌面 / 平板 / 手机与鼠标 / 滚轮 / 触屏 / 键盘（折行容器 + 520px 断点 + `min(100%,…)` 宽度 + number / numeric 输入）。后台页 `/admin/products/bundle`（选商品 → 配选项 → 保存，页面内嵌前台配置器预览）；接口 `/api/product/bundle/{get,set,validate,skus}`；迁移 114/115/116） | 库存流水与扣减、采购、订单、客户（库存真源与仓库实体在 `inventory`，本模块只经 `VariantStockPort` 端口与它交互） |
| `inventory` | 仓库 / 库存真源 / 库存变动契约 / 货源管理（issue #15 / #16 / #17）：仓库实体（短码 / 名称 / 状态 / **默认仓**）与库存**真源** `inventory_stocks`（维度「SKU × 仓库」，唯一约束 `(variant_id, warehouse_id)`，同一 SKU 可在多个仓各有一行）。建变体时归属仓可选（不选即兜底默认仓）并自动生成初始 0 的库存记录；SKU 编码以归属仓短码开头（`{仓短码}_{商品码}_{序号}`，如 `SZ_TEE_001` —— 前缀是默认发货仓，不是「这个 SKU 只属于这个仓」）。「必须有一个默认仓」由部分唯一索引 + 服务层守卫维持（工程内第一个仓自动成为默认仓；默认仓不能删 / 不能停用 / 不能取消默认；缺默认仓时未指定仓库显式报错）；短码工程内唯一（`upper(code)`）、只允许 `A-Z0-9` 且 ≤8 位。依赖方向 **`inventory → product`** —— 本模块实现 product 契约定义的 `VariantStockPort`（ResolveWarehouse / EnsureVariantStock），装配期注入商品模块，商品模块不认识仓库实现。迁移 099（两表）/ 100（9 个权限点）/ 101（「库存管理」菜单）。**#16 补齐变动契约**（迁移 102/103/104：变更原因字典 / 库存流水 / 物料清单 / 缓存同步台账）：① `ChangeStock`（in 入库 / out 出库 / adjust 盘点调整到目标值）与 `DeductStock`（不足即**整体拒绝**），可用量在 `SELECT … FOR UPDATE` 行锁内判定，多行一律按 **(variant_id, warehouse_id) 标识升序**加锁（与入参顺序无关 ⇒ 全局同一锁序，并发批次等待链不可能成环；事务内三段式「幂等建行 ON CONFLICT DO NOTHING → 按序加锁 → 按序应用」，前两段不可合并）；② 每次变动写流水（方向 / 绝对量 quantity / 带符号 delta / 变动前后值 / 原因 / 来源引用 source_type+source_ref / batch_id；调整到当前值不写流水）；③ 变动原因**只认字典**（内置 `project_id IS NULL` 全工程可见且不可改 + 工程内自定义 code 唯一、可改名 / 停用，方向必须与变动方向一致，停用后历史流水仍按 code 快照可读）；④ 物料清单（父 SKU → 子项 × 用量，全量替换、空即清空）扣减时 **explode 到叶子**（有清单的 SKU 被展开而不是被扣），自引用 / 重复子项 / 用量非正 / 成环在维护入口拒绝；⑤ 商品侧缓存经 **`productcontract.VariantStockCachePort`**（product 实现、inventory 调用，与 `VariantStockPort` 方向相反）在**库存事务提交之后**同步并盖 `stock_synced_at` 时间戳，失败不报错、不回滚真源、落 `inventory_stock_cache_syncs` 台账，由 `ReconcileStockCache` 对账兜底（逐变体比对真源汇总 vs 缓存，可修复）。接口 `/api/inventory/{warehouse/*,stock/*,movement/list,reason/*,bom/*,cache/*}`（权限点 25 个）；后台页 `/admin/inventory`（仓库管理 + 某 SKU 各仓库存 + 库存变动表单 + 变动原因字典 + 库存流水 + 缓存漂移提示，商品页每个变体有「各仓库存」入口）。**#17 落地货源管理**（迁移 105/106/107）：一张表 `inventory_sources` 承载**全部进货来源** —— 外部供应商 / 集团内关联公司 / 自家工厂用 `type`（external / internal）区分，`related_party` 是独立于类型的**关联方标志**（内部货源恒为真，数据库 CHECK 兜住「内部即关联方」；外部供应商也可标记）；`config` 是**异构对接扩展信息**（JSON 对象 —— 只有它是自由形状：类型 / 关联方 / 结算价 / 状态都是结构化列，因为报表要按它们分组、采购单要按它们校验；非对象一律拒绝）；内部货源可设 `settle_price`（自产商品的成本口径：外部供应商带结算价一律拒绝，改类型为外部必须显式清空，绝不静默丢值）。接口 `/api/inventory/source/{list,get,summary,create,update,delete}` —— `source/list` 的 type / relatedParty / status / keyword 是可组合筛选维度，`source/summary` 按「类型 × 关联方」分组给出报表区分口径（统计不带状态过滤：停用只是「不再选用」，历史关联交易口径不动）；后台页 `/admin/inventory/sources`（关联方交叉统计 + 筛选 + 建 / 改 / 停启用 / 删，全部原生表单 + csrf_token）。**#18 落地采购单与入库**（迁移 108/109/110）：① 采购单 = 单头（单号工程内唯一 / 来源 = #17 的货源 / 收货仓 / **状态是推导值**）+ 结构化行（SKU × 采购数量 × 采购单价 × **已入库数量**），同一单里同一 SKU 只允许一行；② **状态由「已入库数量 与 采购数量」推导**（pending 未入库 / partial 部分入库 / received 已入库，没有任何人工置位入口），改单与收货后都在同一事务内重算写回；③ 登记收货（`RegisterReceipt`）在**采购单头行锁**内先做记账 —— 按行用「守卫写在 WHERE 里的原子递增」累加已入库数量（`received_quantity + n <= quantity`，受影响行数 0 即超收拒绝，不是先读后写）、写入库单与入库行、用最新行重算状态；随后经 **#16 的 `ChangeStock`**（方向 in + 原因字典 `purchase_in` + 来源引用 `purchase_order`/采购单号）加库存并写流水 —— 绝不旁路写库存；库存变动失败则**补偿**（退回已入库数量、删除入库单、重算状态），不留「记了账没动库存」的半截状态；④ 入库以采购单价（或本次到货价）经 **`productcontract.VariantCostPort`**（product 实现、inventory 调用，第三对端口）写回 `product_variants.cost_price`（提交之后的独立步骤，失败记在入库单行的 `cost_error` 上，不回滚真源）；⑤ **幂等 / 可重放保护** —— `receipts.request_id` 工程内唯一（部分唯一索引，空串不参与），重复提交命中既有入库单并原样返回，不会第二次动库存；⑥ 自家工厂走**生产入库**（`RegisterProductionInbound`）：无采购单（DDL CHECK 钉死「采购才有 order_id」）、来源必须是**内部**货源、**成本价手工填写**，库存变动同样走 `ChangeStock`（原因 `production_in`）；⑦ 进货历史（`purchase/history`）按 SKU / 变体查历次入库（单据号 / 数量 / **当时单价快照** / 来源 / 收货仓 / 成本价回写结果）；接口 `/api/inventory/purchase/{list,get,create,update,receipt,production,history}`（权限点 32 个），后台页 `/admin/inventory/purchases`（建单选 SKU 行 + 列表逐行登记入库 + 生产入库 + 进货历史，全部原生表单 + csrf_token + 一次性幂等键）。**死线：一切影响可用量的判断只读真源，绝不读 `product_variants.stock_total` 缓存** | 订单 / 客户（销售侧）；商品侧 `stock_total` 只作为展示缓存被同步 / 对账，不参与任何可用量判定 |
| `masterdata` | 主数据变更记录（issue #19）：一张 **append-only** 的字段级审计表 `master_data_changes` —— 一行 = 一次写操作里的一个字段（实体类型 / 实体 id / 实体展示名快照 / 动作 / 字段 / 旧值 / 新值 / 来源 / 操作人 / 时间）。三个语义：① **逐字段一行**，新增 / 修改 / 删除都逐字段落行（新增时 old 为空、删除时 new 为空），读取侧永远是同一形状的字段级时间线，筛选维度（实体 / 字段 / 动作 / 操作人 / 时间窗）全部落在列上；② **只写真正变化的字段**（old == new 不落行，审计表的噪声会直接吃掉它的可信度），同值重复保存不留痕迹；③ **append-only 由数据库兜底** —— 迁移 111 建 `BEFORE UPDATE OR DELETE` 触发器直接 RAISE EXCEPTION，代码层也没有任何更新 / 删除入口（审计表能被改写就等于没有审计表）。**与库存流水职责分离**（验收 5）：`inventory_stock_movements` 记「库存数量怎么变的」（入库 / 出库 / 盘点），本表记「主数据字段怎么变的」（价格 / SKU 编码 / 上下架状态 / 默认发货仓 / 货源资料），同一次业务动作可以两边各记一处但记的东西不同，绝不互相替代。**依赖方向 product / inventory → masterdata**：本模块不认识商品与库存表，调用方把「改前 / 改后」字段快照递进来（`masterdatacontract.ChangeInput`），本模块只做 diff、落库、查询；快照格式化助手（价格两位小数 / bool / JSON 压缩）放在契约里，双方口径一致才不会因 99 与 99.00 的表示差异产生假记录。写入覆盖：商品与变体的增 / 改 / 删（含变体组合生成）、变体默认发货仓（建变体时解析出的归属仓，商品侧没有独立列故以 `home_warehouse_id` / `home_warehouse_code` 两个字段入账）、定价工具批量改价（origin=pricing）、采购 / 生产入库的成本价回写（origin=receipt）、货源增 / 改 / 删（类型 / 关联方 / 结算价 / 状态 / 对接配置，origin=source）；操作人是会话里的登录名（与库存流水 operator_id 同口径），由 inbound 覆盖写入、客户端不可伪造。接口 `/api/masterdata/change/{list,count,entities,entity}`（4 个只读权限点，无写接口 —— 对外不提供「手工补一条记录」的口子）；后台页 `/admin/masterdata/changes`（按实体查询：实体清单点一行即锁定该实体看完整时间线，可按实体类型 / 实体 id / 字段 / 动作 / 操作人 / 时间区间组合筛选）；迁移 111（表 + 触发器）/ 112（权限点）/ 113（菜单） | 数量库存的增减（在 `inventory`）；商品与货源的业务规则（本模块只记「改了什么」，不判定「能不能改」） |

> `build` 无独立模块目录：编译内核在 `internal/builder`，发布内核在 `internal/pipeline`。
> `permission/role/menu/dept/datarule` 已并入 `admin` 大模块，不再独立。
> 模块落地后必须同步更新本表；新增模块代码与规则文件在同一批提交中更新，禁止出现「目录已存在、规则仍写未落地」的漂移。

### 命名约束

- `menu` = 管理后台权限菜单；`navigation` = 公开站点导航，两者不可混用
- `admin` = 管理控制面账号；未来访客账号必须另建领域模块
- `build → artifact → publication` 是单向流水线，后者不得反向导入前者实现
- 跨模块只使用 `contract` 和不可变 DTO；不得导入其他模块的 `service/model`（不可变 DTO 允许跨模块传递，对齐 `internal/module/CLAUDE.md` 表隔离约定）

## 核心约定

### 启动与关闭

统一入口：

- `config.Init()` → 读配置
- `config.InitComponents()` → 初始化所有 `pkg` 组件（Critical 优先：database → cache → auth → casbin → …）
- `config.CloseComponents()` → 逆序关闭

组件不自行决定进程退出，组件只返回 `error`。配置校验在各自 `pkg.Init()` 内部完成。
**auth 组件 fail-fast**：`redis.enabled=false` 时启动失败（`RequireSessionStorage`），release 模式弱 `session_secret` 拒绝启动。

### 模板渲染（Jet v6）

- 所有后台页面由 Go 服务端使用 Jet v6 渲染，实现 `gin.HTMLRender` 接口包装为 Gin 标准 Render
- 模板位置：`internal/templates/admin/`（后台页面）、`internal/templates/components/`（构建期组件，go:embed）
- 开发模式 `jet.DevelopmentMode(true)` 禁用模板缓存；**生产模式必须关闭**（由部署配置驱动）
- Jet 模板内 CSRF token 注入必须用 chain 索引写法 `{{ .["csrf_token"] }}`（`{{.csrf_token}}` 缺 key 会运行时报错）

### 交互方式（HTMX）

所有前台交互通过 HTMX 属性驱动，不写自定义 JS（后台工作台 workbench.js 例外，属构建器前端）。
CSRF：HTMX 请求经 `<body hx-headers='{"X-CSRF-Token":"{{ .["csrf_token"] }}"}'>` 继承；原生表单必须显式加 `csrf_token` 隐藏域；fetch 请求必须带 `X-CSRF-Token` 头（workbench.js/media-lib.js 已封装）。

### 认证与鉴权（已实现，三层链）

- `Session + Cookie` → 认证（gin-contrib/sessions + Cookie 存储）：cookie 只存最小认证信息（user_id/username/session_id/issued_at），用户资料走 Redis
- `CSRF` → 所有 POST 写操作强制 token 校验（登录成功返回 token；`X-CSRF-Token` 头或 `csrf_token` 表单域）
- `Casbin` → 鉴权（Enforce(user_id, path, method)；业务权限点见迁移 030/031 seed，超管 is_admin=1 全量策略）

挂载矩阵：

| 路由组 | SessionAuth | CSRF | Casbin |
|---|---|---|---|
| `/api/captcha`、`/api/admin/login` | 豁免 | 豁免 | 豁免 |
| `/api/admin/*` 六领域 | ✅ | ✅ | ✅ |
| `/api/{media,project,block,page,artifact,publication}/*` | ✅ | ✅ | ✅ |
| `/api/{content,contenttemplate,presentation,blueprint,navigation,plugin,inventory}/*` | ✅ | ✅ | ✅ |
| `/admin/*` 页面、`/`、`/workbench*` | ✅ | ✅ | —（页面路由） |

Cookie 属性：`HttpOnly`、`Secure`（release 自动启用）、`SameSite=Lax`。

### 登录安全

- 密码 bcrypt；验证码图片化（`/api/captcha` 只返回 `captcha_id` + `captcha_image`，答案绝不下发）
- 登录失败 ≥5 次只写 `locked_until_time = now+30min`（自动过期），**绝不修改 Status**
- 失败计数必须用原子 SQL（`count = count + 1`），禁止读-改-写回

### 路由

- 只用 `GET` 和 `POST`；禁止 RESTful 路径参数，全部用 Query 参数
- 主路由聚合在 `internal/routers/routes.go`；模块路由在 `internal/module/<模块>/inbound/http/`
- 健康检查：`GET /livez`、`GET /readyz`（组件级就绪）
- 静态面：`/site`（激活产物，`http.Dir` 只读）、`/storage`（媒体上传）、`/static`（后台静态资源）
- CORS：白名单来自 `server.cors_allowed_origins`；release 无白名单拒绝跨域；TrustedProxies release 模式为 nil（不信任 XFF）

### 响应与错误处理

| 请求类型 | 响应格式 |
|---|---|
| HTMX 请求（`HX-Request: true` / `Accept: text/html`） | Jet 渲染的 HTML 片段 |
| JSON API | `Response{Code,Message,Data}`（`pkg/response`） |

- 未登录页面请求 302 到 `/admin/login`；API 请求返回 401 JSON
- 业务模块统一通过模块 `enums` 提供响应消息；`pkg` 和系统包直接用中文提示或原始 `err`
- dashboard 页面 handler 禁止 `c.String(500, err.Error())` 直出内部错误，必须走 `pkg/response` + enums

### 数据库

- 统一走 dbx MCP，默认连接 `WSL_PostgreSQL@16.14`（127.0.0.1:5432），默认库 `base`；调用时显式传 `connection_name`
- 查询一律参数化；context 必须传播（`WithContext`）
- 迁移：`public/migrations/` 版本化 SQL（幂等），`register.go` 注册；seed 用 ConditionSQL（030 权限点 / 031 超管策略）
- datarule 插件字段引用按方言（PG 双引号 / MySQL 反引号）；部门范围整段精确匹配

### model 层定位（重要，评审与开发共同遵守）

`model` 是**表访问单元（Repository）**，不是 DDD Domain Model：

- ✅ 允许：本模块表的 CRUD、聚合与**聚合内原子组合**（如 `CreateWithRevision` 在同一事务内写 `pages` + `page_revisions`）；查询条件一律以参数传入，方法内不得写死业务条件
- ❌ 禁止：跨 model 调用、业务规则/决策（谁能删、状态机）、**跨聚合/跨模块事务**
- 跨聚合/跨模块事务必须在 service 层编排：model 暴露 `Transaction()` 透传（或方法接受外部 `*gorm.DB`/`*gorm.Session`），由 service 决定事务边界与回滚
- `DB(ctx)` / `RevisionDB(ctx)` 等裸 gorm 句柄是 model 的**内部实现细节，只允许被本 model 的仓储方法消费**；service 禁止调用它们拼接查询
- service 对持久化的唯一入口是 model 的具名方法；新增查询需求 = 给 model 加方法，而不是在 service 里写 `.Where().Create()`
- 评审拦截项：`internal/module/*/service` 中命中 `\.DB(ctx)` 或 `\.RevisionDB(ctx)` 即打回（含先存变量的 `query := x.DB(ctx)` 写法；仅 admin 豁免，见下）
- `contract/` 只放模块对外接口；`service` 依赖其他模块能力时直接引用对方 `contract`

**admin 豁免条款**：`admin` 为管理面 CRUD 大模块（六领域合并、同包直调），service 层经 `DB(ctx)` 直查**明文豁免**。豁免边界：仅限 admin 模块、仅限本模块表、跨表事务仍须 `Transaction()` 编排、简单 CRUD 之外的业务查询仍走 model 方法。新增模块一律禁止直查。

## 测试

- 默认跑现有测试，不新增额外测试框架
- 接口优先维护 feature 链路测试（`public/test/`，真实 PostgreSQL 环境），复杂逻辑补 unit
- 测试基建已迁移到本地 PostgreSQL（sqlite 驱动已移除）；PG/Redis 不可用时相关用例 `t.Skip`
- 组件测试在组件包内（`internal/builder/components/*`），含确定性构建与 fuzz 测试
- 并发敏感代码跑 `go test -race`
- **交互改动的验证清单**：涉及输入的改动，逐种输入方式各测一遍 —— 鼠标拖拽 / 滚轮与触摸板 /
  触屏滑动 / 键盘 / 点击。程序化调用（直接设状态、合成单一事件）**覆盖不到**「用鼠标滚一下」
  「用触屏滑一下」这类真实路径；本项目的多个交互缺陷（堆叠轮播无滚轮、纵向 `touch-action`
  写死）都只在真实输入下才暴露。产物层面的快速核对：用 CDP 发真实 `mouseWheel` /
  `PointerEvent(pointerType='touch')`，配合「先聚焦容器再按方向键」验证键盘路径。
- **所有组件必须适配多端**（硬规则，2026-09 确立）：新建或修改组件时，产出必须在
  桌面 / 平板 / 手机上都能正确渲染，在鼠标 / 滚轮与触摸板 / 触屏 / 键盘下都能操作。
  · **宽度不写死**：写 `min(100%, <设计宽度>)`，不要只写 `<设计宽度>`；
  · **绝对值带上限**：编译期算出的半径 / 位移 / 尺寸用 `min()` / `clamp()` 按视口封顶，
    且上限要让**元素自身尺寸**参与计算（`calc((100vw - <元素宽>) / 2 - 边距)`）——
    用 `40vw` 这类经验比例，在「大卡片 + 窄屏」的极端组合下照样溢出；
  · **触屏是独立环境**：`AddHover` 的规则包在 `@media (hover: hover)` 里，触屏上**根本不输出**。
    依赖 `:hover` 的任何形态都必须用 `CSSBuckets.AddHoverNone` 给出触屏等价形态，
    否则手机端该功能等于不存在；按压反馈用 `AddActive`（不带媒体查询）；
  · **验收**：三种视口（1440 / 768 / 375）各看一次、四种输入各操作一次；
    窄视口下在控制台确认 `document.documentElement.scrollWidth === clientWidth`，
    且不存在 `right` 越界的元素。完整规范见 `docs/02-C0-component-base-spec.md` §6.9。
- **做覆盖全部能力的实例页**是性价比最高的一次集成验证：单组件测试与四五个区块的小案例页
  都看不出模板截断、盒模型偏移这类缺陷，只有把全部模式铺在一个长页面里才暴露。
- **动画 / 观察者 / 时间线类改动**：必须读到「**值在变化**」（`transform` / `opacity` / `filter`
  在不同滚动位置或时刻下确实不同），**只读属性名、时间线名或计算值不算验证**。同时要保证
  **触发条件真的成立**再下结论：
  · `IntersectionObserver` 的 `root` 元素必须自身在页面视口内才会触发 —— root 在视口外时
    连初始回调都没有；
  · `animation-timeline: view()` 的元素必须真的处于滚动容器可视区内（且该场景下 view() 对
    **内嵌滚动容器**实测不驱动动画）；
  · 观察者回调是**异步**的，必须等一轮再读结果，不能在同一次求值里读。
  本项目已多次因「核对了自己写下的配置、没核对系统实际做的事」而误判通过
  （`:hover` 未真触发 / 长页面才暴露模板截断 / 动画值恒定不变 / observer 未触发）。
- **无头 + 自动化环境不出渲染帧**：`IntersectionObserver` 不回调、`requestAnimationFrame`
  不执行、`scroll` 事件不派发 —— 三者同源（都等下一次渲染帧），页面脚本创建的实例全都
  静默失效，容易误判成「实现有问题」。
  · **对策（验证用）**：操作之后调用一次**截图**（`ego_screenshot`，强制产生渲染帧），
    再读结果；或把手动创建的同参数实例与页面实例对比，能直接区分「环境问题」与「实现问题」。
  · **对策（实现用）**：增强逻辑优先用**同步几何计算 + `setTimeout` 节流**，而不是
    `rAF` / `IO` —— 前者在任何环境都可断言，后者只在真实浏览器里可靠。

## Git 与工具约定

- 每次 commit 用中文注明改动文件路径和修改内容简述
- 文档更新与代码提交分开；修改规则文件（本文件及子目录 CLAUDE.md）前先重新读取，桌面端可能并发改写
- GitHub 操作（仓库/Issue/PR/Release）优先用 `gh` CLI；Go 项目发版优先 GoReleaser（`goreleaser`）
- 语言运行时版本由 vfox 管理（`~/.vfox`），禁止 Homebrew/apt/系统包安装运行时；Node.js 依赖优先 pnpm，Python 用 uv

## 文档导航

- `docs/01-overview.md` — 概览、边界、冻结边界速查（注意：模块表以本文「模块现状」为准）
- `docs/03-pipeline.md` — 发布管线（§4.3/§5/§6.5/§9 已实现；§4.4/§7/§8 规划）
- `docs/05-implementation-plan.md` — 阶段计划（阶段 0-3 已完成；4-7 待办）
- `docs/06-plugin-system.md` — 插件体系规范（三级能力分层/双轨制/表扩展/样式引擎已落地；P1-P3 待做）
- `docs/06-A-plugin-ecosystem-roadmap.md` — 插件生态路线图（本体收口/SEO 基建/首批插件清单/商品重轨+壳）
- `docs/06-B-dual-track-adr.md` — ADR：双轨制决策固化（分轨/admin 契约双口/商品定位/正文双视图单真源/SEO 分工）
- `docs/02-*` — 组件规格；`docs/03-A-workbench.md` — 工作台
- `docs/04-B-dynamic-development-guide.md` — 动态能力开发指南（How-To：静态绑定/Fragment/Client Enhancement 三路径）
- `docs/02-E-seo-scoring-engine.md` — SEO 评分引擎（rubric 权重卡/Yoast 复用策略/自研计算器/执行计划）
- `docs/agents/` — Issue 追踪、Triage 标签、领域术语
- `internal/module/CLAUDE.md` — 模块开发规范；`pkg/CLAUDE.md` — pkg 组件规范
