# AGENTS.md

go_wp = CMS + Visual Website Builder + Static Publishing Engine。**本文件是全仓唯一规则源**：架构判据、层边界、流程、门禁。
其他文档只引用本文件，不复制、不派生第二份判据。产品背景见 [README.md](README.md) / [goal-and-requirements.md](goal-and-requirements.md)。

开发阶段不做兼容：旧形状有问题就直接改掉——同步修改调用方与过期测试、删掉旧接口/适配层/双轨实现，不拿「时间紧」「重构成本高」维持错误架构。

## 架构

**MVC + Service**：

```text
View      internal/templates/**  Jet 模板 + HTMX（结构 / 样式 / 显示格式 / 取词）
Controller internal/module/<mod>/inbound/http  解析请求 → 调 Service → 选模板 + 带业务 data / 交 JSON
Service    internal/module/<mod>/service  业务规则、校验、事务、聚合与查询
Model      internal/module/<mod>/model    本模块表访问
```

- **Go = 后端**（`internal/**` 与 `pkg/**`、`cmd/**` 中的业务与控制代码）；**Jet 模板 + HTML/JS = 前端**（`internal/templates/**` 的模板与静态资产、`public/static/**`）。
- `internal/templates/**` 里现有的 Go（Jet 引擎适配 `jet_render.go`、模板函数 `funcs.go`、嵌入 `*_embed.go`）属**共享渲染基础设施**，不是业务后端代码，也不是可以在模板目录里写业务逻辑的许可。
- 前端开发体验对标 Laravel Blade 的易用性（模板继承 / 布局 / 组件复用 / 全局取词 / 表单与统一交互能力）。这只是**体验目标**，不引入 Laravel 依赖、不照搬其全部功能、不在本仓再加一层包装链。
- 依赖方向单向：`cmd → routers → module/* → pkg/*`；`pkg` 不 import `internal`；`contract` 不被越级穿透。

## 语言

回复、分析、计划、报告、注释一律简体中文；代码标识符、命令、上游数据保留原文。

## 常用命令

```bash
go run cmd/main.go
go build -o app ./cmd        # 生产构建用包路径，且在 git 工作区内执行
go test ./...                # 全量（可 -race）
go vet ./...
bash scripts/check-all.sh    # 门禁全集（--with-db 含需要库的那组）
```

## 查证（不确定就先查，禁止凭记忆断言）

任何「某个库/框架能不能做 X」「它的签名/行为是什么」的判断，三档依次走，命中即停：

1. **本机源码与文档**：`$GOMODCACHE/<mod>@<ver>/`（README、`docs/`、`_test.go` 里的用法）—— 版本与 `go.mod` 对齐，最可信。
2. **context7**：本地未命中时按 `context7.json` 的映射查（`jet → /cloudykit/jet`），接口 `https://context7.com/api/v1/<owner>/<repo>?topic=..&tokens=..&type=txt`；查到新事实回填该文件。
3. **最小可跑样例**：文档说不清时写 10 行 `go run` 实测，把输出贴进结论。

判据：结论里出现「某库不支持 / 做不到 / 只能这样」而手里没有文档路径或样例输出 → 判失败。

## 响应协议

| 场景 | 形态 |
|---|---|
| 后台页面与局部界面请求 | 控制器指定真实 Jet 模板 + 业务 data，返回 HTML；完整页面与局部更新复用同一份区域结构 |
| 界面请求失败 | 真实状态码 + Jet 错误模板；局部失败只进入发起宿主声明的错误区，保留输入 |
| 数据消费者（外部 API、MCP、编辑器结构化数据） | JSON `{code, message, data}` + 真实状态码 |
| 独立文档与字节流（编译预览、下载、图片、SSE、采集 204） | 各自明确协议；已写 body 后用 `response.ErrHandled` 表示已处理 |

- 浏览器链接/表单 → Controller 原生绑定与基础校验 → Service → Controller 选模板并传业务事实 → HTMX 更新 Jet 声明的区域。
- **Jet 定义界面，Go 不写界面**：标签、属性、文案、界面 URL、HTMX 目标归模板；Go 不拼 HTML/成品属性串，不返回 DOM 选择器或导航配置。
- 两种合法出口形状：控制器直接 `c.HTML(status, tpl, data)`，或走共享渲染基础设施（`pkg/view`、`pkg/view/jet`）注入语言与页面公共上下文后再渲染。共享基础设施只装配一次，不在每个模块重复接线；后台页面的请求事实（CSRF、授权菜单树）由 routers 在装配期经 `pkg/view.SetAdminPort` 注入。
- 路由按真实职责声明响应协议，且在鉴权、CSRF、限流、请求体拒绝之前可识别；不按 `/api`、`Accept` 或 `HX-Request` 猜测协议，不机械复制 HTML/JSON 双端点。
- 输入与输出协议解耦：普通 form、明确 multipart、复杂编辑器 JSON 都可以返回 HTML。普通 UI 不再使用 JSON→DOM 解释器。
- 成功导航只由发起模板显式决定；后端不发 302、`HX-Redirect`、`HX-Location`、`HX-Refresh`。失败、网络错误、非法类型、迟到响应不得执行成功动作。
- 错误模板不携带登录页或整页壳去替换局部表单；模板二次失败不递归、不泄漏内部原文，已写出的流不重写。

## 分层判据

**判据按职责语义归属，不按字段名黑名单归属。** 新增处理器先问「这段计算属于哪一层」，而不是等门禁逐个堵。

### 业务模块（internal/module/<mod>/）

| 目录 | 只准 | 一律禁止 |
|---|---|---|
| dto | 请求基础校验（必填、类型、格式、长度、范围、数组元素的 tag / Validate），以及**返回给 JSON 调用方的响应对象** | 业务规则、数据库查询、表单编码适配；换语言就要重算的**界面**成品句（已取词的 Err/ErrMsg、后端为页面拼的标签句）、`gin.H`、语言注入字段、import service/model |
| service | 业务规则、校验、事务边界、CRUD/list、数据库聚合、多表拼装成业务对象；交业务事实 | 取词（`i18n.Translate` / `TranslateFunc` / `templates.Translate`）、自造界面词条、把已取词句子或 HTML 塞进 data、`*PageResp`、import templates / pkg/view 的业务包装 |
| model | 本模块表的 CRUD、聚合内原子组合、分页筛选/count/limit/offset | 业务规则、跨聚合事务、别人的表名、import service |
| contract | 接口 + 不可变 DTO | import 别人的 service/model、反向依赖 builder/core |
| enums | 枚举、**业务错误声明**（key + 状态码；文案归词库，声明里不抄中文） | 取词、拼句；**不要求登记按钮/标题等界面 key** |
| inbound/http | Gin 原生绑定请求 DTO → 显式基础校验（需要时）→ 调 service → 页面渲染或 JSON 出口 | 第二段业务逻辑、拼 map/串、取词、编译归一；新增绑定薄包装 |
| inbound/mcp | 工具定义与参数校验 | 重抄 dto 字段表、直连 model |
| outbound | 外部系统适配 | 业务规则 |

- **界面 key 与业务模块无关**：模板直接写 UI key + 中文兜底，用全局 `t` / `tf`。**不在模块 enums 里登记按钮/标题这类界面 key**，也不从 handler/service 传翻译函数、不提前取词。
- **业务字段名称恰好含 Title/Text/Label 不是越界证据**：商品标题、文章正文、**真实译文数据（用户/编辑录入的多语言内容，不是界面词条）**、存储的 HTML 内容都是业务事实，可以进 dto 与 JSON。判据是「这是不是后端替**界面**拼的、换语言/换样式就要重算的成品句」，不是名字，也不是出现语言的字段。
- 按职责组织控制器与 service，不按行数机械拆包装；复用现有公共分页，不新造 Repository，不做 SPA 迁移。

### 平台与横切（internal/ 其余）

| 包 | 只准 | 一律禁止 |
|---|---|---|
| routers | 装配、路由注册、渲染器装配 | 业务逻辑、SQL |
| view（`pkg/view`） | 视图结果、成功/失败出口、页面/局部渲染与公共上下文注入、Jet 失败出口 | 业务语义、数据派生、Go 拼界面 HTML |
| text | 按请求语言注入 `t`/`tf`/`tn` 与语言上下文（**共享渲染基础设施**）、`FactOf` 判定 | 业务语义、读模型、替业务拼成品句 |
| builder/** | 构建期编译与组件 | 控制面 DB 写、访客请求处理 |
| pipeline | 发布内核（编译→产物→激活） | 反向 import build/artifact 实现 |
| permission middleware mcp seo shell siteurl uispec backref retention partition | 各自横切能力 | 业务规则、跨模块写 |
| architecture | 门禁断言 | 被生产代码 import |
| templates/** | **模板资产 = 前端层**（HTML/Jet 模板、静态资源）；**代码 = 共享渲染基础设施**（Jet 引擎适配、模板函数、嵌入，现状即在本目录，见 [internal/templates/CLAUDE.md](internal/templates/CLAUDE.md)） | 业务规则、业务计算、controller/service 语义；不放业务 Go |
| 模板渲染调用 | 页面、局部界面与错误响应；Controller 选真实模板并传业务事实 | Go 拼标签、属性、界面 URL 或浏览器 UI 配置 |

### 基础设施（pkg/）

| 包 | 只准 | 一律禁止 |
|---|---|---|
| binding | **请求绑定的唯一入口**，三个入口各自给一份确定协议：`ParseForm`（只读 body、只接受 urlencoded/multipart、非表单编码直接报错）/ `ParseJSON` / `ParseQuery`（只读查询串，按 `listquery` tag 回填 `ListQuery`）。`Field`/`ListQuery`/`WithListQuery` 是辅助实现 | 业务校验、按 Content-Type 猜协议的入口、同一端点两种编码、绕过入口直接 `ShouldBind` |
| facing | 错误事实（key + 状态码；`facing.Msg` 的兜底是**消息事实**，合法保留） | 取词、日志 |
| response | JSON 出口取词与状态码（`MessageOf` 产生 message） | 业务语义 |
| view | 视图结果接口、请求级渲染接线、页面/片段渲染入口、统一成功与失败出口（不含模板引擎） | 模板引擎实现、业务逻辑、按模块定制包装 |
| money pagination | **按函数职责**：`pagination.Window`/`Derive`/`PageBase`/`QueryURL` 与 `templates` 的金额显示函数 → 前端展示；`pagination.ClampPage`/`Slice` → service/model 的数据库分页工具；`money.*` 的金额显示格式函数被模板侧调用 | 业务规则、金额求和、数据库筛选/count；**不要整包归一类** |
| i18n logger database cache rls jsonx utils pathkit safepath | 基础设施 | import internal/** |
| auth casbin crypto captcha mailer queue upload validate sitehttps sitetz datarule reqparams backpath bulkids enums | 能力包（provider 型带 `provider/` 实现） | 业务语义、import internal/** |

### 前端层与入口

| 位置 | 只准 | 一律禁止 |
|---|---|---|
| internal/templates/** | 模板资产：结构、样式、标题/菜单、徽标分档、空态、时间/金额显示格式、分页按钮窗口、取词、表单初值；同目录的 Go 只做渲染基础设施（引擎适配、模板函数、嵌入） | 业务规则的第二份实现、金额求和与库存合计、后端口径的聚合、**业务 Go 代码** |
| public/static/** | JS/CSS（HTMX 基座、`write-base.js`） | 业务计算 |
| cmd/** | 进程入口、装配、迁移命令 | 业务逻辑 |
| public/migrations | 版本化幂等 SQL + seed 注册 | Go 业务逻辑 |
| public/test/** | feature 链路测试 | 被生产代码 import |

- **后端**做数据库分页筛选 / count / limit / offset，交总数、当前页、每页条数等事实；**前端**做分页按钮与链接窗口、空态、徽标、显示格式。页码窗口与链接（`pagination.Window`/`Derive`/`PageBase`/`QueryURL`）与金额显示（`money.*`）由模板调用；`pagination.ClampPage`/`Slice` 属后端分页工具，归 service/model。

## 取词与文案

- **词条 key 是完整、扁平的语义标识**：仅用 ASCII 字母数字，首字符为字母，长度 1–15；不含点或其他层级分隔符，不通过前缀、后缀、format 或模板动态拼接产生。分类用已有 category 字段，不编码为 key 路径；不机械截断，不用随机编号或哈希代替语义命名。
- 一个 key 表达完整语义，译文允许具名占位参数，不拆成多个词条拼句。同 key 可有多语言记录，数据库按 `(item_key, lang)` 唯一。UI key 直接写在 Jet；业务错误、消息事实的完整 key 在 enums 声明，调用处引用常量；Go 常量标识符不受 15 字符限制。
- 此约束不适用于权限码、按钮权限标识、业务状态、SEO 算法检查项、内容字段路径和内容翻译哈希。带参数的错误载荷不是 key 本体，不对整条载荷套用 15 字符限制。未整改模块如实列为待实施，不新增旧形状、兼容别名或放行白名单。
- key 改名同步真实消费者、词库与门禁；检查完整字面量、唯一性及真实中英文命中。迁移保留自定义译文与额外语言，目标冲突整批回滚；重复运行完整 seed 后不得补回已退役 key。不能因为扫描器只认识点分 key 而把短 key 漏查当通过。

- **取词只有两个出口**：模板的全局 `t` / `tf`（`pkg/view/jet` 的请求作用域，经 `pkg/view` 注入渲染数据），与 JSON 出口 `pkg/response.MessageOf`。
- 按请求语言的**注入**只归共享渲染基础设施（`pkg/view/jet` 的 `AttachScope`/`Inject`、`pkg/view`）；模块不重复接线。
- Go 侧出现 `TranslateFunc(` / `templates.Translate(` / `Prepare*View(` 里的取词即越界。
- **业务错误**在声明处只给 key + 状态码（`enums.Err(key)` → `facing.WithStatus(...)`）；HTML 出口传错误事实，由 Jet 取词，数据 JSON 出口由 `response.ErrorStatusAuto` 产生 message。缺词给归口文案，不把裸 key、编码明细或内部原文交给前端。
- **不要求 service 为页面 UI 产出 `facing.Msg` / `facing.Labeled` 或业务枚举**；页面 UI 的文案归模板。
- 状态码沿真实语义：400/422 参数 / 401 未登录 / 403 无权限 / 404 不存在 / 409 冲突与版本 / 413 请求体 / 429 限流 / 500 其余。
- 401/403 按已声明的路由协议输出，不因客户端头改变鉴权结果，不跳转、不把登录页塞入局部界面。

## 架构不变量（注释以「AGENTS.md 不变量 N」引用）

1. **控制面与访问面分离**：分四档判定，不要混为一谈。
   - **普通页面 GET（访客）**：只由 PublicationStore 定位静态产物 —— 不查控制面数据库、不运行 Jet、不解释 AST；URL→文件完全由发布状态决定。
   - **明确注册的访客业务端点**（购物车、会话、订单、AI 采集等）：按自身鉴权、CSRF、限流与模块契约处理；数据消费者用 JSON，实时业务区域可执行固定受控 Jet 模板。能力必须逐条声明权限点或访客鉴权豁免，并隔离会话、缓存、语言与产物版本。
   - **构建与预览**：在控制面解析文档与模板，不服务访客整页请求。
   - **禁止任意运行时文档渲染**：动态业务区域不得回读页面/区块 AST，不恢复已退役的任意文档片段出口，不改变访客完整页面的静态发布主链。

   两处既有特例仍然成立，但不是新例外：`POST /analytics/collect`（只写一条、响应恒 204）与 AccessGuard 只读访客会话（不查 PG、不写、不续期、fail closed）。
2. **两条发布路径共享同一管线**：Page 与 PresentationInstance 都走 Publish Compiler → ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃**，ContentTemplate 每次构建参与。
4. **Binding 不是 Query DSL**：Document 只存白名单 FieldBinding/CollectionSource/MediaBinding。
5. **确定性构建**：同一输入产生相同 Artifact 字节。
6. **冻结边界不可越权**：模块、组件、协议各有负责与禁止范围（[internal/module/CLAUDE.md](internal/module/CLAUDE.md)、[internal/templates/CLAUDE.md](internal/templates/CLAUDE.md)）。
7. **构建期数据源依赖方向**：共享形状在 `pkg/builder/source`；契约包不得反向 import `builder/core`（`scripts/check-contract-deps.sh` 判传递闭包）。

> 控制面的后台页面与局部界面由 Jet 实时渲染；访客完整页面仍静态，只有明确业务区域使用固定模板，不执行任意页面文档。

## 数据库

- 表只被一个模块读写；跨模块走 `contract`。model 不声明列型（`internal/architecture/model_gorm_tag_test.go`）。
  - 已批准例外只有两处，都是商品模块的**只读**读模型，只服务各自的列表页，不写、不改归属，也不给其他模块开同类口子：
    - 公开商品目录（`internal/module/product/model/product_public_catalog.go`）只读关联 `presentation_instances` / `presentation_publications` 两表，用于在一条语句内收敛「有有效发布的商品」候选并分页。
    - 发布记录页（`internal/module/product/model/publish_record_page.go`）只读关联 `presentation_instances` / `presentation_publications` / `presentation_artifacts` 三表，用于在一条语句内完成「商品级筛选 + 精确 total + 越界夹页 + 本页多实例多语言明细」，让 total 与本页行出自同一份语句快照。
- 时间列：新表用 `create_time`/`update_time`；业务时刻沿用 `*_at`；`timestamptz` + `time.Time`；对外 JSON 只给时刻（`utils.JSONTime` 输出 unix 毫秒；**显示格式由模板决定**）。软删除统一 `deleted_at`。
- 迁移：版本化幂等 SQL + `register.go`；seed 门槛逐条枚举本批对象（禁用 LIKE/全库总量）；改列名同步触发器与 seed SQL；新 SQL 上线前 PREPARE 校验；Migrations 台账先跑、Seeds 后跑。
- 裸 SQL 只减不增（`raw_sql_boundary_test.go`）；唯一 JSON 入口 `pkg/jsonx`（`json_import_boundary_test.go`）。
- 主键：对外实体 uuid（应用层生成）；纯内部流水与字典 bigint identity；只增分区流水用 UUIDv7。
- 权限点：一条 API 路由一个独立权限点（常量按操作属主声明在各模块 `enums/permission_codes.go`，注册处用 `middleware.RouteGroup`/`DeclareRoute` 声明），页面路由可复用。权限名称与启停只来自数据库；启动期只核对、不反写。
- RLS 顺序：先包 `pkg/rls.InProjectScope` → 再把连接角色换成非超级 → 最后 `require_rls_role=true`。
- 聚合口径：同一屏上的两个数字若回答同一批行，筛选条件抽成共享常量；多序列共用横轴时聚合粒度必须一致且由调用方统一传；桶键读回来必须还是同一时刻（`AT TIME ZONE 'UTC'` 写两遍，`::date` 不用）。

## 写操作

- 普通提交使用 Jet 原生 form + HTMX 4 标准表单编码与 HTML 交换；上传明确 multipart，复杂编辑器明确 JSON 输入。`write-base` 只接 CSRF、响应校验、错误区、生命周期和前端成功动作，不另写普通表单 fetch 执行器。
- 查询用 `binding.ParseQuery[T]`（只读查询串）；普通写 body 用 `binding.ParseForm[T]`（只读 body，不合并 query，非表单编码直接报错）；复杂输入用 `binding.ParseJSON[T]`；上传明确 multipart（`ParseForm` 一视同仁，按声明的编码选 FormPost 或 FormMultipart）。不直接调 `ShouldBind`/`ShouldBindWith`：前者按 Content-Type 自适应并把 query 并进表单，后者对 JSON 体静默得到空表单，带默认值的 DTO 会把「没提交」写成「要求默认值」。Gin 不会自动调用任意 `Validate()`，需要时显式调用；不新增万能 binder 或同义包装，保留请求体上限。
- DTO 不复用展示响应对象作为请求；操作人和授权范围等可信字段由服务端提供，不开放客户端绑定。归属、状态、事务与锁仍归 service。
- 原生字段名与请求 tag 一致，ID 保持字符串；明确 checkbox、空数字、0、缺失/清空集合和完整替换/PATCH 语义。不得假设 Gin 自动理解任意嵌套表单下标，不新增通用 presence/表单 Schema。
- 普通表单不逐页拼 JSON；复杂编辑器只提供真实业务对象，发送仍由 HTMX 执行，不 stringify 后藏进普通字段。旧字段整形、旧执行器、旧别名不保留。
- HTML 成功才允许交换和前端声明的成功动作；失败只更新明确错误区，保留文件、富文本、焦点与未保存行。非法类型、重定向、网络错误与迟到响应不得触发成功动作。真实数据消费者仍使用明确 JSON 协议。
- 模板落地与属性门禁见 [internal/templates/CLAUDE.md](internal/templates/CLAUDE.md)。未迁页面是待实施缺陷，不是恢复旧提交链的许可。

一次用户可感知的写操作涉及两处以上持久化写入时，必须同一事务；事务边界由 service 决定，句柄从 model 下传（`…Tx` 方法）。
读-改-写必须行锁或原子 SQL。跨库/外部系统的补偿必须幂等、留痕、可重放并写明理由。
冲突一律打回（列出可定位的数据），不自动加后缀、不静默合并。
**批量要么全成要么全败**：不推算 skipped、不返回逐条回执；拒绝时把 id / 名称 / 原因放 `data.items`。
门禁：`public/test/architecture/tx_boundary_scan_test.go`（启发式，绿灯不等于没问题）。

## 测试

- **需求决定实现，测试验证需求**：不为旧断言改变业务、不保留错误架构。
- 分层：模块内 unit 测纯逻辑；接口链路走 `public/test/<mod>/feature`（真实 PG）；组件测试在组件包内。
- 表结构一律来自生产迁移（`support.NewMigratedPGTestDB`）；禁止手抄 `CREATE TABLE`。
- 断言等式而非各自的值；错误断言状态码 + 对外文案且不含内部原文；页面断言真实 HTML 片段。
- 计数与命名不是判据：`rg -c` 只是筛查起点，结论前回读语义；门禁绿不等于没问题。
- 构建 / 权限 / 工程隔离 / 事务等**事实校验**仍然有意义，不能用「门禁计数」替代交付。
- **不要没事干去跑全量测试。针对功能做单测就行**：默认只为本次改动的功能编写并运行单元测试，用包/用例限定范围（`go test ./internal/module/<mod>/...`、`go test ./public/test/<mod>/feature -run <Case>`）；**不得自行运行全量测试**（`go test ./...` 等），全量只在用户明确要求时执行。
- **真实 PG feature 包很贵，按用例下探**：每个夹具会复制模板库并跑一遍种子。改代码期间只用 `-run 'TestA|TestB'` 跑本次碰到的用例；**整包（不带 `-run`）每个模块只在收尾跑一次**，不要「改一处 → 整包 → 再改 → 再整包」。
- **先改完模块代码，再统一改测试**：重构的功能面在动手前就已确定，不要每个功能点都停下来写一轮测试。顺序是「模块代码一次改完 → 按新契约统一更新测试 → 定向验证 → 整包一次」。中途失败先定位到具体用例，不要用整包重跑来复现。
- **模块内的 `internal/module/<mod>/...` 单测便宜**（无 PG），改完一处可以随手跑；贵的只有 `public/test/<mod>/feature`。

## 流程

1. **规划先行**：一次改动先写「层归属表」（每处改动属于哪层、该层允许什么）；判据不齐不动代码。
2. **直接在 main 上改**：不开分支、不建 worktree（用户 2026-11-01 明确要求）。
3. **一次完整交接**：主代理只规划与验收，指定编辑代理一次改完、集中验收，不细碎分批停工。
4. **批次**：一屏/一域一个提交；先删旧形状，再建新形状；每批只放该批改动。
5. **验收四件**：`go build ./...` 干净 · 相关包 + feature 测试绿（feature 用 `-run` 定向，收尾整包一次） · 本次涉及的门禁脚本绿 · 门禁台账减条。
   `scripts/check-all.sh` 会间接跑 `go vet ./...`（全仓测试编译）与 workbench 的一批测试，属于用户明确要求时的整仓验收；日常按批次只跑本次相关的门禁脚本（如 `check-write-attributes.sh`、`check-i18n-keys-seeded.sh`、`check-write-fields.sh <模块>`）。
6. **收口**：反查过期注释、重复实现、形状漂移；做完的区域比动手前更少。改到的文件顺手清掉指向已删文档的指针（`docs_pointer_test.go` 只减不增）。
7. **提交**：中文说明改动文件与内容；精确暂存（逐文件 add），暂存里出现不认识的文件先查证。
8. 改规则文件前重新读取（桌面端可能并发改写）；文档更新与代码提交分开。

**不做的事**：不建兼容层 / 适配旧出口 / 「暂保留旧形状」；用「有意保留」「迁移期」「存量兼容」为错误架构开口子一律算缺陷。
存量问题只能在文档里**如实标为待实施**，不能写成许可。

## 门禁（会变红的判据）

**判据先于实现**：新边界先落一条判据让现状变红，再逐条修到绿；台账类判据双向登记（新增与清理未同步都判失败）。判据要指向修复方向，不能只报「违规」。门禁绿不等于没问题——启发式与台账式判据结论前要回读语义。

- `internal/architecture/`：`controller_shape_test.go`、`controller_objects_test.go`、`controller_orchestration_test.go`、`text_domain_test.go`、`html_boundary_test.go`、`template_logic_test.go`、`module_boundary_test.go`、`raw_sql_boundary_test.go`、`json_import_boundary_test.go`、`model_gorm_tag_test.go`、`dto_shape_test.go`、`docs_pointer_test.go`。
- `scripts/`：`check-all.sh`、`check-no-internal-error-leak.sh`、`check-service-db-boundary.sh`、`check-contract-deps.sh`、`check-controller-i18n-literals.sh`、`check-permission-gaps.sh`、`check-i18n-*`、`check-empty-state-table-head.sh`、`check-shell-migration.sh`、`check-write-attributes.sh`、`check-htmx-api.sh`。

## 指针

[README.md](README.md)（产品与快速开始）· [CONTRIBUTING.md](CONTRIBUTING.md)（本地起步、门禁、测试执行、提交）· [architecture.md](architecture.md)（拓扑、核心概念、部署形态）· [TODO.md](TODO.md)（**待核实**的存量整改项，不是规范、不产生判据）· [goal-and-requirements.md](goal-and-requirements.md)（产品目标与范围）· [SECURITY.md](SECURITY.md)（漏洞报告）· [internal/module/CLAUDE.md](internal/module/CLAUDE.md)（模块目录与分层细则）· [internal/templates/CLAUDE.md](internal/templates/CLAUDE.md)（模板契约、取词、HTMX、写基座、多端）· [pkg/CLAUDE.md](pkg/CLAUDE.md)（基础设施）· [public/CLAUDE.md](public/CLAUDE.md)（迁移与静态资源）· [public/test/CLAUDE.md](public/test/CLAUDE.md)（feature 测试）· [scripts/README.md](scripts/README.md)（脚本清单）。