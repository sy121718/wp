# internal/module 开发规范

`internal/module/` 存放业务模块代码。模块直接平铺在本目录下，不区分前后台。

当前已有模块（只列一句话边界；完整职责、不变量与迁移细节见 `docs/13-module-inventory.md`）：

- `admin/` — 管理控制面大模块（管理员、角色、权限点、菜单、部门、数据权限）
- `common/` — 公共业务能力（验证码）
- `dashboard/` — 需要后端逻辑的后台页面入口（仪表盘、工作台、媒体库、文章管理、客户管理）
- `media/` — 附件与文件分类
- `project/` — 站点工程、SiteSettings、多主题
- `page/` — 手工 Page 与 Page Document（草稿 / 构建 / 发布 / 回滚 / 改 URL、系统页面槽位）
- `block/` — 复用资产（全局块）：kind + reuse_mode 与 stale 传播编排
- `artifact/` — Artifact 元数据与内容对象闭包
- `build/` — 构建任务队列（调度与可见性，不含编译逻辑）
- `publication/` — URL 占用、激活（两段式回执）、回滚
- `content/` / `contenttemplate/` / `presentation/` — CMS 内容、版本化结构模板、自动发布实例
- `blueprint/` — Page Document 初始化工具（用完即弃）
- `product/` — 商品域（商品 / 变体 / 属性 / 分类 / 品牌 / 标签 / 定价 / 捆绑）
- `product/inventory/` — 仓库 / 库存真源 / 库存变动 / 物料清单 / 货源 / 采购
- `navigation/` — 公开站点导航（与后台 `menu` 严格隔离）
- `plugin/` — 插件体系
- `masterdata/` — 主数据字段级变更记录（append-only）
- `mail/` — 邮箱模块（发信账号 / 邮件模板）
- `user/` — 访问面访客账号（注册 / 登录 / 账号中心；与后台 admin 完全隔离）
- `order/` — 订单（销售侧）：订单头 / 明细 / 状态机 / 支付落账 / 优惠码 / 退货入库
- `cart/` — 购物车与访客结算（状态只在客户端签名 cookie；六个运行时片段能力）
- `analytics/` — 站点访问统计（唯一由访客浏览器写库的路径；后台只读聚合）
- `webhook/` — 外部集成通道（OSS-006）：端点白名单（事件类型 × 目标 URL，管理员预注册）+ 投递日志 + 异步签名投递 worker。对外两个出口：管理面 `contract.EndpointService`（三层链）/ 派发口 `contract.Dispatcher`（一条 `DispatchEvent`，业务模块注入后 best-effort 通知）

> 编译内核在 `internal/builder`，发布内核在 `internal/pipeline`；`build/` 模块只承载**构建任务队列**（调度与可见性），不含任何编译逻辑。
> 模块落地后必须同步更新本列表与 `docs/13-module-inventory.md`（模块实现清单），同一批提交完成，禁止「目录已存在、规则仍写未落地」的漂移。
> `AGENTS.md`「模块现状」只保留一句话级边界，详情一律记在清单里。

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

## 构建期数据源接入（issue #35）

业务模块要给构建器（组件渲染页面时）提供数据，按这个形状接 —— 详细六步见
`docs/04-B-dynamic-development-guide.md` §1.4：

1. 在**本模块 contract 包**声明受限数据源接口（如 `ProductDataSource`）：只嵌
   `source.CollectionResolver` / `source.CollectionSchemaProvider`（可再加
   `source.CollectionFilterOptionsProvider`）—— **写方法不进这个接口**，
   越权防护靠接口形状而不是调用方自觉；
2. 本模块的服务契约**嵌入**它（`type ProductService interface { ProductDataSource; ... }`），
   并加编译期断言 `var _ xxxcontract.XxxDataSource = (*Service)(nil)`；
3. `builder/core` 的 `RenderContext` 加一个字段、builder 加一个 CompileOption，
   装配期注入（片段 / 页面 / 发布三条路径都要接）。

两条死线：

- **共享形状放 `internal/builder/source`，本模块 contract 包不得反向 import
  `builder/core`** —— 反向即成环（`core → 契约 → core`），core 无法再持有业务契约；
- **读取集合项用 `source` 的访问器与字段常量**（`source.ItemFloat(item, source.ItemFieldMinPrice)`），
  不要裸写 `item["minPrice"]`：`ok=false` 表示「没有这个值」而不是「值为零」，
  「没有启用变体」与「0 元」是两回事。

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
```（示意摘录；实际装配已按审计 CQ-008 分段：`internal/routers/routes.go` 只保留入口与顺序调用，各装配段落见 `assembly.go` 与 `assembly_publish.go`，与依赖顺序一致）

## 测试策略（审计 CQ-020）

两层，各管一段，不追求覆盖率数字。

**模块内就近单测**（`internal/module/**/*_test.go`）—— 纯逻辑，不碰数据库、不走装配：
状态机流转表、金额计算、文档校验、路径归一化、错误映射、哈希。
放模块内的理由是**改动成本**：这类逻辑几乎每次改动都会碰到，就近跑一次不到一秒；
扔进 `public/test` 就要先起库，于是最该被覆盖的改动反而最少被跑到。

已覆盖：`order`（状态机穷举边 + 金额）、`page`（发布纯函数、槽位与 kind 枚举）、
`presentation`（模板解析的错误分类）、`blueprint`（文档校验）、
`contenttemplate`（文档校验与哈希）、`analytics`、`cart`、`mail`（自动化图）、
`runtimefragment`、`dashboard`（局部）、`user`（局部）。

**feature / 集成测试**（`public/test/**`）—— 需要数据库、事务、HTTP 或完整装配路径的行为：
`admin`、`artifact`、`block`、`content`、`masterdata`、`media`、`navigation`、`plugin`、
`product`、`project`、`publication`，以及 `page` / `presentation` / `contenttemplate` 的发布链。

判定标准是**「这个判断错了会不会静默出错」**：会（多记一条流转、多标一个 stale、金额舍入、
错误被压成同一句话）就补模块内单测；不会、只是路径走不通，交给 feature 测试。

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