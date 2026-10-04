# 门禁

<!-- TODO(开源前确认): 本文件描述的入口 `scripts/check-all.sh` 与它调用的 `scripts/check-contract-deps.sh`、`scripts/check-dto-immutability.sh`、`scripts/check-page-endpoint-authz.py`，以及两份豁免清单 `scripts/contract-deps-allow.txt`、`scripts/page-endpoint-authz-allow.txt`，尚未被版本控制跟踪（写作时 `git ls-files` 无输出），`make check-db` / `make check-all` / `make test-race` 三个 Makefile 目标也只在工作区改动中；开源提交前先确认它们已入库，否则克隆下来的仓库里没有这些命令。 -->

`scripts/check-all.sh` 是本仓库门禁的唯一入口，`Makefile` 只做三件事：跑 lint、按模式调它、跑竞态测试。

| 命令 | 实际执行 |
|---|---|
| `make check` | `gofmt -l internal/ pkg/ public/ cmd/ config/` + `go vet ./...`，然后 `bash scripts/check-all.sh`（**无外部依赖组，12 个脚本**） |
| `make check-db` | `bash scripts/check-all.sh --db-only`（**只需数据库组，1 个脚本**） |
| `make check-all` | `bash scripts/check-all.sh --with-db`（**全部 13 个脚本**） |
| `make test-race` | `go test -race -p 4 ./pkg/... ./internal/pipeline/... ./internal/builder/... ./internal/module/publication/... ./internal/module/presentation/... -count=1 -timeout 20m` |
| `make test` | `go test -p $(nproc) ./... -count=1` |
| `make test-short` | `go test ./pkg/... ./internal/... ./cmd/... ./config/... ./public/migrations/... -count=1` |

`check-all.sh` 会 `export SKY_CSS_GUARD=error`，把多设备 CSS 检查从「默认告警」升为硬门禁。未知参数退出码 2。脚本文件缺失视为失败，不静默跳过。

## 无外部依赖组（`PLAIN_SCRIPTS`，12 个）

### `check-no-internal-error-leak.sh`

管什么：内部错误（`err.Error()`）不得出现在后台 handler 的输出里 —— 它会把 schema、SQL、路径泄漏到页面上。

判据：按三种**调用形状**匹配，不看变量名 —— ① 响应写入（`*.ErrorWithMessage(` / `c.String(`）的实参带 `.Error()`；② 重定向 query 里出现 `?err="+url.QueryEscape(err.Error())`；③ 模板数据 `data.Errors = []string{… + err.Error()}`。

改法：用 `shell.PageError(c, scene, err)`，或模块自己的归口助手（如 `internal/module/admin/inbound/http/admin_err.go`），或 `pkg/response.ErrorAuto` —— 用户看场景文案，细节进日志。

豁免：脚本内 `EXEMPT`（文件 + **形态**）逐条记账（`declare -A EXEMPT`，`:74` 起）。只放行某文件的某一种形态，该文件将来出现别的形态照样会被抓住。登记了却不再命中的条目会被判为过期（`:107`），逼着清理。

### `check-i18n-coverage.sh`

管什么：后台模板不得新增硬编码中文/日文/韩文文案。

判据：**固化基线 + 禁止新增**（不是一步归零）。计数按**行**：一行里含未被 `t()` 包住的中日韩字符即记一行；注释整段剔除（Jet `{* *}` 与 HTML `<!-- -->`）；`t()` 内的中文是兜底，不算。扫描范围是 `internal/templates/**admin**/` 与 `**fragments**/` 下的 `.html`，**两个目录合并计一个数**（拆分会让数字随文件搬家而漂移）。只收 `.html`（`fragments/*.jet` 的 data 是 struct，不受此检查）。

豁免/基线：`scripts/i18n-coverage-baseline.txt`（实有 1 行）。新增硬编码 → 退出码 1。**抽取文案后要主动下调基线**，脚本会打印下调命令：`echo $CURRENT > scripts/i18n-coverage-baseline.txt`。基线文件不存在时脚本会先写入当前值。

### `check-i18n-keys-seeded.sh`

管什么：反方向 —— 模板用到的 `t()` key 必须真的被 seed 进库，否则页面上是空白（`{{tr := .["t"]}}` 缺 key 是**静默输出空串**，不报错）。

判据：「已 seed」= 该 `item_key` 真的出现在 `INSERT INTO sys_i18n … VALUES (…)` 的**元组**里（解析 `public/**/*.sql`），而不是「这段文本在任意 `.go`/`.sql` 里出现过」。

豁免/基线：`scripts/i18n-keys-seeded-baseline.txt`（可用环境变量覆盖：`I18N_KEYS_BASELINE`，配合 `GOWP_ROOT` 可隔离试跑）。中英成对缺失属 WARN 段，不参与退出码。

**这个门禁有已知盲区**：它只保证「用到的 key 在库里」，不保证「该面板的 key 都齐」。实测 2026-10 时 `admin.page.langs.*` 面板 18 个 key 有 16 个没 seed，而门禁是绿的 —— 基线为 0 只证明没人硬编码中文，不证明文案齐全。

### `check-service-db-boundary.sh`

管什么：模块 `service` 层不得绕开 model 直接拿裸 DB 句柄拼查询（除了 admin 的明文豁免）。

判据两条：① 非 admin 模块的 `service` 里出现任意裸句柄门面 `.<Name>DB(ctx)`（含 model 自定的 `RouteDB`/`ReceiptDB`/`PublicationDB`/`VariantDB` 一类）；①b 在事务或裸句柄上直接拼查询（`tx.WithContext(ctx).Model(...)` / `Create` / `Where` / `Exec` …）。句柄名来自固定白名单（`tx`/`txn`/`trx`/`session`/`dbtx`/`dbx`/`db`/`t`）加上对 `q := s.model.ReceiptDB(ctx)` 这类局部变量的动态收集。判定逻辑是脚本内联的 python3 heredoc。

豁免：`scripts/service-db-boundary-allow.txt`（实有 31 行，含注释），**每条必须写明理由**；文件缺失直接报错退出。admin 模块的既有直查是明文豁免（仅限本模块表、简单 CRUD）。

什么时候可以豁免：只有当这段查询确实无法下沉到 model（例如需要跨表联查又不属于任何单一实体）时才登记，并把「为什么不能下沉」写进理由。新模块照抄 admin 的写法**不构成**豁免理由 —— 脚本头注释承认这正是它要堵的洞。

### `check-page-endpoint-authz.py`

管什么：POST 页面端点必须挂鉴权，否则任意已登录账号可跨工程操作（实例：`POST /workbench/instance/save` 曾被任意已登录账号用来跨工程覆盖实例文档）。

判据三类（按调用形状，不按文件）：① 第二实参是 `permission.XxxPerm` → 免检（`permission.RouteGroup` 的组链自带 `CasbinMiddleware`）；② 实参里出现 `CasbinMiddleware*` 或该组 `.Use(...)` 挂了它 → 免检；③ 都不满足 → 违规，除非命中豁免清单。覆盖 `/admin`、`/workbench` 与各模块页面组。纯文本扫描，不依赖 `rg`/`grep` 版本。

豁免：`scripts/page-endpoint-authz-allow.txt`（实有 54 行）。条目**不再命中即失败** —— 只增不减的清单等于没有门禁。

### `check-multidevice-css.sh`

管什么：多设备下 CSS 不越界（审计 UI-015）。

判据：`SKY_CSS_GUARD` 控制档位（`off`/`warn`/`error`，默认 `warn`），实际执行 `go run ./scripts/multidevice-css-check`；`error` 下未豁免违规退出码 1。纯文本扫描，不需要数据库。`check-all.sh` 统一导出 `error`，所以走门禁入口时它是硬门禁；单独手动跑默认只告警。

### `check-empty-state-table-head.sh`

管什么：空态不得吃掉表头（否则用户看不到列名，也无从判断「没数据」还是「加载失败」）。

判据（按形状，对每个 `{{if}}` 块）：if 段渲染 `.empty-state` ∧ if 段无 `<table` ∧ if 段无 `colspan=` ∧ else 段有 `<table` ⇒ 命中。正确形态是把空态放进 `<tbody>` 的 `<td colspan="列数">`。首轮扫描命中 43 处、横跨 12 个模块。

豁免：`scripts/empty-state-table-head-allow.txt`（实有 21 行，格式 `<仓库相对路径>` TAB `<理由>`）。**缺理由的条目脚本自己报错**；过期条目同样失败。

### `check-inventory-sku-format.sh`

管什么：仓库 SKU 永远是裸码（`DRAWERSMOKE_001`），仓码前缀只属于商品侧（`SZ_DRAWERSMOKE_001`）。

判据：三类存量一起查 —— ① `inventory_stocks.sku_code` 为空或纯空白；② 以该仓短码 + `_` 开头；③ `inventory_purchase_order_lines.sku_code` 为空或以本工程任一仓短码 + `_` 开头。

豁免：**没有豁免，也不自动修**。只读巡检，命中即退出码 1 卡上线。不自动改的理由写在脚本里：空编码剥不出东西；剥前缀可能撞 `UNIQUE (warehouse_id, sku_code)`（迁移 244/262）。命中后由人决定是补编码还是改数据。

### `check-stock-sku-prefix-collisions.sh`

管什么：迁移 262 的**只读预检**。跑迁移之前先确认数据里没有会造成前缀歧义的存量。

判据：三处谓词与迁移 262 逐字一致（`length(s.sku_code) > length(w.code) + 1` ∧ `upper(left(s.sku_code, length(w.code))) = upper(w.code)` ∧ `substr(s.sku_code, length(w.code) + 1, 1) = '_'`），限定 `current_schema()`。只跑 `SELECT`。

豁免：无。退出 0 = 可以安全跑迁移；1 = 有冲突，打回给人。

### `check-workbench.sh`

管什么：工作台契约与渲染一致性。

判据：`export GOWP_REQUIRE_NODE=1`，调 `go run ./cmd/workbench-contracts -check`，再 `go test -count=1 ./internal/templates ./internal/builder/... ./internal/module/workbench/inbound/http ./public/test/workbench/feature`。

前提：需要 Node，缺了直接退出 1（「通过 vfox 准备测试运行时；工作台检查不能跳过」）。Node **只用于开发验证**，不参与生产资产构建。想单独跑完整契约生成：`go run ./cmd/workbench-contracts`。

豁免：无。

### `check-contract-deps.sh`

管什么：契约包的依赖方向（AGENTS.md 不变量 7：契约包不得反向 import builder 内部）。

为什么需要脚本：契约包多 import 一个 builder 子包，本模块照样编译得过 —— 编译期谁都发现不了，直到 `builder/core` 某天想持有这个契约才炸成 import cycle，而那时改动面已经摊到全仓。

判据：对每个 `internal/module/*/contract` 包跑 `go list -deps`，输出里出现 `go_wp/internal/builder/core`、`go_wp/internal/builder/style`、`go_wp/internal/builder/plugincomp` 任一即违规。契约包只允许依赖 `go_wp/internal/builder/source`（纯数据形状，自身零 builder 内部依赖）。查的是**传递闭包**而不是直接 import 列表 —— 只查直接依赖等于给「多绕一层」（例如经 `builder/plugincomp` 把 `core` 拖进来）留后门。

豁免：`scripts/contract-deps-allow.txt`（格式 `<包路径>  # <理由>`）。格式错、理由为空、重复登记都算脚本自身错误（退出码 2）。登记一条 = 承认破了不变量 7，必须同时写出「怎么还」；条目一旦不再命中，脚本失败并要求删条目（技债清单只增不减就会掩盖回归）。正常状态下该文件为空 —— 2026-09 清零留档：`internal/module/plugin/contract` 曾同时有两条 builder 依赖。

退出码：0 = 无白名单外违规；1 = 有违规或过期条目；2 = 环境/清单/编译问题。需要 Go 工具链。

### `check-dto-immutability.sh`

管什么：契约 DTO 的「可被调用方就地改写」形状盘点。跨模块只传不可变 DTO，但调用方拿到的 Resp 里若带 `[]T` / `map` / 裸指针，就能就地改元素 —— 改的是提供方返回结构里的**同一块内存**（切片共享底层数组、map 共享哈希表、指针指向同一对象），提供方自己那份也跟着变。这类缺陷不报错、不 panic，只表现为「莫名其妙少了一行」。

判据（**启发式**，只认形状不认语义）：`internal/module/*/dto/*.go` 里名字以 `Resp` / `Response` 结尾的 struct，字段类型字面含 `map[` / `[]` / 裸指针 `*` 即记一条告警（输出文件:行 + 类型名 + 字段名）。

**这是唯一一个 warn-only 的门禁**：无论发现多少告警都退出 0，只有脚本自身错误（目标目录不存在 / 没有 python3）才非 0。理由写在脚本头部：上百条告警里大部分是既有设计（`*float64` 这类「可空标量」是刻意的零值表达 —— 「未填」与「填了 0」是两回事），一刀切改成退出 1 只会逼人把字段藏起来。要升级成硬门禁，先给基线（「告警数不得高于 N」或「新增切片字段必须进白名单」）。

已知漏报（改判据前先看脚本头部）：`json.RawMessage`（`[]byte` 别名）、`type T = []X` 这类别名、经方法返回的可变类型，字面里没有 `[]`/`*`/`map[`，判据一律不认；只扫 dto 包内定义的 struct，把可变形状藏在非 dto 包就绕过了。

豁免：无豁免清单（warn-only 本身就是它的豁免口径）。

## 需要数据库组（`DB_SCRIPTS`，1 个）

### `check-permission-gaps.sh`

管什么：接口装了 Casbin 中间件，但 `sys_permission` 里没有对应权限点 —— 后果是**含超管的全体 403**（072/077/078/079 迁移的注释各自写过一遍；151 补了 `page:delete` 与 `block:clone`）。

判据：把**运行时装配出来的路由表**与 `sys_permission` 比对。刻意不 grep 源码猜路径 —— 那样会漏掉拼接出来的前缀与 Group 嵌套。依赖本地可连的数据库（配置从 `config.yaml` 的 database 段读，脚本不另存口令）与 `psql`；缺 psql 直接退出 1。输出「路由 N 条 / 权限点 M 条 / 已豁免 K 条」；有缺口退出 1。另有一个信息级的反向兜底（库里有、代码没声明的权限点），不影响退出码。

豁免：脚本内联 `EXEMPT` 名单（实有 5 条：`GET /api/admin/profile`、`GET /api/admin/routes`、`GET /api/captcha`、`POST /api/admin/login`、`POST /api/admin/logout`）。加之前先回答一句：这个接口被任意登录用户随便调，会不会出事？**注意豁免有两份** —— 运行时 `permission.Exempt`（会打进启动日志）与这个脚本的 `EXEMPT` 名单，改一处不会自动同步另一处。

## 豁免的一般规则

1. **豁免要有理由**。三个豁免文件里有两个会因缺理由而自己失败；理由写「历史原因」等于没写。
2. **清单只减不增地过期即失败**。条目不再命中就报错，防止「只增不减的清单」把门禁稀释成摆设。
3. **豁免按最小范围记账**（文件 + 形态），不做整文件豁免。
4. **豁免是可见的选择**，不是静默通过：运行时豁免会进启动日志，脚本豁免要写进文件。

## 不在 `check-all.sh` 里的脚本

这些存在但**不参与门禁**，别把它们写进提交前清单：

`dev.sh`（开发启动）、`deploy.sh` / `server-deploy.sh`（部署）、`rls-role-setup.sh`（建 RLS 角色与授权，一次性运维）、`index-usage-report.sh`（索引使用报告）、`site-preview.py`、`audit-mark-resolved.py` / `audit-fix-note.py`（审计台账工具）、`loadtest_admin_product_list.go`（压测）、`build-all.sh`/`.ps1`、`build-frontend.sh`/`.ps1`、`prepare-embed.sh`/`.ps1`（历史打包脚本，见 README「验证与契约生成」与已知限制）、`multidevice-css-check/` 与 `wp-import/`（被脚本/命令调用的实现目录）。

## 新增一个门禁

1. 脚本放 `scripts/`，命名 `check-<主题>.sh`（Go 版实现放 `scripts/<主题>-check/`，用 `go run` 调用；也可写 `check-<主题>.py`，入口脚本会按扩展名选 `python3`）。
2. 退出码语义固定：0 = 通过，非 0 = 失败。**不要用退出码表达警告** —— 警告要写进输出并留在退出码 0 里，或者干脆按当前档位参数化（像 `SKY_CSS_GUARD`）。
3. 接进 `scripts/check-all.sh`：不需要数据库的加进 `PLAIN_SCRIPTS`，需要数据库的加进 `DB_SCRIPTS`。两个数组就是全部门禁的白名单，末尾会打印「✓ 全部门禁通过（$MODE，共 N 个脚本）」。
4. 需要例外时提供**外置豁免清单**（放 `scripts/`，`<仓库相对路径>` + 理由），并让条目过期即失败。清单别硬编码进脚本，除非像 `check-permission-gaps.sh` 那样条目少且与角色语义强相关。
5. 在本文件（`docs/contributing/gates.md`）加一节：管什么、判据、豁免方式；并在 [`extend-a-module.md`](extend-a-module.md) 的合并前自查里体现。

## 门禁覆盖不到的地方

- 门禁是**静态扫描 + 库表比对的组合**，不保证运行时行为正确。事务边界、RLS 作用域、渲染产物字节这些靠 `public/test/` 与 `make test-race`，不靠脚本。
- 退出码 0 不等于「测试真的跑过了」：依赖数据库或 Node 的用例在环境缺失时可能被跳过。提交说明里要如实写清跳过了什么。
- `check-i18n-keys-seeded.sh` 只验证 key 被 seed，不验证某个面板的 key 齐全（见上文盲区）。
- `check-dto-immutability.sh` 是 warn-only：它的告警**不是**失败，看到告警要人判断是既有设计还是新引入的可变形状。它出现在 green 输出里不代表 DTO 不可变。
