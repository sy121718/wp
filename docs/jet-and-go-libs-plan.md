# Jet v6 全面解析与融入方案 + Go 库规划

> 本文回答两个问题：① Jet v6 能力全貌及如何完整融入 go_wp；② 项目 Go 依赖库的审计与规划。
> 与 `docs/component-jet-migration-plan.md` 互补：那份是「组件渲染迁移的具体步骤」，这份是「Jet 能力地基 + 依赖面治理」。

---

## 第一部分：Jet v6 全面解析与融入方案

### 1. 能力全景（实测 `github.com/CloudyKit/jet/v6@v6.3.2` 源码）

| 能力 | 语法 / API | 说明 |
|---|---|---|
| 模板继承 | `{{ extends "./layout.html" }}` | 布局骨架复用 |
| 组合-块 | `{{ block body() }}...{{ end }}` / `yield` | 子模板填充父模板槽位 |
| 组合-引入 | `{{ import "..." }}` / `{{ include "..." }}` | import 引入函数/块，include 内联渲染 |
| **运行时动态 include** | `{{ include name .Ctx }}` | 模板名运行时求值（可变量），`eval.go:executeInclude` |
| **递归 include** | 自引用模板 | `eval_test.go:844` 有自引用测试；`includeDepth >= 100_000` 深度保护 |
| **include 上下文传递** | `{{ include "child.jet" .Value }}` | 子模板 `.` 即传入值，`st.context` 切换 |
| 自动转义 | `{{ .Text }}` | 默认 HTML 转义 |
| 原始 HTML | `{{ unsafe(.X) }}` | 显式声明「已安全，原样输出」 |
| 条件/循环 | `{{ if }}` / `{{ range }}` | C 风格表达式 |
| 全局函数注入 | `Set.AddGlobal(key, any)` / `AddGlobalFunc(key, Func)` | 运行时给模板塞函数/变量 |
| 模板缓存 | `WithCache(Cache)` / `DevelopmentMode(bool)` | dev 禁用缓存热改，prod 缓存 |
| 安全写入 | `WithSafeWriter(SafeWriter)` | 拦截输出（可写 strings.Builder） |
| 定界符/扩展名 | `WithDelims` / `WithTemplateNameExtensions` | 自定义 `{{ }}` 与 `.html` 后缀 |

**关键结论**：Jet 是「可编程模板引擎」而非「静态文本模板」——include 动态 + 递归 + 上下文传递 + 全局函数注入，这四点是它能承担「组件树渲染」的根因。

### 2. 项目现状接入（`internal/templates/jet_render.go`）

**已实现**：
- 单 `*jet.Set` + `OSFileSystemLoader` + `DevelopmentMode(isDev)` + `WithTemplateNameExtensions([".html"])`
- 封装 `gin.HTMLRender`，后台 `admin/*.html` + 工作台 `workbench/layout.html` 用它渲染
- `t.Execute(w, nil, data)` 写响应，`WriteContentType` 设 text/html

**缺口（融入差距）**：

| 缺口 | 影响 |
|---|---|
| 组件渲染未用 Jet | builder 仍 Go 拼字符串（迁移计划解决） |
| **单 Set 无隔离** | admin / workbench /（未来）组件 共用一套模板空间，命名易冲突、dev 缓存策略无法区分 |
| **无 AddGlobalFunc 注入** | 后台模板只靠 data 传值，缺格式化/URL 生成等复用函数 |
| **构建期无独立缓存 Set** | 组件模板要进构建路径，需非 dev 缓存的专属 Set |
| **无 SafeWriter 复用** | 构建期要写 strings.Builder，当前只写 HTTP ResponseWriter |

### 3. 融入方案：三层 Set + 统一全局函数

```
Jet 运行时按「用途」隔离为三个 Set：

setAdmin      = jet.NewSet(adminLoader,      DevelopmentMode(isDev))
setWorkbench  = jet.NewSet(workbenchLoader,  DevelopmentMode(isDev))
setComponents = jet.NewSet(componentLoader,  WithCache(memCache))   ← 构建期，非 dev 缓存

loader 用 jet.NewOSFileSystemLoader 指到 internal/templates/<子目录>
（或 embed.FS，构建期组件模板推荐 embed 避免文件路径耦合）
```

**统一全局函数注入（三个 Set 都 AddGlobalFunc）**：

| 函数 | 用途 |
|---|---|
| `formatNumber` / `formatBytes` | 后台列表格式化 |
| `assetURL(assetID, variant)` | 媒体地址生成（后台缩略图、组件媒体） |
| `sanitizeHTML` | 复用 builder 的富文本白名单（组件模板用，与 Go sanitize 同源） |
| `attr` / `unsafeHTML` | 动态属性安全拼接 |
| `i18n(key)` | 未来多语言 |

**组件 Set 的融入**（配合迁移计划）：
- `setComponents` 缓存模式，`nodeView` 树 + `node.jet` 递归入口
- 构建路径 Execute 到 `strings.Builder`（`WithSafeWriter` 或直接传 buf），HTML/CSS 分离
- 转义策略：普通文本走默认转义，仅 sanitize 后内容 + 动态 attrs 用 `unsafe`

### 4. 融入注意事项

1. **dev / prod 边界**：`DevelopmentMode(true)` 只给 admin/workbench（热改模板）；构建路径**强制非 dev**（缓存），加测试断言防误入。
2. **SafeWriter**：构建期写 `strings.Builder`（复用），HTTP 写 `ResponseWriter`——同一套 Set 可配不同 SafeWriter。
3. **转义纪律**：`unsafe` 只允许出现在「sanitize 后内容」和「Go 拼好的安全属性串」两处，其余一律默认转义。
4. **错误上抛**：`Execute` 的 error 在构建期必须阻断构建（确定性 + 一致性），在 HTTP 渲染必须转 500。
5. **递归安全**：Jet 有 100000 深度保护，但组件树由 `nodeView` 预先转好（非自引用），实际深度 = 组件树深度，远低于上限。

---

## 第二部分：Go 库规划

### 1. 直接依赖审计表

| 库 | 状态 | 说明 |
|---|---|---|
| `CloudyKit/jet/v6` | **保留·核心** | 模板引擎，融入重点（见第一部分） |
| `gin-gonic/gin` | 保留 | Web 框架 |
| `gorm.io/gorm` + mysql/postgres | 保留 | ORM + 主库驱动 |
| `casbin/v3` + gorm-adapter | 保留 | 鉴权（Enforce） |
| `spf13/viper` | 保留 | 配置 |
| `go.uber.org/zap` | 保留 | 日志 |
| `hibiken/asynq` + `go-redis/v9` | 保留 | Build Worker 任务队列（Redis broker） |
| `alicebob/miniredis/v2` | 保留 | 测试 mock |
| `go-playground/validator/v10` | 保留 | Gin 参数校验 |
| `golang-jwt/jwt/v5` | **已彻底移除（含 indirect）** | 认证已迁 Session+Cookie，`pkg/auth/jwt.go` 已删除，本项目代码零引用；sqlserver 驱动移除后连 indirect 传递依赖也已清除，go.mod / go.sum 均无 `golang-jwt` |
| `glebarez/sqlite` + `modernc.org/sqlite` | **已移除** | `pkg/database/driver/sqlite.go` 已删除，go.mod 已无 sqlite 驱动（主库为 PostgreSQL，测试基建也已迁本地 PG） |
| `gorm.io/driver/sqlserver` + mssqldb | **已移除** | `pkg/database/driver/sqlserver.go` 已删除，go.mod 已无 sqlserver 驱动（按 §2 决策执行） |

### 2. 数据库驱动面的矛盾点（需决策）

当前 `pkg/database/driver/` 有 **4 个驱动**：mysql / postgres / sqlite / sqlserver。

| 文档出处 | 声称 |
|---|---|
| CLAUDE.md 技术栈表 | MySQL |
| dbx MCP 约定 | PostgreSQL 默认（127.0.0.1:5432） |
| config.yaml 实际 | PostgreSQL（wp 库） |

**决策建议（✅ 已执行）**：实际部署是 PostgreSQL，文档三处口径已统一为「PostgreSQL 主库 + MySQL 兼容（历史）」；sqlite / sqlserver 驱动已从 `pkg/database/driver/`（现仅 mysql / postgres）与 go.mod 完全移除，与 go.mod 现状一致。

### 3. 内部库开源拆分建议（builder 面）

若要把 builder 抽成可开源/可复用的库（当前在 `internal/`，Go 语义不对外）：

```
builder（顶层编排）
  ├─ core        组件基座：Node/Registry/Atom/CSSBuckets/controls/icons/render 上下文
  │               → 零外部依赖（仅 stdlib + jet），可独立开源
  ├─ media       媒体契约 resolver（contract_resolver）
  │               → 依赖 core
  ├─ components/ 18 个组件实现
  │               → 依赖 core + media
  └─ builder      ParsePage/ValidatePage/Compile/RenderDocument/ComponentSchemas
                  → 依赖以上全部
```

**公开 API 建议**（拆出 `internal` 后）：
- `builder.ParsePage` / `ValidatePage` / `Compile` / `RenderDocument`
- `builder.ComponentSchemas()`（检查器 schema）
- `core.Register` / `core.Lookup` / `core.Types`（扩展点）
- `media.ContractResolver`（媒体契约 → 构建期 URL）

**依赖方向纪律**（沿用 CLAUDE.md）：
- `components → core`，`core` 不反向依赖组件
- `core` 仅 stdlib + jet，**不引 gin/gorm**（构建期与 Web 层解耦）
- 构建期依赖（jet）**不进访问面** runtime fragment

### 4. 依赖治理原则（落地 checklist）

1. **控制面/访问面依赖隔离**：构建期引 jet，访问面 runtime fragment 不引 jet/gorm。
2. **清理遗留**：JWT→Session 迁移完成后删 `jwt/v5`；确认 sqlite/sqlserver 去留。（✅ 已执行：三者均已从 go.mod / go.sum 与 `pkg/database/driver/` 移除）
3. **文档口径统一**：数据库主库统一 PostgreSQL，修正 CLAUDE.md 技术栈表。
4. **builder 解耦**：`core` 只依赖 stdlib + jet，为未来抽库/开源铺路。
5. **go 版本**：`go.mod` 声明 `go 1.26`，确认 vfox 管理的 Go 版本与之匹配（避免 CI/本地版本漂移）。

---

## 待办优先级建议

> 状态核对（2025-09）：P0 组件 Jet 化、P2 JWT 清理、P2 驱动去留、P1 数据库口径均已落地；仅剩 P1 三层 Set 与 P3 抽库两项待办。

| 优先级 | 事项 | 依据 | 状态 |
|---|---|---|---|
| P0 | 组件渲染 Jet 化（迁移计划 Phase 0~3） | 架构回归 CLAUDE.md 约定 | ✅ 已完成（18 组件全 Jet，见 component-jet-migration-plan） |
| P1 | 三层 Set 隔离 + AddGlobalFunc 注入 | Jet 融入地基 | 待办 |
| P1 | 数据库文档口径统一（PostgreSQL） | 三处矛盾 | ✅ 已执行（go.mod / driver 面已按决策收敛） |
| P2 | JWT→Session 迁移收尾 + 清 jwt 依赖 | CLAUDE.md 已定 | ✅ 已完成（golang-jwt 已不在 go.mod / go.sum） |
| P2 | sqlite/sqlserver 驱动去留决策 | 依赖精简 | ✅ 已执行（驱动已移除） |
| P3 | builder/core 抽库（拆 internal） | 开源复用 | 待办 |
