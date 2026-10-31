# publication SEO 审计 MVC — 集中验收命令清单（E）

> 固定 E 负责人执行。依据 `public/docs/publication-SEO审计MVC实施任务书.md` §E（原文件 :94-102）。
> **当前状态：清单已落盘，未执行任何命令。** 开跑前置条件是任务书 :96「所有写手停止、主负责人静态复审通过」。

## 0 · 前置条件（缺一不跑）

| 项 | 判据 |
|---|---|
| 冻结屏障 | lead 明确放行，A/B/C/D 全部停止写入 |
| 静态复审 | 主审完成，架构项无遗留 |
| 工作树 | `/home/sky/project/go/wp/.worktrees/mvc-publication-seo`，分支 `refactor/mvc-publication-seo` |
| 隔离环境 | §2 三处目标全部确认指向隔离库/配置后，先落 `00-env-route.log` |
| 首轮规则 | 一轮跑完收集全部失败，统一汇总交原负责人；不中途自行改实现或放宽测试 |

## 1 · 命令总表（6 组，一次跑完）

所有命令在 `/home/sky/project/go/wp/.worktrees/mvc-publication-seo` 下执行，输出同时 `tee` 到 `$LOG/`。

### G1 构建

```bash
go build ./...                                                # → 01-build.log
go build -o /tmp/wp-seoaudit-acceptance ./cmd                 # → 01-build.log
```

依赖：无外部服务。
本批无 schema 改动（任务书 :14），不跑 `schema-snapshot.sh --check`——不在批准预算内，属额外扩检查。

### G2 单包与单测（无 PG）

```bash
go test ./internal/module/publication/... ./internal/module/analytics/inbound/http/... -count=1   # → 02-unit.log
```

依赖：PG（见 §2 隔离确认后统一执行；`internal/module/publication/...` 各包是否用 DB 不预判，以实际 skip 记录为准）。
覆盖：`internal/module/publication/service/seo_audit_result_test.go`（七类事实、完整重复路径、无 Message 拼句、`Issues=[]` 与 `Count == len(Issues)` 等式、空态）、`internal/module/publication/service/seo_audit_test.go`（既有审计用例）、`internal/module/publication/dto/seo_audit_req_test.go`（`ResolveProject` 表格：body 优先、空白回落、双空、缺工程 400）；DS-B 的 HTTP 契约测试；既有 `internal/module/analytics/inbound/http/seo_page_handle_test.go`（旧 hx 断言已迁移）。

### G3 feature（真实 PG + Redis）

```bash
GOWP_REQUIRE_PG=1 GOWP_REQUIRE_REDIS=1 go test ./public/test/publication/feature/... ./public/test/analytics/feature/... -count=1   # → 03-feature.log
```

依赖：PG 与 Redis 均可达且已迁移（§2）。两个 `REQUIRE_*` 同时开启：真实登录链经 Redis 存会话，Redis 不可用时跳过会让生产链未测而显示通过（`public/test/support/require_env.go:23,35-38`）。
覆盖：`public/test/publication/feature/seo_audit_json_test.go`（真实装配链：会话、CSRF、缺任一权限、两工程隔离、无重定向）；`public/test/analytics/feature/` 下本批涉及的 SEO 页面链路。

### G4 架构与文本域断言（无 PG）

```bash
go test ./internal/architecture/... -run 'TestControllerFileNames|TestControllerBindingEntry|TestControllerQueryBinding|TestControllerNoUILineImport|TestControllerRenderExit|TestControllerMethodSize|TestTemplateTranslateKeysStayInUIDomain|TestMsgKeysStayInUIDomain|^TestSEOAudit' -count=1   # → 04-architecture.log
```

依赖：无。
函数名已实测核对：

| 函数 | 位置 | 判据 |
|---|---|---|
| `TestControllerFileNames` | `internal/architecture/controller_shape_test.go:257` | 文件命名（`seo_audit_handle.go`） |
| `TestControllerBindingEntry` | 同文件 `:288` | 绑定唯一入口 |
| `TestControllerQueryBinding` | 同文件 `:334` | query 参数读取收口到 DTO 方法 |
| `TestControllerMethodSize` | 同文件 `:381` | 方法行数上限 |
| `TestControllerRenderExit` | 同文件 `:458` | 渲染出口（移除 BareSlot 后减条） |
| `TestControllerNoUILineImport` | 同文件 `:567` | UI 线 import（`:544` 的 `seo_audit_result.go` 条目应减） |
| `TestTemplateTranslateKeysStayInUIDomain` | `internal/architecture/text_domain_test.go:58` | 模板取词域 |
| `TestMsgKeysStayInUIDomain` | 同文件 `:129` | Msg 词条域 |
| `^TestSEOAudit*` | `internal/architecture/publication_seo_audit_boundary_test.go:63/118/192` | 事实类型无展示字段 / 无页面出口 / 路由形状（AST 匹配 `POST /seo-audit`、注册数必须 1、missing·multiline·other·duplicate 等 6 反例） |

`^TestSEOAudit` 已整括三个新断言，不另加轮。

### G5 门禁（一次，含两组）

```bash
bash scripts/check-all.sh --with-db   # → 05-check-all.log
```

依赖：PG + 迁移 + `GOWP_AUDIT_CONFIG` 指向隔离审计配置（§2）。

**只跑一次**：`scripts/check-all.sh` 的 `--with-db` 走 `MODE=all`，选择数组是 `PLAIN_SCRIPTS` 与 `DB_SCRIPTS` 的拼接，plain 组 31 个脚本 + db 组 8 个脚本，共 39 个，一次即覆盖两组。本清单采用这一次 `--with-db`，不再分拆执行。

`--with-db` 覆盖明细：

- **plain 组（31，无外部依赖）** 含本批直接相关：`check-go-vet.sh`（等价于 `go vet ./...`，故**不单列 vet**）、`check-static-scans.sh`（`./public/test/security/...` + `./public/test/architecture/...`，故**不单列这两组**）、`check-js-controls.sh`（递归 `internal/templates/static/js/**/*.test.cjs`，逐文件串行 `node --test`，自动纳入新增 `seo-audit` 与 `write-latest` 测试，故**不单列 JS 测试**）、`check-controller-i18n-literals.sh`、`check-htmx-api.sh`、`check-write-attributes.sh`、`check-write-combinations.sh`、`check-write-fields.sh`、`check-controls-no-htmx.sh`、`check-frontend-naming.sh`、`check-template-presentation.sh`、`check-i18n-coverage.sh`、`check-i18n-keys-seeded.sh`、`check-no-internal-error-leak.sh`、`check-service-db-boundary.sh`、`check-contract-deps.sh`、`check-dto-immutability.sh`、`check-route-registration-placement.sh`、`check-page-endpoint-authz.py`、`check-page-title.sh`、`check-page-view-dto.sh`、`check-multidevice-css.sh`、`check-jet-translation-call-sites.sh`、`check-empty-state-table-head.sh`、`check-shell-migration.sh`、`check-workbench.sh`、`check-automation-canvas-js.sh`、`check-help-popover.py`、`check-component-colors.sh`、`tests/test_page_view_scan.py`
- **db 组（8，需 PG）** 含本批直接相关：`check-permission-gaps.sh`（路由与 `sys_permission` 比对）、`check-page-get-authz.sh`（`:38` VIRTUAL 表，绑 `permission.Declare` 位置）、`check-write-endpoints.sh`、`check-menu-page-binding.sh`、`check-button-code-binding.sh`、`check-inventory-sku-format.sh`、`check-stock-sku-prefix-collisions.sh`、`check-duplicate-indexes.sh`

**明确不跑**（任务书 :99）：全量 `go test ./...`；`make test-race`（`RunSEOAudit` 只读、无共享可变状态、无 goroutine，无并发敏感范围）。

### G6 真实浏览器矩阵（人工核查）

依赖：真实浏览器通道，由 E **实际执行**（browser 技能 + ego），逐项操作并记录结果；不以「人工已验」这类结论替代操作证据。失败或阻塞按原样记入 `06-browser-matrix.md` 与 `99-summary.md`，不记为通过。

视口（`internal/templates/CLAUDE.md:85` 三档）：**1440 / 768 / 375**。
输入（同条）：鼠标 / 滚轮 / 触摸 / 键盘，各一次。

| # | 核查项 | 视口 | 断言 |
|---|---|---|---|
| 1 | 两种空态 | 三档 | `scanned=0`（无产物）与 `scanned>0 & count=0`（未发现问题）文案不同且正确 |
| 2 | 错误分支 | 三档 | 失败保留输入与已有结果，不渲染旧响应 |
| 3 | 无权限分支 | 三档 | 403 显示已取词提示，不泄露内部原文 |
| 4 | 竞态 | 三档 | 连续两次提交，旧响应晚到不渲染/不提示/不解锁新请求（`data-write-latest`） |
| 5 | 键盘 | 三档 | 完整重复路径经 `details/summary` 可键盘展开可见，不藏在 `title` 属性 |
| 6 | 恶意文本 | 三档 | 标题/路径含 HTML 时不执行脚本（`textContent` 填值，无 `innerHTML`） |
| 7 | 未知 Item | 三档 | 显示已取词通用提示 + 原标识纯文本 |
| 8 | 无导航 | 三档 | 成功后无跳转、无刷新、不关抽屉 |

## 2 · 环境隔离（三处独立目标，不可混用）

**三条链不是同一个变量**，须分别确认；任一未确认即不进入 G3/G5。

| 链 | 由什么决定 | 证据位置 |
|---|---|---|
| feature 测试库 | libpq 环境变量 `PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE`，默认 `127.0.0.1:5432` `root` `wp_test` | `public/test/support/pgtest.go:27-39`（默认常量）、`:133-139`（`localPGEndpoint`）。**`GOWP_AUDIT_CONFIG` 不支配 feature** |
| 审计脚本配置 | `GOWP_AUDIT_CONFIG`，默认 `config.yaml` | `scripts/check-permission-gaps.sh:27`、`scripts/check-page-get-authz.sh:7`、`scripts/check-menu-page-binding.sh:29`、`scripts/check-button-code-binding.sh:26`、`scripts/check-inventory-sku-format.sh:38`、`scripts/check-stock-sku-prefix-collisions.sh:29` |
| 应用装配配置 | `config.yaml`（`SetupTestBootstrap` 走真实配置） | CI `.github/workflows/go-test.yml:114-116` 同做法；工作树内 `config.yaml` 当前不存在，只有 `config.yaml.example` |
| 测试 Redis | `GOWP_REDIS_DB`，`public/test/support/component_config.go:47` 固定为逻辑库 `15`；端点由 `pingRedis`（`public/test/support/testenv.go:149-160`）探测 | 需核实该逻辑库无其他使用者 |

**Redis 独占性核实（必做）**：全仓 `*.go` 当前无 `FlushDB`/`FlushAll` 调用点，但为防后续或间接清除，仍须在 `00-env-route.log` 记录：隔离实例/逻辑库编号、该库当前 `DBSIZE`。若与在用应用共用同一实例的同一逻辑库，改用独占实例或独占逻辑库段（避开 `15` 之外已被占用的段），否则测试可能 flush 掉他人数据。核实方法与结论一样落日志，不以推断代替。

准备步骤（屏障放行后执行，凭据不落日志）：

1. 新建独立库（不复用既有 `wp_test`，不默认跑在用库）。
2. 生成隔离 `config.yaml` 并从 `config.yaml.example` 改写 `database` 段指向该库——复制后必须改目标，不是直接使用默认值。
3. `GOWP_AUDIT_CONFIG` 指向第 2 步的隔离配置。
4. `PGDATABASE`（及必要的 `PGHOST/PGPORT/PGUSER/PGPASSWORD`）指向隔离库。
5. 核实 Redis 独占：实例地址端口、逻辑库编号、当前 `DBSIZE`，确认无他人使用（见上）。
6. **先落 `00-env-route.log`**：只读记录三处目标（PG 库名/主机/端口、配置文件路径、Redis 实例与逻辑库）及实际 skip 情况，**不含密码**；记录后才执行 G1–G6。
7. 迁移：`go run ./cmd -migrate-only`（在隔离库上，`go-test.yml:123` 同款；须在 G5 的 db 组之前）。

## 3 · 日志路径

```
/tmp/wp-seoaudit-acceptance-<YYYYmmdd-HHMM>/
├── 00-env-route.log       # 三处目标的真实解析结果（无凭据）
├── 01-build.log           # G1（build ×2 + schema-snapshot --check）
├── 02-unit.log            # G2
├── 03-feature.log         # G3
├── 04-architecture.log    # G4
├── 05-check-all.log       # G5（--with-db 一次，含 39 个脚本明细）
├── 06-browser-matrix.md   # G6 人工核查逐项结论
└── 99-summary.md          # 首轮汇总：命令、退出码、失败条目、跳过项、归责负责人
```

每条日志记录：完整命令、退出码、耗时、跳过项（区分「跳过」与「通过」）。
`99-summary.md` 首轮只汇总不修复，失败条目按 A/B/C/D 归属派回原负责人；修复后仅定向复验对应组，不重跑全部。

## 4 · 实现期偏离记录（不计入验收证据）

| 成员 | 事实 | 定性 |
|---|---|---|
| D | `go vet` 累计 3 次：第 1 次退出码非 0、第 2 次 0、第 3 次 0 | **实现期提前执行（偏离流程）**。违反任务书 :15「不运行 build/test/vet/门禁」。三次均不作为验收证据；G5 中 `check-go-vet.sh` 的结论才是验收口径 |
| C | `node --check` ×4 文件 | 语法自检，不等价于 `*.test.cjs` 行为测试，不计入行为验收 |

## 5 · 首轮规则

1. 一轮跑完 G1–G6，**不在中途修改实现或放宽测试**。
2. 失败统一进 `99-summary.md`，按文件所有权派回：A（service/dto/enums 及单测）/ B（handler/router/权限/台账/HTTP 测试）/ C（模板 JS 及 JS 测试）/ D（feature 与架构测试）。
3. main 上本来就红的用例单列，不顺手修，不作阻塞。
4. 修复后**仅定向复验受影响组**，不重跑全量。
5. 任何通过结论附真实证据（日志文件 + 退出码），不以「stdout 为空」代替退出码判断。
