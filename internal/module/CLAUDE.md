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
- `product/` — 商品域（#11 已落地标签与自动规则）：商品与变体 CRUD（价格在变体上，商品级字段是新增变体的默认值模板）；商品属性组与属性值（组 + 值合一，值走 JSONB；`is_variation` 标记是否参与变体；商品侧只存 `attribute_ids` 引用，同一属性组可跨商品复用）；变体组合生成（#8：勾选属性值 → 笛卡尔积，`option_values` 组合幂等去重，维度/数量上限整体拒绝，新变体逐字段继承商品级默认值 —— 单变体商品前台不输出规格选择器）；分类树与品牌（#10：分类父子层级 + 同级排序 + 工程内唯一 slug + SEO 字段；品牌是独立实体，含 logo/描述/slug/SEO；商品挂多个分类（`category_ids`）并指定主分类（`primary_category_id`，迁移 088），「主分类必属于附属分类」的不变量由服务端维持 —— 显式指定则自动纳入、附属列表被替换掉原主分类时自动解绑；品牌同样只存引用；有子级或被商品引用的分类、被商品引用的品牌一律拒绝删除；后台页 `/admin/product-categories` 与 `/admin/product-brands`）；标签与自动规则（#11：手工标签手工挂载，自动标签只接受内置规则类型 + 白名单参数 —— `new_arrival`（上架 N 天内，判定基准是 `products.published_at`）/ `price_range`（存在启用变体价格落在区间内）/ `on_sale`（存在启用变体有划线价），未知类型、未知键、类型不符、越界一律拒绝；重算时机只有四条 —— 商品写操作后、变体写操作后、标签定义变更后、显式调用（`POST /api/product/tag/recalc` 与后台按钮），重算按元素摘挂只动自己那一个 tag_id，绝不覆盖手工标签；迁移 091 补 `products.published_at` 与 `product_tags.recalc_at`；后台页 `/admin/product-tags` 可查看每个标签命中哪些商品）；向实体类型注册表注册 `product`（字段白名单 + 构建期字段解析器，可翻译字段按构建语言取译文）；注册商品集合源（#9：`content:product` 的 `CollectionResolver` + `CollectionSchemaProvider` —— 集合源元数据给字段白名单 / 过滤维度 `status` / 排序键 `sort,createdAt`，构建期按构建上下文的工程 ID 取数、按构建语言取译文；白名单外字段与维度在构建期被拒绝）
- `navigation/` — 公开站点导航（与后台 `menu` 严格隔离）
- `plugin/` — 插件体系
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