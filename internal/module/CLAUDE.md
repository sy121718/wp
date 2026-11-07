# internal/module 开发规范

架构判据、层边界、响应协议、取词规则**全仓唯一来源是** [AGENTS.md](../../AGENTS.md)。本文件只写本目录的**结构与真实局部约定**；判据不回述，遇到归属问题回根规则查。

## 目录
```text
<mod>/
  contract/      对外契约：接口 + 不可变 DTO（别的模块只能 import 这里）
  dto/           请求绑定与校验；JSON 响应对象（不含后端替界面拼的成品句）
  enums/         枚举常量、业务错误声明（key + 状态码；HTML 由 Jet 取词，数据 JSON 由共享出口取词）
  model/         表访问单元：本模块表 CRUD、聚合内原子组合、分页筛选
  service/       业务规则、校验、事务边界、聚合与查询（交业务事实）
  inbound/http/  控制器：<mod>_router.go 路由 + <domain>_handle.go 处理器
  inbound/mcp/   AI 工具（读用 mcp.New、写用 mcp.NewWrite）
  outbound/      对外部系统的适配器
```

## 本目录真实局部约定
- 文件名只有两种：`<mod>_router.go`、`<domain>_handle.go`；类型 `<Module>Handle` / `<Module><Domain>PageHandle`。
- 本模块内部按根规则的单向分层依赖（`inbound/http → service → model`，只往下不往回，model 不 import service）；**跨模块**只经对方的 `contract`，不 import 别人的 model/service，也不直接读别人的表。
- service 不持有 `*gorm.DB`、不调 model 的 `DB(ctx)`/`RevisionDB(ctx)`（`scripts/check-service-db-boundary.sh`）；事务句柄从 model 下传。
- `inbound/mcp` 读工具用 `mcp.New`、写工具用 `mcp.NewWrite`。
- 控制器不 import 已清退的 `internal/adminui`；页面渲染与取词经 `pkg/view` / `pkg/view/jet`，业务语义仍归 service。
- 模块内 unit 只测纯逻辑与装配；接口链路走 `public/test/<mod>/feature`（真实 PG）。
- **反向端口是正常接线**，不算反向依赖：调用方持接口、被调方实现——`inventory/outbound/orderstock` 实现 `order` 的 `StockOperator`；`inventory/outbound/purchasestock` 实现 `purchase` 的 `StockPort` 与 `VariantCatalog`；`page` 与 `presentation` 各自实现 `media` 的 `StaleMarker`。
- 带 FORCE 策略的表在缺工程作用域时表现为「不存在」：只有 id 的入口必须**逐工程探测归属**再进事务，不得退化成「不限工程」。
- 存量重构债（控制器侧取词、展示分档在后端、手工读参、旧转发层等）是**待实施清单，不是许可**，见 [TODO.md](../../TODO.md)。

渲染写法、取词出口、分页归属、错误声明位置、测试判据等一律见 [AGENTS.md](../../AGENTS.md)。