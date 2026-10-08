# internal/module 开发规范

`internal/module/` 存放业务模块代码。模块直接平铺在本目录下，不区分前后台。

> **模块清单的权威在 [`docs/13-module-inventory.md`](../../docs/13-module-inventory.md)** —— 各模块的完整职责、
> 不变量与落地细节都在那里，本文件**不重复模块列表**（两份真源必然漂移）。新增 / 更名模块时先改那份清单。
> 编译内核在 `internal/builder`，发布内核在 `internal/pipeline`，`build/` 模块只承载**构建任务队列**。

## 目录结构

```text
module_name/
├── contract/                   # 本模块对外暴露契约（<module>_service.go）
├── inbound/http/               # router 只注册路由；handle 做绑定、装配、Jet 渲染
├── outbound/<适配谁>/          # 按需：形状翻译层（见下方判据；顺手能满足端口就留 service 同包）
├── service/                    # 同目录多文件。一个关注点一个文件，不按方法拆，下面不再开子目录
├── model/                      # <module>_model.go
├── dto/                        # <module>_req.go + <module>_resp.go
└── enums/                      # 必选：用户可见文案（响应消息、错误消息、展示标签）
```

- `contract/` — **跨模块可见的东西都放这里**，三段分工：
  · **对外能力**：`XxxService`（及其嵌的收窄子接口）—— 别的模块能用本模块做什么；
  · **跨模块形状**：调用方要传进来 / 收回去的类型 —— 本模块 dto 的**重导出**（`type X = xdto.X`），
    不另造一组「契约自有形状」（两份必须逐字段等价的定义 = 把「一处改、调用方编译错」换成「一处改、另一处静默分叉」）；
  · **索要的端口**：本模块需要外部给什么（`XxxPort` / `XxxReader` / `XxxSource` / `XxxResolver` / `XxxEnqueuer` …）
    —— **由对方实现**（对方放在 `outbound/<本模块名>/` 并加编译期断言），入参形状在这里自有、
    **不借用对方的 dto**：借了就等于本模块认识了对方的绑定层，对方改一个请求字段本模块跟着编译错（审计 CQ-004）。
  判据：**跨模块可见即入契约**；只在本模块内流转的实现形状不进。
  跨契约引用（契约 import 别的模块契约）**只允许用于传递形状**（如 `blockcontract.BlockUsage` 作返回类型）；
  要把对方的能力接口直接透出自定义方法签名时必须写明理由并确认它是收窄过的。
- **契约的文件怎么分**服从下面「文件怎么分」。契约包里多一个文件，只因为那是另一条契约：
  · **收窄端口**（给某个窄消费方的越权防护面）：`content/search.go`（匿名检索）、`user/customer_admin.go`
    （后台客户管理，不并进访客侧的 UserService）、`user/visitor_account.go`、`presentation/published_locator.go`
    —— 文件名就是那条边界的名字。并回主服务等于让片段层在主接口上看见写方法；
  · **构建期数据源**：`data_source.go` 是各模块统一的约定落点（issue #35）；
  · **能力域**：会独立演进的一组形状 + 接口，如 `product/variant_stock.go`、`product/variant_cost.go`。
  · 错误哨兵、单个类型定义留在所属契约文件里。按「类型」再开一个文件，读一个能力要翻两个文件。
  · 单文件内部按三段顺序排列（对外能力 → 跨模块形状 → 索要的端口）。一个文件里有多段时用 `// ===` 分段
    （`order/contract` / `page/contract` 是样板）。
- `inbound` — 承接外部调用 · `service` — 实现本模块契约 · `model` — 持久化与表访问
- `outbound/<适配谁>/` — **非必需**，是**形状翻译层**，判据是「要不要翻译」而不是「依不依赖外部」：
  · 需要**翻译**（换字段 / 拼多个来源 / 换数据源 / 换实现）→ 独立子包，目录名说清适配谁
    （`outbound/orderstock` = 给订单的库存适配、`outbound/source` = 菜单项的来源解析）；
  · 只是**顺手满足**对方端口（签名对得上、用的还是自己的 model）→ **留在 `service` 同包** + 编译期断言
    （`order/service` 里 `HasPurchasedProduct` 实现 `productcontract.PurchaseChecker` 就是这类，多包一层反而要暴露内部）；
  · 同一契约的**多个实现**（假通道 / 真通道）→ 一个实现一个子包（`cart/outbound/mockpaypal`，真通道来了加兄弟目录，service 不动）。
  纯 RPC / HTTP / MQ / SDK 客户端同此判据。**不要把所有适配器并进一个 `outbound` 包**：包名会退化成位置词，
  「适配谁」这条信息从目录名里消失，且不同性质的适配同包互相可见。
- `dto` — 请求/响应结构，数据流 `inbound -> service -> inbound`。
  金额、折扣、状态的**展示串**在 service 组装响应时写进 dto（`Label` / `State`）。
  页面、片段、MCP 文案只读这些字段：分与元的换算、「现在算不算生效」的解释，不在 inbound 再做一遍。
  币种符号取 `sys_dict`，不写死在代码里。
  同一份字段表只保留一份。MCP 入参与 dto 逐字段重合时直接用 dto；形状不同才在 mcp 包另声明。
- `enums` — **必须存在**，只放用户可见文案

## 核心关系

- `service` 可直接依赖其他模块的 `contract/`（及不可变 `dto/`），**禁止导入其他模块的 `service/model`**
- 跨模块依赖直接注入目标模块的 `contract` 接口，并加编译期断言
  `var _ <contract>.XXXService = (*Service)(nil)`
- 实现依赖契约时（outbound 适配器）同样要加编译期断言

## 文件怎么分

`contract` / `service` / `inbound` 用同一条。

**一个文件 = 一个关注点。** 关注点看两件事：谁消费它，以及改一处时另一处是否必须一起改。
同一批调用方、被同一条口径绑在一起的方法，放同一个文件。

**同包 = 同一目录。** Go 里子目录就是另一个包，调用必须 `import`，做不到「同包直接调用、只是换个目录存放」。
本层复用的函数放在该层目录的另一个文件里。单独成包只有两种情况：包外也要调用，或者必须零依赖才能避免成环
（先例 `internal/builder/source`）。

这些都不是拆文件的理由：

- 原始行数。注释和空行算进行数，按行数拆等于奖励删掉设计理由。
- 一个文件里有几个方法。三个各一百行、同一个关注点的方法留在一个文件里。
- 一个方法很长。长方法是内部混了步骤，在同文件里抽成函数，不搬到新文件。

新开一个文件，是因为它是另一条业务用例。一个方法一个文件，默认就是拆错了，要并回去。
实现别的模块声明的端口也并进对应用例，不单独占文件。

拆文件时留等价性证据：全目录 `func` 集合一致、函数体指纹不变。对比范围是整个目录，
只比新文件会把没动过的文件算成差异。

## inbound/http

- `*_router.go` 获取 `db`、创建 `model` 与 `service`、注册路由。路由注册（`.GET(` / `.POST(`）只出现在 router 文件里。
- `handle` 负责参数绑定、调用 service、**输出响应**。Jet 渲染在 handle，不在 router。
  页面视图形状和回填字段清单留在 handle（它们和模板做双向断言），不进 `enums`，不进 `dto`。
- 一个页面的 handler、查询、视图、回填在同一个文件里。方法短不是再拆文件的理由。
  多个页面共用的展示映射可以单独一个文件。JSON API 与页面分开，各自一个文件，不再按域拆。
- `inbound/mcp` 一个模块一个文件：工具注册、入参、回给模型的文案都在里面。
- 响应消息统一取 `enums`
- 取当前操作人一律用 `shell.CurrentUserID(c)` / `shell.CurrentUserIDText(c)`（内部走 `builtin.GetUserID`，
  带类型断言保护 —— 裸断言在类型异常时会把请求打成 500）。只有确实要区分「未登录 → 401」与
  「会话值类型异常 → 500」时才直接读 `c.Get("user_id")`，并写明理由。
- **列表页批量操作（多选后删除 / 改状态 / 审批）必须成对遵守两条**：
  · id 一律经 `shell.BulkIDs(c)` 读取，不要直接 `c.PostFormArray("ids")` —— 它负责去空白、去重与数量上限
  （`MaxBulkIDs`），超限**整批拒绝**并把可展示的原因回带列表页。直接取数组等于把循环次数交给请求方决定。
  · 逐条走各自的单条路径、**单条失败不中断整批** —— 一条失败就整批回滚会让人以为「一条都没删」然后反复
  重试；结论按「成功 N / 跳过 M」回带，不允许静默的部分成功。

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

- `xxx_service.go` 只放 `Service` / `NewService()` 与全用例共享的工程定位。
  业务按用例整合，一条用例一个文件：建单与状态、查询、概览、客户、优惠券、退货。
  一个方法一个文件要并回它所属的用例。
- `Service` struct 只持有本模块 `model` + 契约接口，**不持有 `*gorm.DB`**
- 构造函数直接传参，不用 `Deps` 结构体（参数 ≤6 时直传）
- 返回 `error`，业务错误消息统一取 `enums`；使用命名返回值
  `func (s *Service) Xxx(ctx, req) (res *XxxResp, err error)`
- **写操作必须有事务边界**：一个方法里出现**两处及以上持久化写入**就必须包进同一事务，任一步失败整体回滚。
  完整判据与门禁见 `AGENTS.md` §「写操作的事务与回滚」

## 编码风格

- import 别名：`pagedto`、`adminmodel`、`pubcontract`（模式：`<模块名小写>dto/model/contract/enums`）
- 函数签名使用命名返回值，`error` 放最后

## model

- 放 Entity + `NewXxxModel(db)` + `DB(ctx)` + 通用查询方法；可放本模块固定常量（表名、状态值、API 路径）
- `DB(ctx)` 返回 `m.db.WithContext(ctx).Model(&Entity{})`
- 查询条件、分页、排序以**参数**传入方法（仅限本模块表）；方法内不得写死业务条件，不得多表关联
- **不放业务规则**（状态机、归属校验等留在 service）；请求/响应结构放 `dto/`，不放入 `model/`

> **model 层定位（Repository，非 DDD Domain Model）的权威版本在 `AGENTS.md`** —— 允许/禁止边界、
> `DB(ctx)` 的使用限制、admin 豁免条款、评审拦截项都在那里。本文件不重复摘录。

## 表隔离约定

模块间的数据表严格隔离，不允许跨模块直接关联查询。

```text
page/service
  ├── 持有 pagemodel.Model                → 只能碰本模块表
  ├── 持有 pubcontract.PublicationService → 接口，不知道数据从哪来
  └── 不持有 *gorm.DB                     → 无法 .Table() 切表

跨模块：page/service → 调 pubcontract.PublicationService
                    → publication/service → publication/model → publication 路由表
```

- service 层禁止用 `.Table()` / `.Model()` 切换到非本模块的表
- model 的 `DB(ctx)` 恒绑定本模块表，不得重绑定；service 不得调用它（见 AGENTS.md）
- 跨模块调用统一依赖目标模块的 `contract`；可传递类型是 `contract` 与**不可变 dto**，
  **禁止导入目标模块的 `service` / `model`**

## 装配

各模块自己装配 `model` 与 `service`；顶层 `routes.go` 获取通用依赖（`db`）并按依赖顺序调用模块 Setup 函数。
需要跨模块契约时，由顶层从被依赖模块取契约再传入：

```go
projectService := projecthttp.SetupProjectRoutes(authorizedAPI, db)
blockSvc := blockhttp.SetupBlockRoutes(authorizedAPI, db, projectService)
```

> 实际装配已按审计 CQ-008 分段：`internal/routers/routes.go` 只保留入口与顺序调用，
> 各装配段落见 `assembly.go` 与 `assembly_publish.go`，与依赖顺序一致。

### 数据域（datarule）注册

**域声明属于拥有该表的模块**，注册动作在**装配入口**完成（且必须在**注册路由之前** —— 域没注册上的表不会被
任何规则拦住）。白名单写在实体字段的 tag 上，没有 tag 的字段不可配（fail-closed）：

```go
DeptID uint64 `gorm:"column:dept_id" datarule:"label=所属部门;ops=EQ,NEQ,IN,NOT_IN"`
```

域值由 model 暴露（如 `adminmodel.AdminDataRuleDomain()`）。声明写错（未知操作符、缺 label、字段名非法）
一律**装配期失败**，不要降级成「这个域没有白名单」。完整规则见
[`docs/rules/database.md`](../../docs/rules/database.md)。

## 构建期数据源接入（issue #35）

业务模块给构建器（组件渲染页面时）提供数据，按这个形状接 —— 详细六步见
[`docs/04-B-dynamic-development-guide.md`](../../docs/04-B-dynamic-development-guide.md) §1.4：

1. 在**本模块 contract 包**声明受限数据源接口（如 `ProductDataSource`）：只嵌 `source.CollectionResolver` /
   `source.CollectionSchemaProvider`（可再加 `source.CollectionFilterOptionsProvider`）——
   **写方法不进这个接口**，越权防护靠接口形状而不是调用方自觉；
2. 本模块服务契约**嵌入**它（`type ProductService interface { ProductDataSource; ... }`），
   并加编译期断言 `var _ xxxcontract.XxxDataSource = (*Service)(nil)`；
3. `builder/core` 的 `RenderContext` 加字段、builder 加 CompileOption，装配期注入
   （片段 / 页面 / 发布三条路径都要接）。

两条死线：

- **契约包不得反向 import `builder/core`**（反向即成环，core 无法再持有业务契约）；
- **读取集合项用 `source` 的访问器与字段常量**（`source.ItemFloat(item, source.ItemFieldMinPrice)`），
  不要裸写 `item["minPrice"]`：`ok=false` 表示「没有这个值」而不是「值为零」——
  「没有启用变体」与「0 元」是两回事。

## 测试策略

两层，各管一段，**不追求覆盖率数字**。完整方法论见 [`docs/rules/testing.md`](../../docs/rules/testing.md)。

- **模块内就近单测**（`internal/module/**/*_test.go`）—— 纯逻辑，不碰数据库、不走装配：状态机流转表、
  金额计算、文档校验、路径归一化、错误映射、哈希。放模块内的理由是**改动成本**：这类逻辑几乎每次改动都会
  碰到，就近跑一次不到一秒；扔进 `public/test` 就要先起库，于是最该被覆盖的改动反而最少被跑到。
- **feature / 集成测试**（`public/test/**`）—— 需要数据库、事务、HTTP 或完整装配路径的行为。
- 判定标准是**「这个判断错了会不会静默出错」**：会（多记一条流转、多标一个 stale、金额舍入、错误被压成
  同一句话）就补模块内单测；不会、只是路径走不通，交给 feature 测试。

## 响应与路由

| 请求类型 | 响应格式 |
|---|---|
| HTMX 请求（`HX-Request: true`） | Jet 渲染的 HTML 片段（列表刷新、局部更新、弹窗内容） |
| JSON API | `pkg/response` 的 `Response{Code,Message,Data}` |

- JSON 统一走 `pkg/response`（`Success` / `SuccessWithMessage` / `ErrorWithMessage`），消息来自模块 `enums`
- HTMX 响应直接渲染 Jet 片段返回，**不走** `pkg/response`
- 路由**只用 `GET` / `POST`**：`GET` 查询，`POST` 用于新增、修改、删除、状态变化

## enums

- `enums/` 是**必须目录**，只放**用户可见文案**：成功消息、参数错误、未授权、业务错误，以及状态 / 分段的展示标签
- 展示标签的形状是 `XxxLabel(value) (key, fallback string)`：i18n key + 中文兜底都要有，key 必须能在 seed 里找到
- 不放这些：表单字段清单（在 handle）、长度上限（在做校验的 service）、值域白名单（在拥有该值的 model）
- `handle` 和 `service` 不直接硬编码响应文案
- 未接好 `i18n` 时，`ErrXxx` / `MsgXxx` 直接等于中文常量
