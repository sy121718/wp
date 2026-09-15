# SEO-025 侦察结论（子代理 fe474e0e，2026-09-14）

审计条目 SEO-025 的 remediation 假设「从 page_routes 的 301 类型路由读全部重定向」，
但**重定向目标根本不在数据库里**，因此该 remediation 无法照字面实施。

## 三条硬事实

1. `page_routes` 由 publication 独占（`publication/model/publication_model.go` 的 TableName）；
   page 侧源码显式声明不碰该表（`page/model/page_model.go:363-364`）。
   枚举值 reserved / active / redirect（DDL CHECK 见 `migrations/init_builder_schema.sql:166`）——
   审计说的「301 类型路由」即 `route_kind = redirect`。
2. publication contract **没有**所需入口：只有 Redirect（标记单条）、Deactivate（按 path 取消）、
   DeleteRoutesByPage/ByPresentation（按归属全清）、ListActivePaths（返回 active+redirect 的 path，
   不带 kind、不带目标）、IsPathOccupied（布尔）。读不到完整 redirect 清单，也删不掉单条重定向行。
3. **重定向目标不在 DB**：`RedirectReq` 无目标字段，page 侧 `ensureRedirectRoute`
   调 Redirect 时连 ArtifactID 都不传（注释「重定向产物不入库」）。目标只存在于
   `artifacts/redirects/<sha256>/redirect.json` 的 `RedirectDirective{TargetPath, StatusCode}`，
   经 active 目录符号链接生效。pipeline 只能按单路径 Inspect，没有枚举 API。
   → 沿链走到底、A→B→C→A 检环所需的 path→target 全表同样拿不到。

## 解锁条件（需 publication 侧先补契约）

- `ListRedirectRoutes(ctx, *ListRedirectRoutesReq{ProjectID}) ([]RedirectRouteResp, error)`：
  Resp 含 Path / TargetPath / StatusCode / PageID / PresentationID / UpdatedAt / 产物是否缺失。
  实现用 RouteDB 取 `route_kind = redirect` 行，再对每条 path 调
  `LocalPublicationStore.Inspect(path)` 读 redirect.json 补 TargetPath。
- `DeleteRedirectRoute(ctx, *DeleteRedirectRouteReq{ProjectID, Path}) error`（删路由行 + 产物与符号链接清理）。
- 扩展 `RedirectReq` 加 `TargetPath`：现有 `Redirect` 的语义是「旧路径 → 该归属者当前激活路径」，
  无法表达手动指定任意目标（营销短链）。
- 装配侧：`SetupDashboardRoutes` / `dashboardhttp.NewHandle` 目前不接收 publication 契约，
  页面要拿到它必须改 `internal/routers/routes.go`。

## 为何暂停而不是降级实现

代理提出过「只读列表」降级版（遍历 ActiveRoot 读 redirect.json，绕开 page_routes），
但那与 remediation 指定的数据源不符，且**无法增删**，不满足 verification
（「能看到并管理全部重定向 / 成环的重定向被拒绝」）。硬凑一个只读页会把
「能看不能改」当成完成，比留着 open 更糟。

**排期**：等 publication 目录空闲（SEO-012 代理正在改它）后再安排；
届时契约方法可能已随那批改动就位。
