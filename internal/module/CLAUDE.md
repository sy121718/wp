# internal/module 开发规范

`internal/module/` 存放业务模块代码。模块直接平铺在本目录下，不区分前后台。

当前已有模块：

- `admin/` — 管理控制面大模块（管理员、角色、权限点、菜单、部门、数据权限）；模块内部同包直调，自包含装配，不拆子模块
- `common/` — 公共业务能力（当前包含验证码）
- `dashboard/` — 需要后端逻辑的后台页面入口（仪表盘、可视化编辑器）
- `media/` — 附件与文件分类

CMS、Visual Builder、构建发布等 go_wp 核心模块按设计文档逐阶段落地，不应在实现前写入"当前已有模块"。

> 管理面六领域（管理员/角色/权限/菜单/部门/数据权限）已合并为 `admin` 大模块：
> 每个领域占 model/dto/handle/service 下的一个文件（如 `role_model.go`、`role_crud.go`），
> service 层同包互调，无跨模块契约与 setter 注入。新增管理面领域时沿用此模式。

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
- `service` — 实现本模块契约，可直接依赖其他模块的 `contract/`，禁止导入其他模块的 `service/model/dto`
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
- 跨模块依赖直接注入目标模块的 `contract` 接口
- 构造函数直接传参，不用 `Deps` 结构体（参数 ≤6 时直传）
- 必须加编译期断言：`var _ <contract>.XXXService = (*Service)(nil)`
- 业务用例拆到 `xxx_<action>.go`
- 返回 `error`，业务错误消息统一取 `enums`
- 使用命名返回值：`func (s *Service) Xxx(ctx, req) (res *XxxResp, err error)`

## 编码风格

- import 别名：`permissiondto`、`adminmodel`、`permissioncontract`
- 函数签名使用命名返回值，`error` 放最后

## model

- 放 Entity + `NewXxxModel(db)` + `DB(ctx)` + 通用查询方法
- 可放本模块固定常量（表名、状态值、API 路径）
- 请求/响应结构放 `dto/`，不放入 `model/`
- `DB(ctx)` 返回 `m.db.WithContext(ctx).Model(&Entity{})`
- 禁止在 model 层写：条件筛选、分页、排序、聚合、多表关联
- 不放业务规则

## outbound

- 用于 RPC / HTTP / MQ / SDK / cache 等外部调用
- **非必需目录**，直接引用对方 `contract` 即可满足需求时不加 outbound
- 实现依赖契约时必须加编译期断言

## 表隔离约定

模块间的数据表严格隔离，不允许跨模块直接关联查询。

### 隔离机制

```text
admin/service
  ├── 持有 admin/model          → 只能碰本模块表
  ├── 持有 contract.RoleReader  → 接口，不知道数据从哪来
  └── 不持有 *gorm.DB           → 无法 .Table() 切表
```

跨模块数据链路：

```text
admin/service → 调 permission/contract.PermissionReader
  → permission/service → permission/model → sys_permission
```

这里强调的是依赖方向：调用方只依赖目标模块的 `contract`，目标模块自行负责其数据访问。

### 规则

- service 层禁止使用 `.Table()` / `.Model()` 切换到非本模块的表
- model 的 `DB(ctx)` 已绑定本表，不得修改
- 跨模块调用统一依赖目标模块的 `contract`
- 调用方禁止直接导入目标模块的 `service`、`model`、`dto`

## 装配

各模块自己负责装配 `model` 和 `service`，顶层 `routes.go` 获取通用依赖（`db`）并按依赖顺序调用模块的 Setup 函数。

对于需要跨模块契约的模块，顶层 routes.go 在调用时从被依赖模块获取契约并传递过去：

```go
permissionServices := permissionhttp.SetupPermissionRoutes(api, db)
adminhttp.SetupAdminRoutes(api, db, permissionServices.Reader)
```

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