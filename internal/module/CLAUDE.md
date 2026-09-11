# internal/module 开发规范

`internal/module/` 存放业务模块代码。模块直接平铺在本目录下，不区分前后台。

当前已有模块（2026-09 核对）：

- `admin/` — 管理控制面大模块（管理员、角色、权限点、菜单、部门、数据权限）；模块内部同包直调，自包含装配，不拆子模块
- `common/` — 公共业务能力（当前为验证码：标准库自绘 PNG 图片化，答案绝不下发）
- `dashboard/` — 需要后端逻辑的后台页面入口（仪表盘、可视化工作台 Workbench、媒体库、主题管理）
- `media/` — 附件与文件分类（LIKE 通配符转义、软删除过滤）
- `project/` — 站点工程、SiteSettings、多主题 Theme（list/activate/delete/settings）
- `page/` — 手工 Page 与 Page Document：草稿/构建/发布/回滚/改 URL
- `block/` — 复用资产（全局块）：16 种 kind + reuse_mode（global 引用/template 一次性复制）与 stale 传播编排
- `artifact/` — Artifact 元数据与内容对象闭包
- `publication/` — URL 占用、激活（两段式回执）、回滚
- `content/` / `contenttemplate/` / `presentation/` — CMS 内容、版本化结构模板、自动发布实例
- `blueprint/` — Page Document 初始化工具（用完即弃）
- `product/` — 商品域（#13 已落地定价工具）：商品与变体 CRUD（价格在变体上，商品级字段是新增变体的默认值模板）；商品属性组与属性值（组 + 值合一，值走 JSONB；`is_variation` 标记是否参与变体；商品侧只存 `attribute_ids` 引用，同一属性组可跨商品复用）；变体组合生成（#8：勾选属性值 → 笛卡尔积，`option_values` 组合幂等去重，维度/数量上限整体拒绝，新变体逐字段继承商品级默认值 —— 单变体商品前台不输出规格选择器）；分类树与品牌（#10：分类父子层级 + 同级排序 + 工程内唯一 slug + SEO 字段；品牌是独立实体，含 logo/描述/slug/SEO；商品挂多个分类（`category_ids`）并指定主分类（`primary_category_id`，迁移 088），「主分类必属于附属分类」的不变量由服务端维持 —— 显式指定则自动纳入、附属列表被替换掉原主分类时自动解绑；品牌同样只存引用；有子级或被商品引用的分类、被商品引用的品牌一律拒绝删除；后台页 `/admin/product-categories` 与 `/admin/product-brands`）；标签与自动规则（#11：手工标签手工挂载，自动标签只接受内置规则类型 + 白名单参数 —— `new_arrival`（上架 N 天内，判定基准是 `products.published_at`）/ `price_range`（存在启用变体价格落在区间内）/ `on_sale`（存在启用变体有划线价），未知类型、未知键、类型不符、越界一律拒绝；重算时机只有四条 —— 商品写操作后、变体写操作后、标签定义变更后、显式调用（`POST /api/product/tag/recalc` 与后台按钮），重算按元素摘挂只动自己那一个 tag_id，绝不覆盖手工标签；迁移 091 补 `products.published_at` 与 `product_tags.recalc_at`；后台页 `/admin/product-tags` 可查看每个标签命中哪些商品）；向实体类型注册表注册 `product`（字段白名单 + 构建期字段解析器，可翻译字段按构建语言取译文）；注册商品集合源（#9：`content:product` 的 `CollectionResolver` + `CollectionSchemaProvider` —— 集合源元数据给字段白名单 / 过滤维度 `status` / `categoryId` / `brandId` / `tagId`（#21：三条 id 维度是等值过滤，分类与标签走 GIN 索引的 JSONB 包含、品牌走迁移 117 的 `idx_products_brand_id`，非法 uuid 在解析期拒绝）/ 排序键 `sort,createdAt`，构建期按构建上下文的工程 ID 取数、按构建语言取译文；白名单外字段与维度在构建期被拒绝）；商品域多语言（#12：商品名 / 副标题 / 描述 / 图片 alt、分类名与分类描述、品牌名、标签名、属性组名与属性值展示文本按语言翻译，译文与原文分离存于 `sys_translation`（内容寻址 `(source_hash, context, lang)`，语境 = 实体类型.字段名），改原文后旧译文按 hash 自动失效；slug / SKU / 条码 / 价格与数字 / 属性值 `key` 不参与翻译 —— 属性值只翻 `label`，筛选参数、URL 段与规格组合原样保留；无译文逐字节回退原文；实体类型注册表扩到 `product` / `product_category` / `product_brand` / `product_tag` / `product_attribute` 五个类型（分类 / 品牌 / 标签 / 属性组可各自作为数据源绑定，也可随商品进 `product.related` 派生值）；翻译入口 `/admin/products/translations`（商品列表行内「多语言」按钮，保存复用 `/api/product/update` 权限点，不做独立菜单）；译文变更同时标记手工页面（`page.MarkStaleForI18n`）与受影响自动发布实例（`presentation.MarkStaleByDependency`，键 `direct_content:{实体类型}:{实体id}`）；迁移 094 补 `products.images_alt`；定价工具（#13：四种内置定价规则 —— 成本乘倍数 / 成本加价 / 目标毛利率（售价 = 成本 ÷ (1 - margin)）/ 统一售价，参数各走白名单（未知类型、未知键、类型不符、越界一律拒绝，不接受自由表达式）；四种尾数处理 —— 不舍入 / 向上取整到元 / 尾数 9 / 尾数 99，一律向上取整，只抬不降；作用范围三种 —— 单个 SKU（变体 id）、单个商品的全部变体、筛选集（工程 + 状态 / 关键词 / 分类 / 品牌 / 标签，空筛选与无命中一律拒绝，单批商品数有上限）；应用前可预览（`PreviewPricing` 与 `ApplyPricing` 共用同一份算价逻辑，预览不写库），应用在**同一事务**里写回 `product_variants.price` 并写留痕 —— 批次 `product_price_adjustments` + 逐变体「原价 → 新价」明细（含规则 / 尾数 / 范围 / 操作人 / 备注），只有真正变化的变体才写库与留痕，一条都没变时返回 `ErrPricingNothingChanged`；成本价与划线价一律不动；落库后按「变体写操作后」的时机重算本工程自动标签；**不进构建管线** —— 构建期（实体类型解析器 / 集合源）读到的就是落库后的确定值，定价源码不依赖 builder / pipeline / artifact；迁移 095（两张留痕表）/096（权限点）/097（后台菜单）；接口 `/api/product/pricing/{rules,roundings,preview,apply,history,adjustment}`，后台页 `/admin/product-pricing`）；详情页模板可选与预览（#14：同一商品类型下可建**多套命名模板**，各自的 `content_templates` 行 + 不可变版本快照独立演进（改一套只推进它自己的版本号）；发布实例的 `template_id` 从「只写不读的身份列」变成**可切换的绑定** —— 发布/预览显式指定就用指定那套，缺省仍按实体类型取默认模板（既有口径与行为逐字不变）；实例重建（含内容变更触发的 `RebuildStale` 自动重建）读**绑定**模板而不是「同类型最新」，否则切换过的模板会被下一次内容更新悄悄换回去；发布前可预览 —— `presentation.PreviewInstance` 只读渲染（不写快照/产物/指针、不落盘、不激活 URL），预览与发布共用同一份渲染，预览看到的字节 == 发布会产出的字节；跨实体类型的模板在入口拒绝（`ErrTemplateTypeMismatch`）；模板解析新增按 ID 入口 `contenttemplate.ResolveTemplateByID`（不存在即报错，不静默回落到类型默认）；后台页 `/admin/products/template`（商品列表行内「详情页模板」入口：列多套命名模板与版本、新建命名模板、预览渲染效果、发布、切换模板并重新发布）；迁移 098 补 `presentation:preview` / `presentation:get_by_entity` 权限点，接口 `/api/presentation/{preview,get-by-entity}`；捆绑品可配置选项与整单校验（#20：套餐的**选项集跨商品挑选已存在 SKU**（spec 第 53/54 条），每项带必选 / 可选与默认、最小、最大数量 —— 最大填 0 即「留空 = 不设上限，只受库存约束」；主体可配选项数量上限（服务端护栏 1..20）与整单最小 / 最大**总件数**（按总件数而非单项判定）。存储沿用既有决定：规则落在 `products.bundle_items` 这一个 JSON 列（迁移 114 把裸数组收紧为对象 + CHECK 形状约束，不新建关联表）。保存时拒绝自相矛盾的配置（最大 < 最小、默认 < 最小、整单最大 < 最小）与**永远无法满足的整单下限**（下限高于所有选项能加到的上限 —— 存进去等于把商品锁死），并逐个校验 SKU 的存在性、同工程、非自引用、不重复；跨工程 / 已删除 / 自引用 SKU 一律拒绝。**整单校验是同一份纯读逻辑服务两个出口**（后台 API `/api/product/bundle/validate` 与前台片段`/_fragments/bundleConfiguratorCheck`），因此构造请求绕过前端也一律被拒：漏必选、单项超上下限、低于整单下限、数量非法（负数 / 非整数 / 超硬上限）、配置外 SKU、重复 SKU 逐条给出具体错误。**数量上限与整单下限同时受库存可用量约束，且可用量只读 `inventory` 真源** ——经 `VariantAvailabilityPort`（inventory 实现、装配注入），端口未注入即 fail-closed；把 `product_variants.stock_total` 缓存改成 999 也照样拦得住（缓存永不参与判断）。套餐价 = 主体自定价（商品级默认价，缺失时取最低启用变体价）：子项价格不参与前台展示、子项成本仍在后台保留，校验结论同时给出展开后的子项快照与成本合计（订单域将来必须快照这份结果，不得引用商品当前的 BOM）。前台配置器走 **runtimefragment**（`bundleConfigurator` GET 渲染 +`bundleConfiguratorCheck` POST 校验，均为 anonymous 的纯计算能力，不写库、不预占库存；片段端点自 #20 起支持 POST 表单并行数组，并按 `Spec.Method` 做方法匹配）；产出适配桌面 / 平板 / 手机与鼠标 / 滚轮 / 触屏 / 键盘（折行容器 + 520px 断点 + `min(100%,…)` 宽度 + number / numeric 输入）。后台页 `/admin/products/bundle`（选商品 → 配选项 → 保存，页面内嵌前台配置器预览）；接口 `/api/product/bundle/{get,set,validate,skus}`；迁移 114/115/116））
- `inventory/` — 仓库与库存记录（#15 已落地）、库存流水 / 扣减契约（#16 已落地）与货源管理（#17 已落地）：仓库实体（短码 / 名称 / 状态 / **默认仓**）与库存**真源** `inventory_stocks`（维度「SKU × 仓库」，唯一约束 `(variant_id, warehouse_id)`）。建变体时归属仓可选（不选即兜底默认仓）并在归属仓自动生成初始 0 的库存记录；SKU 编码以归属仓短码开头（`{仓短码}_{商品码}_{序号}`，如 `SZ_TEE_001` —— 前缀是默认发货仓，不是「这个 SKU 只属于这个仓」）。「必须有一个默认仓」由部分唯一索引 + 服务层守卫维持：工程内第一个仓自动成为默认仓，默认仓不能删 / 不能停用 / 不能取消默认，缺默认仓时「未指定仓库」显式报错（不静默挑仓）；短码工程内唯一（`upper(code)`）、只允许 `A-Z0-9` 且 ≤8 位（下划线是 SKU 编码分隔符，短码里不许出现）。依赖方向 **`inventory → product`**：本模块在 `service/product_port.go` 实现 product 契约定义的 `VariantStockPort`（ResolveWarehouse / EnsureVariantStock），顶层装配时经接口断言注入商品模块 —— 商品模块不认识仓库实现。#16 在此之上补齐**变动契约**（迁移 102/103/104 四张新表：变更原因字典 / 库存流水 / 物料清单 / 缓存同步台账）：① 按 SKU 增减 —— `ChangeStock`（in 入库 / out 出库 / adjust 盘点调整到目标值）与 `DeductStock`（不足即**整体拒绝**），可用量在 `inventory_stocks` 的 `SELECT … FOR UPDATE` 行锁之内判定，多行一律按 **(variant_id, warehouse_id) 标识升序**加锁（与入参顺序无关 ⇒ 全局同一锁序，并发批次等待链不可能成环）；事务内三段式「幂等建行（ON CONFLICT DO NOTHING）→ 按序加锁 → 按序应用」，①②不可合并（边建边锁才会真成环）。② 每次变动写流水 `inventory_stock_movements`（方向 / 绝对量 quantity / 带符号 delta / 变动前后值 / 原因 / 来源引用 source_type+source_ref / batch_id，调整到当前值不写流水）。③ 变动原因**只认字典**（不接受自由文本）：内置原因 `project_id IS NULL` 全工程可见且不可改，自定义原因工程内 code 唯一（小写字母数字下划线）、可改名 / 停用，原因方向必须与变动方向一致，停用后历史流水仍按 code 快照可读。④ 物料清单 `inventory_bom_items`（父 SKU → 子项 × 用量，全量替换，空清单 = 清空）—— 展开扣减**递归explode 到叶子**：有清单的 SKU 被展开而不是被扣，中间件半成品不动；自引用 / 重复子项 / 用量非正 / 成环在维护入口拒绝（成环判定从父项沿「谁把它当子项」上行，撞到新子项即成环）。⑤ 商品侧缓存（`product_variants.stock_total` + `stock_synced_at`）经 **`productcontract.VariantStockCachePort`**（product 实现、inventory 调用，与 `VariantStockPort` 方向相反）在**库存事务提交之后**同步（跨模块写不进同一事务）；同步失败不报错、不回滚真源，落 `inventory_stock_cache_syncs` 台账（status/error/synced_at），由 `ReconcileStockCache` 对账兜底（逐变体比对真源汇总 vs 缓存，可 `repair`）。接口 `/api/inventory/{warehouse/*,stock/*,movement/list,reason/*,bom/*,cache/*}`（权限点 100 + 104 + 106 共 25 个）；后台页 `/admin/inventory`（仓库管理 + 某 SKU 各仓库存 + 库存变动表单 + 变动原因字典 + 库存流水 + 缓存漂移提示）。#17 在此之上落地**货源管理**（迁移 105/106/107）：`inventory_sources` 一张表承载全部进货来源 —— 外部供应商 / 集团内关联公司 / 自家工厂靠 `type`（external / internal）区分，`related_party` 是独立于类型的**关联方标志**（内部货源恒为真，DDL CHECK 兜住；外部供应商也可标记）—— 报表按「类型 × 关联方」交叉取数（`SourceSummary`），筛选维度（type / relatedParty / status / keyword）直接落到查询上；`config` 是**异构对接扩展信息**（JSON 对象，非对象拒绝；类型 / 关联方 / 结算价 / 状态一律结构化列）；内部货源可设 `settle_price`（自产商品成本口径：外部带结算价拒绝、改类型为外部须显式清空）；接口 `/api/inventory/source/{list,get,summary,create,update,delete}`，后台页 `/admin/inventory/sources`。**#18 落地采购单与入库**（迁移 108/109/110）：采购单 = 单头（单号工程内唯一 / 来源 = #17 的货源 / 收货仓）+ 结构化行（SKU × 采购数量 × 采购单价 × 已入库数量，同一单里同一 SKU 只一行），**状态是推导值**（pending / partial / received，由「已入库数量 与 采购数量」推出，没有人工置位入口）；`RegisterReceipt` 在**采购单头行锁**内先记账 —— 已入库数量用「守卫写在 WHERE 里的原子递增」（`received_quantity + n <= quantity`，受影响行数 0 即超收拒绝，不是先读后写）累加，写入库单与入库行，再用最新行重算状态 —— 随后经 **#16 的 `ChangeStock`**（方向 in + 原因字典 `purchase_in` + 来源引用 `purchase_order`/采购单号）加库存并写流水，绝不旁路写库存；变动失败则**补偿**（退回数量、删单、重算状态），不留「记了账没动库存」；入库单价经 **`productcontract.VariantCostPort`**（product 实现、inventory 调用）写回 `product_variants.cost_price`（提交之后的独立步骤，失败记在入库单行 `cost_error`，不回滚真源）；`receipts.request_id` 工程内唯一（部分唯一索引）= **幂等 / 可重放保护**，重复提交命中既有入库单并原样返回；自家工厂走**生产入库**（无采购单 + 来源必须是内部货源 + 成本价手工填写，库存变动同样走 `ChangeStock`）；`purchase/history` 按 SKU / 变体查历次进货（单据号 / 数量 / 当时单价快照 / 来源 / 收货仓 / 成本价回写结果）；接口 `/api/inventory/purchase/{list,get,create,update,receipt,production,history}`，后台页 `/admin/inventory/purchases`（原生表单 + csrf_token + 一次性幂等键）。**死线：一切影响可用量的判断只读真源（带行锁），绝不读 `product_variants.stock_total` 缓存** —— 缓存只被同步 / 对账，且同步必须在提交之后
- `navigation/` — 公开站点导航（与后台 `menu` 严格隔离）
- `plugin/` — 插件体系
- `masterdata/` — 主数据变更记录（**issue #19 已落地**）：一张 append-only 的字段级审计表 `master_data_changes`（一行 = 一个字段：实体类型 / 实体 id / 展示名快照 / 动作 / 字段 / 旧值 / 新值 / 来源 / 操作人 / 时间）。逐字段一行（新增 / 删除同样逐字段落行，old 或 new 为空），只写真正变化的字段（old == new 不落行），**append-only 由迁移 111 的 `BEFORE UPDATE OR DELETE` 触发器在数据库层兜底**（代码层也没有更新 / 删除入口）。与库存流水职责分离：流水记数量变动，本表记字段级配置变更（价格 / SKU 编码 / 上下架状态 / 默认发货仓 / 货源资料），各记一处、互不替代。依赖方向 **product / inventory → masterdata**：本模块不认识业务表，调用方把「改前 / 改后」字段快照经 `masterdatacontract.MasterDataService.RecordChanges` 递进来（装配期用 `SetMasterDataChanges` 注入两个业务模块，任一未实现即 fail-fast —— 漏接的表现是「审计静默缺失」，比报错隐蔽得多）；快照格式化助手（`FormatPrice` / `FormatBool` / `FormatJSON`）放在契约里保证双方口径一致。操作人是会话登录名（inbound 覆盖写入）。接口 `/api/masterdata/change/{list,count,entities,entity}`（4 个只读权限点，**无写接口**）；后台页 `/admin/masterdata/changes`（实体清单 + 字段级时间线 + 组合筛选）；迁移 111/112/113。
- `runtimefragment/` — 白名单动态片段（无 contract，直挂访问面路由）

> `build` 无独立模块目录：编译内核在 `internal/builder`，发布内核在 `internal/pipeline`。
> 模块落地后必须同步更新本列表与 `AGENTS.md`「模块现状」表，同一批提交完成，禁止「目录已存在、规则仍写未落地」的漂移。

> 管理面六领域（管理员/角色/权限/菜单/部门/数据权限）已合并为 `admin` 大模块：
> 每个领域占 model/dto/handle/service 下的一个文件（如 `role_model.go`、`role_crud.go`），
> service 层同包互调、无 setter 注入；`contract/` 预留对外能力，当前无外部消费者。
> admin 的 service 层经 `DB(ctx)` 直查有明文豁免（见下方 model 层定位）。新增管理面领域时沿用此模式。

## 目录结构

```text
module_name/
├── contract/
│   └── <module>_service.go     # 本模块对外暴露契约
├── inbound/
│   ├── http/
│   │   ├── <module>_handle.go
│   │   └── <module>_router.go   # 自装配 + 路由注册
├── outbound/
│   └── <dependency>/            # 按需：外部协议转换或适配
├── service/
│   ├── <module>_service.go
│   └── <module>_<action>.go
├── model/
│   └── <module>_model.go
├── dto/
│   ├── <module>_req.go
│   └── <module>_resp.go
└── enums/                       # 必选，响应消息与错误消息
    └── <module>_enums.go
```

## 核心关系

- `contract/` — 只放本模块对外暴露的接口，不定义外部依赖接口
- `inbound` — 承接外部调用
- `service` — 实现本模块契约，可直接依赖其他模块的 `contract/`（及不可变 `dto/`），禁止导入其他模块的 `service/model`（豁免规则见 model 层定位）
- `outbound` — 按需增加，用于外部协议转换或适配（非必需目录）
- `model` — 持久化模型与数据库访问
- `dto` — 请求/响应结构
- `enums` — 必须存在，统一管理响应消息

## contract

- 只放本模块对外暴露的接口，如 `<module>_service.go`
- 不定义外部依赖接口；需要其他模块的能力时，直接引用对方 `contract` 包
- 所有接口放在一个文件即可，不必拆分多个文件

## inbound/http

- `router.go` 自行获取 `db`、创建 `model` 和 `service`、注册路由。
- `handle` 只负责参数绑定、调用 service、输出响应。
- 返回给前端的响应消息统一取 `enums`。

示例：

```go
func SetupXxxRoutes(rg *gin.RouterGroup, db *gorm.DB, ...契约参数) {
    m := model.NewXxxModel(db)
    svc := service.NewService(m, ...)
    handle := NewHandle(svc)

    g := rg.Group("/xxx").Use(builtin.SessionAuthMiddleware())
    g.GET("/list", handle.List)
}
```

## service

- `xxx_service.go` 只放 `Service` / `NewService()`
- `Service` struct 只持有本模块 `model` + 契约接口，不持有 `*gorm.DB`
- service 禁止调用 `model.DB(ctx)` 等裸句柄拼接查询；持久化唯一入口是 model 具名方法（admin 豁免，见 model 层定位）
- 跨模块依赖直接注入目标模块的 `contract` 接口
- 构造函数直接传参，不用 `Deps` 结构体（参数 ≤6 时直传）
- 必须加编译期断言：`var _ <contract>.XXXService = (*Service)(nil)`
- 业务用例拆到 `xxx_<action>.go`
- 返回 `error`，业务错误消息统一取 `enums`
- 使用命名返回值：`func (s *Service) Xxx(ctx, req) (res *XxxResp, err error)`

## 编码风格

- import 别名：`pagedto`、`adminmodel`、`pubcontract`、`pagemodel`（模式：`<模块名小写>dto/model/contract/enums`）
- 函数签名使用命名返回值，`error` 放最后

## model

- 放 Entity + `NewXxxModel(db)` + `DB(ctx)` + 通用查询方法
- 可放本模块固定常量（表名、状态值、API 路径）
- 请求/响应结构放 `dto/`，不放入 `model/`
- `DB(ctx)` 返回 `m.db.WithContext(ctx).Model(&Entity{})`
- 查询条件、分页、排序以**参数**传入方法（仅限本模块表）；方法内不得写死业务条件，不得多表关联
- 不放业务规则（状态机、归属校验等留在 service）

### model 层定位（Repository，非 DDD Domain Model）

> 权威版本见 `AGENTS.md` §「model 层定位」；两处冲突时以 AGENTS.md 为准，本节保持同步摘录。

- ✅ 允许：本模块表的 CRUD、聚合与**聚合内原子组合**（如全量替换 `Delete+Create` 在同一事务内）；查询条件以参数传入
- ❌ 禁止：跨 model 调用、业务规则/决策（谁能删、状态机）、**跨聚合/跨模块事务**
- 跨聚合/跨模块事务必须在 service 层编排：model 暴露 `Transaction()` 透传（或方法接受外部 `*gorm.DB`/`*gorm.Session`），由 service 决定事务边界与回滚
- `DB(ctx)` 等裸 gorm 句柄是 model 内部实现细节，**只允许被本 model 的仓储方法消费**；service 禁止调用它拼接查询
- 评审拦截项：`internal/module/*/service` 命中 `\.DB(ctx)` 或 `\.RevisionDB(ctx)` 即打回（含先存变量的写法）；**仅 admin 有明文豁免**（仅限本模块表、简单 CRUD；跨表事务仍须 `Transaction()` 编排），新增模块一律禁止直查

## outbound

- 用于 RPC / HTTP / MQ / SDK / cache 等外部调用
- **非必需目录**，直接引用对方 `contract` 即可满足需求时不加 outbound
- 实现依赖契约时必须加编译期断言

## 表隔离约定

模块间的数据表严格隔离，不允许跨模块直接关联查询。

### 隔离机制

```text
page/service
  ├── 持有 pagemodel.Model                → 只能碰本模块表
  ├── 持有 pubcontract.PublicationService → 接口，不知道数据从哪来
  └── 不持有 *gorm.DB                     → 无法 .Table() 切表
```

跨模块数据链路：

```text
page/service → 调 pubcontract.PublicationService
  → publication/service → publication/model → publication 路由表
```

这里强调的是依赖方向：调用方只依赖目标模块的 `contract`，目标模块自行负责其数据访问。

### 规则

- service 层禁止使用 `.Table()` / `.Model()` 切换到非本模块的表
- model 的 `DB(ctx)` 恒绑定本模块表（`WithContext + Model(&Entity{})`），不得重绑定到其他表；service 不得调用 `DB(ctx)`（见 model 层定位）
- 跨模块调用统一依赖目标模块的 `contract`；跨模块可传递的数据类型是 `contract` 与**不可变 dto**（对齐 `AGENTS.md`「命名约束」），禁止导入目标模块的 `service`、`model`

## 装配

各模块自己负责装配 `model` 和 `service`，顶层 `routes.go` 获取通用依赖（`db`）并按依赖顺序调用模块的 Setup 函数。

对于需要跨模块契约的模块，顶层 routes.go 在调用时从被依赖模块获取契约并传递过去：

```go
projectService := projecthttp.SetupProjectRoutes(authorizedAPI, db)
blockSvc := blockhttp.SetupBlockRoutes(authorizedAPI, db, projectService)
presentationSvc := presentationhttp.SetupPresentationRoutes(authorizedAPI, db, contentTemplateSvc, contentSvc)
```（节选自 `internal/routers/routes.go`，与实际装配顺序一致）

## dto

- `*_req.go` 给 `inbound` 绑定
- `*_resp.go` 给 `service` 返回
- 数据流：`inbound -> service -> inbound`

## enums

- `enums/` 是必须目录
- 所有响应内容都走模块 `enums`
- 包括：成功消息、参数错误消息、未授权消息、业务错误消息
- `handle` 和 `service` 不直接硬编码响应文案
- 未接好 `i18n` 时，`ErrXxx` / `MsgXxx` 直接等于中文常量

## 响应

按请求类型区分两种响应模式：

| 请求类型 | 响应格式 | 说明 |
|---------|---------|------|
| HTMX 请求（`HX-Request: true`） | Jet 渲染的 HTML 片段 | 列表刷新、表单提交后局部更新、弹窗内容 |
| JSON API | `pkg/response` JSON 结构 | 纯数据接口 |

- JSON 响应统一走 `pkg/response`：`response.Success`、`response.SuccessWithMessage`、`response.ErrorWithMessage`
- 传给 `response` 的消息统一来自模块 `enums`
- HTMX 响应直接渲染 Jet 模板片段返回，不走 `pkg/response`

## 路由

- 只用 `GET` / `POST`
- `GET` 查询
- `POST` 用于新增、修改、删除、状态变化