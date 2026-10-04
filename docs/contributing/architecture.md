# 架构

面向要动这块代码的人。只讲「东西放在哪、为什么这么放、改哪一处会牵动什么」。产品定位与冻结边界见 [`../01-overview.md`](../01-overview.md)，这里不重复。

## 1. 系统形状

```text
CMS 内容 + Page / ContentTemplate
              ↓
DocumentSnapshot + BuildContext + Registry
              ↓
Publish Compiler → 不可变 Artifact
              ↓
ArtifactStore → PublicationStore 激活 URL → Static Server / CDN

实时交互 → Runtime Fragment Registry → 受限业务能力 → HTML Fragment
```

控制面（后台编辑）与访问面（公开站点）是两条独立的链路：控制面读数据库、跑模板、走鉴权链；访问面直出已激活的静态产物，**不执行模板、不查数据库**。需要实时数据的功能经受限的 Runtime Fragment 端点提供，都是白名单只读或纯计算能力，不把访客请求带进应用主链路。

定位原句（[`../01-overview.md`](../01-overview.md)）：

> go_wp 是 `CMS + Visual Website Builder + Static Publishing Engine`，不是访问时动态 Theme Runtime，也不是通用 Headless CMS Builder。

单公开 URL 对应 Page 或 PresentationInstance 之一且独占。Article / Product 的公开详情 URL 必须各自关联 PresentationInstance（`Product → PresentationInstance → DocumentSnapshot → Artifact`），**禁止** `Product → Template → Runtime Render`。

## 2. 仓库分层与改动落点

| 路径 | 职责 |
|---|---|
| `cmd/` | 进程入口与生命周期；`-migrate-only` 只跑迁移与 seed 后退出 |
| `config/` | 配置装载（viper；环境变量覆盖 YAML） |
| `internal/routers/` | 顶层装配：路由、中间件链、端口注入、装配自检 |
| `internal/module/` | 业务模块，跨模块只经契约协作 |
| `internal/builder/` | 文档协议、组件注册、受限样式编译、产物生成 |
| `internal/pipeline/` | 发布编排：编译入口、产物存储、URL 激活、访问面 |
| `internal/permission/` | 权限点常量表、声明式路由组、启动期 upsert |
| `internal/templates/` | 后台与工作台 Jet 模板、公共控件、静态资产 |
| `pkg/` | 基础设施（RLS、i18n、缓存、队列、上传、校验…） |
| `public/migrations/` | 版本化 SQL + Go 侧注册 |
| `public/test/` | feature / 集成测试与测试基建 |

### 2.1 模块内三层

```text
internal/module/<模块>/
  contract/        跨模块唯一入口（对外能力 / 跨模块形状 / 索要的端口）
  inbound/http/    router.go + *handle.go：参数绑定 → 调 service → 输出响应
  outbound/<适配谁>/ 需要翻译时才独立子包；只是顺手满足对方端口就留 service 同包
  service/         Service/NewService + 用例文件；只持本模块 model 与契约接口
  model/           Entity + NewXxxModel(db) + DB(ctx)；不放业务规则
  dto/ enums/      跨模块形状、错误码与文案归口
```

三条硬约束：

1. **contract 是跨模块唯一入口**。`service` 不得 `.Table()` / `.Model()` 切到非本模块的表；跨模块拿数据一律经 contract + 不可变 dto。跨模块形状用重导出（`type X = xdto.X`）而不是复制结构。
2. **service 不持 `*gorm.DB`**。它持本模块 model 与契约接口；数据访问留在 model 的具名方法里。这条有门禁兜底（见 [`gates.md`](gates.md) 的 `check-service-db-boundary.sh`）。
3. **两处及以上持久化写入必须同一事务**。跨模块事务不允许，同一业务流程要落同一事务的数据必须在同一模块内。

`service` 需要对方能力时声明的是**收窄端口**（只声明用到的那几条方法），由对方实现并在装配期注入；提供方放 `outbound/<本模块名>/` 并在那里做编译期断言。

## 3. 控制面与访问面分离

控制面两条路由组：

- `/admin` 页面组：Session + CSRF + 权限上下文，渲染 Jet 模板。
- `/api` 业务 API：`authorizedAPI = permission.NewRouteGroup(api.Group("", SessionAuthMiddleware(), CSRFMiddleware(), CasbinMiddleware()))`（`internal/routers/assembly.go`）。中间件链只在这一处定义，模块不得自建同类链。

`permission.RouteGroup` 的意义是「注册路由」与「声明所需权限点」是**同一个动作**：`g.POST("/create", permission.OrderCreate, h.CreateOrder)`。绝对路径由 group 的 BasePath 加相对路径算出，与运行时请求路径天然一致 —— 不存在第二份会漂移的清单。启动期 `permission.SyncToDB` 幂等 upsert 进 `sys_permission` 与超管策略，**只补缺失，不删不改**，因此新增接口不需要写 seed 迁移。

路由动词只用 `GET` 与 `POST`。

访问面装配在 `internal/routers/routes.go` 的 `setupStaticFace`：挂载产物根下的 `active` 目录（`pipeline.ActiveRoot()` 单源），只读直出文件、禁用目录列表；必须**无条件挂载** —— 首次发布发生在启动之后，启动时目录必然还不存在，按「目录存在与否」跳过挂载会让静态访问面永远无法生效。

## 4. 发布管线

一次发布的可验证路径：

```text
Page Document / PresentationInstance
   → DocumentSnapshot（不可变快照）
   → builder.Compile(page, opts...)   ← 唯一编译入口
   → CompiledPage（HTML/CSS/JS + manifest）
   → ArtifactStore（内容寻址持久化）
   → PublicationStore（激活 URL）
```

`builder.Compile` 的调用点只有三处，这正是「手工 Page 与自动 PresentationInstance 共用同一编译与发布管线」的实现证据：

- `internal/pipeline/publisher.go:408`
- `internal/module/page/service/page_assemble.go:245`
- `internal/module/presentation/service/presentation_render.go:340`

注入能力全部经 `CompileOption`（`WithProjectID`、`WithContentResolver`、`WithCollectionResolver`、`WithComponentSet`、`WithProductDataSource`、`WithUISources` 一类），在装配期由顶层把模块契约注入进来。契约包**不得反向 import `internal/builder/core`**；共享的构建期数据形状在 `internal/builder/source`，业务契约包可以依赖它。

两条容易踩的性质：

- **构建确定性**：同一 document、同一 BuildContext、同一 registry 与插件版本集下，产物必须逐字节一致。改渲染路径（哪怕只是换了取数顺序）就是改产物字节，必须有 golden 断言陪伴。
- **Blueprint 用完即弃**：它只初始化页面，后续修改不传播；ContentTemplate 则每次自动构建都参与。两者不是同一类东西。

构件实现变化后已发布的 Artifact **不会自行改变**：启动时可标记待重建页面，再经重建或发布流程更新。

## 5. RLS 作用域

工程隔离的底座是 PostgreSQL 行级安全策略：`pkg/rls` 的 `ScopedPredicate` 用会话变量 `app.project_id` 判定，`GlobalPredicate` 另放行全局行。

- `InProjectScope(ctx, db, projectID, fn)`：把一批读写包进设定了作用域的事务。**必须在事务内设置**（`set_config(..., is_local => true)`），事务外设置会立即失效 —— 所以 `ScopeTx` 在事务外调用会返回 `ErrNotInTransaction`。
- `ScopeTx(tx, projectID)`：给已经打开的事务（例如 model 的 `*Tx` 变体）补作用域，避免另开事务导致「外层未提交数据看不见 + 同表自锁」。
- `BypassedRole(ctx, db)`：探针，用于测试与运维确认当前连接角色是否绕过了 RLS。

切换顺序**不能反**：必须先把未覆盖的读写路径补完，再把 `database.user` 换成非超级角色（`scripts/rls-role-setup.sh` 建角色并授权，含 `ALTER DEFAULT PRIVILEGES`）。先换角色会让未包作用域的路径静默返回 0 行 —— 表现为「有货的仓库被判定可删」这类静默数据错误，而不是报错。切换步骤见 [`../rls-role-cutover.md`](../rls-role-cutover.md)。

漏包作用域的典型形态是**静默失效**：查询成功、状态码 200、日志干净，只是结果集为空或只剩全局行。改任何按 project_id 取数的路径后，跑 `public/test/rls/` 的用例。

## 6. 装配清单：`internal/routers/wiring.go`

装配是**线性**的：后一段依赖前一段构造出的契约，端口注入必须发生在两侧都构造完成之后。`internal/routers/routes.go` 只保留入口与顺序调用，段落细节在 `assembly.go` 与 `assembly_publish.go`：

```text
buildFoundation          基础设施（模板渲染器 / 静态资源 / 访问面 / 健康检查 / db / 权限 seed）
buildAPIAndCoreCRUD      /api 三层链 + 各模块自装配
buildIdentityAndCommerce 身份与交易侧模块
wireProductInventoryPorts 跨模块端口注入
wireRuntimeAccessFace
buildPublishingModules
wirePublishingPorts
wireContentTemplateImpact 模板引用反查
startRuntimeTasks        进程内后台任务
mountAdminPages → runSelfCheck → mountPublicFace
```

拆分只是为了可读性，**不是为了可以重排或并行**。顺序改动表现为「某个端口注入到了空实现上」，而接口断言仍然成立，编译期看不出来。

`wiring.go` 是这张线性装配的自检清单。跨模块能力经「装配期注入的可空端口」（提供方 `SetXxx`、调用方判 nil）连接，每注入一次就 `marks.mark(端口名)`，末尾统一核对。三类 Kind：

| Kind | 含义 | 未注入时 |
|---|---|---|
| `required-contract` | 必需契约（对方模块的主能力） | 装配末尾 fail-fast，一次报出全部缺失 |
| `required-port` | 必需端口（收窄接口，提供方必须实现） | 同上；提供方经 `RequireWiringPort(port, ok)` 当场 panic |
| `optional-degraded` | 可选端口（缺失只降级，不阻断启动） | 写启动日志（Error 级），属**可见降级** |

`wiringEntry` 里真正有价值的是 `Consequence` 字段：必须写清「未注入 → 用户或运维看到什么」。只写端口名等于没写 —— 排查时看不出降级的具体后果。端口名用文件头部 const 块里的常量（`routes.go` 只经常量 mark），手写字符串拼错会让自检永远通过。

相关函数：`newWiringMarks()` / `marks.mark(port)` / `CheckWiring(marks)`（返回 missing 与 unknown，按名排序）/ `mustAllPortsWired(marks)`（装配末尾 fail-fast）/ `logDegradedOptional(marks)` / `RequireWiringPort(port, ok)`。

装配末尾还有一道 `runSelfCheck`：核对运行时装配出的路由表与快照一致（`WP_DUMP_ROUTES=1 go test ./internal/routers/` 可导出这张表）。拆大文件时的等价性证据就是它。

## 7. 不变量速查

1. 控制面与投递面分离：公开访问不执行模板、不查数据库。
2. 手工 Page 与自动 PresentationInstance 共享同一编译器、产物存储与发布存储。
3. Blueprint 用完即弃；ContentTemplate 每次自动构建都参与。
4. Binding 是白名单引用，不是查询语言。
5. 同一 document / context / registry / 插件版本集下构建确定性，产物逐字节可复现。
6. 共享构建期数据形状在 `internal/builder/source`；业务契约包可依赖它，不得反向依赖 `internal/builder/core`。
7. 跨模块只经 contract；service 不持 `*gorm.DB`；同一业务流程的多次写入同一事务。

改任一处的操作步骤见 [`extend-a-module.md`](extend-a-module.md)；这些不变量由哪些门禁守着见 [`gates.md`](gates.md)。
