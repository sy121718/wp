# MVC 单职责代理执行矩阵

> 本代理只读核查 + 只改本文档。**未跑 build/test/vet/门禁/DB/浏览器、未改代码、未 commit/merge/push**。
> 依据：`public/docs/其他模块MVC职责分离规范任务书.md`、`public/docs/商品模块MVC改造执行记录.md`、AGENTS.md、各域已有任务书、`/tmp/wp-seoaudit-acceptance-20261030-1008/` 与 `round2/` 实际日志。
> 本文是**执行矩阵与接口交接单**，不复制规范正文，不做第 N 稿追加。域状态以"实际文件证据"列为准，"待核实"不写成"已完成"。
> **证据口径**：**本代理本轮未运行验收命令 ≠ 本树从未验收**。下文每项结论都指向已落盘的日志路径与退出码；日志没覆盖的项写"未覆盖"，不写成"零"。

---

## 1. 开工信息（本批实际值）

| 项 | 值 |
| --- | --- |
| 目标域 | ① publication SEO 审计（analytics SEO 页面为直接消费者）② navigation 译文写入（下一批） |
| 工作树 | `/home/sky/project/go/wp/.worktrees/mvc-publication-seo` |
| 分支 | `refactor/mvc-publication-seo` |
| 起始 HEAD | `87902f126208c6fe11786becaff5caa102e1cb52`（`git rev-parse HEAD`） |
| 脏改动规模 | 开工时 39 项（18 已跟踪 + 21 未跟踪）；**本代理核实时 33 项**（20 已跟踪 + 13 未跟踪，见 §1.1 末段） |
| 允许共享依赖 | `pkg/binding`（`JSONLimited`，已在本树新增）、`pkg/response`、`pkg/facing`、`pkg/i18n`（`UpsertTx`/`requireTx` 已交付**仅源码**，本轮无任何 test/build/PG 运行（见 §1.1 第 3 条）；生产调用方未实施，见 §4 与 §7.2）、`internal/templates/static/js/ui/write-base.js`、`public/test/support` |
| 主审集中验收负责人 | **单一人**（本主代理），只有他能跑验收命令；各实现代理只写源码 |
| 当前写入 owner | `pkg/i18n/content_write.go` 的 **R1 唯一 owner = `bc9ceb11-c742-44bd-bac0-07e96828bd91`**；T1 已停写，该文件现由 R1 写入（见 §7.1 末行） |
| 规划任务书 | navigation 下一批由**规划代理 `1de0e098`** 落实；本文只引用其待审任务书 `public/docs/navigation译文MVC下一批规划草案.md`，**不自行另造并发设计** |
| 验收授权 | 用户已批"你自己规划完成所有的任务吧。我休息了"，主审自主推进；**不再把已裁定问题列为待用户答复**（§7 已改为已裁定/在办台账） |
| 保留行为 | 见 §1.2（不可作为迁移借口删除） |

### 1.1 已有脏改动边界（其他代理持有，本代理不碰）

- **product MVC 范例**（已验收，基线 `91062459`）：`internal/templates/static/js/ui/write-base.js`、`internal/templates/partials/ui_scripts.html`、`internal/templates/static/css/theme.css`、`pkg/binding/binding.go` 的范例侧改动属于已验收基线，**不视为本域进度**。`pkg/i18n/content_write.go` **不再归入范例**：它现由 R1（`bc9ceb11-c742-44bd-bac0-07e96828bd91`）写入，见 §7.1 末行。
- **publication SEO 审计域：有实现，也已有集中验收与 G6 定向浏览器验收证据**（§6.1 逐项列出）。本代理本轮**未重跑**这些命令，结论只引用已落盘日志。
- **navigation 域**：仅 `pkg/i18n` 的 `UpsertTx`/`requireTx`（`pkg/i18n/content_write.go`，`git diff` 显示 +195/-17）已交付，**交付形态是源码**：T1 最终修复轮（`.superpowers/sdd/navigation译文MVC下一批规划草案/T1-final-fix-report.md:4`）明文「**本轮未运行** go build / go test（任何包）/ go vet / 门禁脚本 / 真实 DB 用例 / 浏览器」，`:7`「**不宣称任何用例通过**」，`:88`「以下均为源码直读结论」。`public/test/pkg/i18n/content_write_tx_test.go` 是本树新增未跟踪的**测试源码（未运行）**，不得据此记为「单测已跑/已通过」；红绿由集中验收执行（`:89` 要求 `go test ./public/test/pkg/i18n/ -run 'TestUpsertTx|TestUpsertGrouped' -v`，需真实 PG）。生产调用方（navigation handler）全部未实施。
- **R1 进度口径**：R1（`bc9ceb11-c742-44bd-bac0-07e96828bd91`）仍是 `pkg/i18n/content_write.go` 唯一 owner；树内已出现新文件 `public/test/pkg/i18n/content_read_tx_test.go`（33.1K），但 R1 **尚未出报告**，故不宣称任何 R1 项完成、不宣称冻结。
- 未跟踪资料（`public/docs/navigation译文MVC下一批规划草案.md`、`public/docs/publication-SEO审计MVC实施任务书.md`、`public/docs/publication-SEO审计MVC验收命令清单.md` + `.superpowers/sdd/*`）不属代码。

**本代理本轮实际核实（只读）**：`git rev-parse HEAD` = `87902f126208c6fe11786becaff5caa102e1cb52`，分支 `refactor/mvc-publication-seo`；`git status --porcelain` 为 20 项已跟踪修改/删除 + 13 项未跟踪（比开工时 39 项多出 `theme.css`、`write-contracts.json`、`pkg/binding/binding.go`、`pkg/i18n/content_write.go`、`public/test/support/admin_session.go`、`public/migrations/697_seo_audit_i18n.sql`、`register_seo_audit_i18n.go`、`internal/templates/static/css/seo_audit_hidden_contract_test.go`、`pkg/binding/json_limited_test.go`、`public/test/pkg/i18n/content_write_tx_test.go`）。文件存在性与行数已逐一核对，与下表一致。

### 1.2 必须保留的行为

1. `/api/navigation/update` 路径与权限点 `navigation:update` 不变；`Fanout` 行为不变（不放进事务）。
2. publication 双权限：声明式 `publication:seo_audit` + `CasbinMiddlewareForPath("/api/seo/audit")`（虚拟对象，`permission.Declare` 原位保留）。
3. 商品 `WithRedirect`（发布 URL 迁址 301）是业务语义，不是操作导航。
4. 共享 `internal/seo/scoreview` 仍被 `internal/module/project/inbound/http/project_page.go:58/157/312` 与 `internal/module/content/inbound/http/content_page.go:111/1190/1905` 使用 —— **本批不清退**。
5. `binding.JSON` 同时接受 JSON 与表单体，不得仅为命名改成只接 JSON。

---

## 2. 角色与单职责边界

| 代号 | 角色 | 只做 | 绝不做 |
| --- | --- | --- | --- |
| **R** | 范围与调用方核查（只读） | 读源码/文档/图谱，产出符号清单、调用方清单、缺口登记 | 写任何文件、下"已通过"结论 |
| **PLAN** | 任务书编写（文档） | 层归属表、接口表、owner 表、验收矩阵 | 改代码、改 progress、改主任务书 |
| **BE** | 后端 service/model | 本域 service、model、事务边界、工程范围、完整分页 | 跑测试、改 dto/enums/http/前端 |
| **PROTO** | DTO/enums/HTTP 契约（**单一 owner**） | dto、enums、inbound/http handler、权限常量声明 | 与 BE 并发改同一 service 文件 |
| **FE** | 前端模板/JS | Jet、JS、DOM/浏览器行为测试源码 | 发明请求字段、复制业务算法、改 Go |
| **ASM** | 装配/路由/权限 | `internal/routers/*`、模块 router、assembly、台账减项 | 改业务实现、跑验收 |
| **TEST** | 测试源码 | 只写测试文件（含 feature），真实迁移建表 | **运行**任何测试/构建/门禁/连库 |
| **REV** | 独立只读复审 | 只读 diff + 反例推演，出缺陷清单 | 改文件、跑验收、复述实现者结论 |
| **E** | 主审集中验收 | 全批完成后统一跑命令、汇总失败、定向复验 | 实现阶段查实现点、写生产代码、放宽断言 |

**固定路由（本批）**：所有子代理用 `open-design-3/deepseek-v4.1-flash`；**不启用 Agent Teams**。一个代理每次只领一个清晰任务；不按函数切片。

**一文件一 owner**（跨域冲突一律按依赖序串行，不并发覆盖）：

| 文件 | owner | 文件 | owner |
| --- | --- | --- | --- |
| `publication/service/publication_feed.go`、`service/seo_audit_result.go` | BE | `dto/publication_req.go`（`SeoAuditReq`）、`enums/publication_enums.go` | PROTO |
| `publication/inbound/http/seo_audit_handle.go`、`publication_router.go` | PROTO | `analytics/.../seo_page_handle_test.go` | PROTO（旧断言迁移） |
| `templates/admin/analytics/seo.html`、`ui/seo-audit.js`、`ui/write-base.js`(`data-write-latest`) | FE | `public/test/publication/feature/seo_audit_json_test.go` | TEST |
| `internal/architecture/controller_shape_test.go`、`text_domain_test.go`、`publication_seo_audit_boundary_test.go` | ASM（台账）/ TEST（新断言） | `pkg/i18n/content_write.go` | **R1 = `bc9ceb11-c742-44bd-bac0-07e96828bd91` 唯一 owner**（T1 已停写，见 §7.1 末行） |
| `public/test/support/admin_session.go`（`testRedisDBByDir` 领号） | ASM | `public/migrations/697_*.sql` + `register_seo_audit_i18n.go` | ASM（迁移单独一人） |
| `routers/assembly*.go`（navigation 端口装配） | ASM | `public/migrations/698_*.sql`、`schema/CURRENT.sql` | ASM（迁移单独一人） |

**测试源码归属规则**：实现者默认自带本域单元/组件测试源码；只有需要独立文件（feature 全链、架构反证）时才由 TEST 补，**不双 owner**。TEST 与实现者不得同时改同一测试文件。

---

## 3. 屏障与验收预算

```
A（范围锁定/层归属/接口表，R+PLAN）
   ↓
B（后端契约与业务边界：PROTO → BE，PROTO 先出接口表）
   ↓
C（前端与调用方：FE；与 B 只通过接口表耦合，可并行但不得改 B 的文件）
   ↓
D（实现完成屏障：**全部写手停写** → ASM 台账/装配收口 + TEST 补齐 + gofmt + 差异回读）
   ↓
E（主审集中验收，一次开跑）
```

**D 屏障的触发条件**：`pkg/i18n/content_write.go` 的 R1（`bc9ceb11-c742-44bd-bac0-07e96828bd91`）**停止写入之后**才允许进 D 与集中验收。R1 在写期间，对该文件的任何结论只能是"进行中快照"，**不得宣称冻结**，也不得对其跑以"通过"为目的的验收组。

- **第一轮全部失败汇总**：E 一次跑全部命令，收集所有失败（按"本次缺陷 / 旧测试断言旧协议 / 环境不可用 / 既有无关失败"四类归档）。
- **后续只跑失败项 + 本次修复的影响面**，不重跑已过的组，不重跑全批。
- **不默认跑** `go test ./...` / `-race` 全量；全量只在合并进 main 前一次。
- **PG**：优先 `GOWP_REQUIRE_PG=1`，隔离测试库（`support.NewMigratedPGTestDB`）；**主库禁写**。`require_env.go:16-18`：`GOWP_REQUIRE_PG=1` 时缺库 = 失败而非跳过。
- **skip 必须单列**：任何 `t.Skip` 计为"未验证"，不能并入通过数。
- **门禁**：`bash scripts/check-all.sh`（plain 组）与需要真实 PG 的 `DB_SCRIPTS`（`check-write-endpoints.sh`、`check-permission-gaps.sh` 等）分两组报告；先读数组避免重复执行相同检查。
- **DB_SCRIPTS 集中验收规则**：本批**API/权限变更涉及的项**必须集中验收（`check-write-endpoints.sh`、`check-permission-gaps.sh`、`check-menu-page-binding.sh`、`check-button-code-binding.sh`、`check-page-get-authz.sh`）；**已有通过的项在其依赖输入未变时复跑，不重复验收**。已通过的原始证据登记在 §6.1，引用而非重跑。
- **环境隔离与 skip 单列**：每条命令首行必须是显式 `PGDATABASE=wp_seoaudit_accept` + `TEST_REDIS_ADDR=127.0.0.1:6390`（独立 Redis 实例，db13 应用 / db1..8 测试）。首轮 G2 未导出 `PGDATABASE` 而回落到 `wp_test`（`public/test/support/pgtest.go:137` → `:39` `DefaultPGDatabase = "wp_test"`，`wp_test.xact_commit` 59829→59871，+42）——这是**已记在案的执行偏离**，后续所有组必须显式导出。skip 与"未覆盖"各自单列，都不进通过数。

---

## 4. navigation 本域最小依赖界定

**本域完成所必需（缺一不可）**：

1. 事务读写：`pkg/i18n` 的 `UpsertTx` + `LoadDetailsForProjectTx`/`LoadTargetsForProjectTx`（R1 已批准；只读调用方事务，不自开、不设 scope，**无 FOR UPDATE**）。
   - 本树实际签名：`func (w *ContentWriter) UpsertTx(ctx context.Context, tx *gorm.DB, items []ContentWriteItem) (written int, err error)`；事务判别 `requireTx(tx)` → `ErrContentTxRequired`（nil / typed-nil / 连接池句柄 / 已失效句柄）；跨工程一批 → `ErrContentProjectMixed`（调用方须先按工程分组）。**事务边界与分组只留在本方法**：`UpsertTx` 不新开事务、不改调用方 scope。
2. stale 传播：page/presentation 侧 `MarkStaleByDependencyTx`（Tx 端口，必须与译文写同事务）。
3. 可靠菜单任务：`menu_stale` 入队（`EnqueueMenuStaleTx` + 原子号源 `tbl_build_job_seq` + 部分唯一索引 `(context_key, input_version)`）+ 同批执行器注册。**入队无消费 = 永久 pending**（`build_worker.go:37-100` `ErrExecutorMissing`）。

**已裁定（主审，不再列为待裁）**——取代本文此前 §7 的 D-9.1~D-9.4 提问：

| 裁定 | 内容 |
| --- | --- |
| 旧 worker × 新成功 | **不接受「旧 worker 覆盖新成功」**。锁内复验并激活线性化：worker 写回/激活前必须在锁内重新确认自己仍是该 source 的最新租约持有者。 |
| page 更新后取消外部时间/stale 跳过 | 允许取消（`RebuildStalePages` 不再以外部时间/stale 作为前置跳过依据），改由单条原子领取 SQL 判定。 |
| 必要跨 source 互斥 | **纳入本域最小依赖**（不推给通用治理）；对应原 D-9.3。 |
| 译文 + stale + 可靠入队 | **同一事务**：译文写、page/presentation stale 标记、`menu_stale` 入队三者同 PG 事务。 |
| 译文与 FS 激活 | **不是**「译文和文件系统激活强塞同一 PG 事务」。FS 激活仍在 DB 提交之外，由 pending 回执 + 启动恢复收敛。 |

**通用发布内核的历史缺口（另列）**：

| 缺口 | 结论 | 处理 |
| --- | --- | --- |
| `persistMultiLangArtifacts` / `commitPublish` 非单事务（文件系统激活与 DB 提交有窗口） | 已由 pending 回执 + 启动恢复收敛 | 登记，不在本批修 |
| `instanceStateFingerprint`（`presentation_publish.go:335-346`）不含译文/菜单输入 | 只译文变化时指纹相等 | 登记；本批不启用 `depsEpoch` |
| worker 执行期不校验租约（`build_worker.go:92-99` 仅结束写状态时校验） | "旧 worker 不会回写陈旧产物"在现状下**未证明** | **已裁**：改为锁内复验 + 激活线性化（见上表）；实现落地前不得宣称成立 |
| claim 互斥粒度是 `(source_type, source_id)`，跨 source 不互斥 | 两执行器可同时重建同一实例 | **已裁**：必要跨 source 互斥纳入本域最小依赖（见上表） |
| 实例锁是进程内 64 分片 mutex（`presentation_service.go:147-162`），前提单实例部署 | 跨进程无强互斥 | 登记 |

**什么样的反例让本域完成"受阻"**（出现即判未完成，不得以"通用治理"回避）：

- 译文写入成功但菜单任务未入队 / 入队后无执行器 → 菜单永久陈旧。
- 译文写成功而 page/presentation stale 标记在另一事务失败 → 用户看到"已保存"，发布不更新。
- 读现值失败仍继续写；或在锁外读现值再改（TOCTOU）。
- 工程 A 的写入落到全局行或工程 B。
- 空 target 的不可翻译行导致正常保存 409。
- 处理头/footer 两个 kind 时 `context_key` 相同导致第二条任务被 `DoNothing` 吞掉。

**裁定方式（收窄）**：上面那批通用缺口**不是一律不阻塞**，而是**按具体反例是否破坏本域承诺逐条裁定** —— 上表"处理"列就是逐条裁定结果。已裁定项按裁定执行；实现未落地前，不得宣称对应承诺成立。

**不得与之矛盾**：不得一边把某项登记为"通用治理、本批不管"，一边又承认它已经破坏本域承诺。最具代表性的一条：**必要跨 source 互斥已裁定纳入本域最小依赖**，因此不能再以"跨 source 不互斥属通用内核缺口"为由为本域开脱。

**边界**：不得用"沉淀通用发布内核治理/全仓共享能力"替代本题的 MVC 职责迁移；同样不得把已纳入本域依赖的项推回通用治理来换取停下。

---

## 5. 候选模块（依据现有总目标，非全仓盘点）

已核实证据：`grep -rl "BareSlot"` 找到 9 个模块 router 共 32 处 BareSlot；`pageview.Render` 出现在 25 个模块。**这是形态证据，不是完成状态**。

| 模块 | 本域对应域 | 状态 | 证据 |
| --- | --- | --- | --- |
| **publication + analytics SEO** | SEO 审计 JSON 化 | **已通过集中验收 + G6 定向浏览器验收**（残留项见 §6.2） | `internal/module/publication/service/seo_audit_result.go` 55 行 / `internal/module/publication/inbound/http/seo_audit_handle.go` 62 行 / `internal/templates/static/js/ui/seo-audit.js` 315 行 / `public/test/publication/feature/seo_audit_json_test.go` 1246 行 / `internal/architecture/publication_seo_audit_boundary_test.go` 262 行 / `public/migrations/697_seo_audit_i18n.sql` 63 行，均已实地确认存在；`internal/module/publication/inbound/http/publication_router.go:56` 已无 BareSlot；架构台账减 3 条（`controller_shape_test.go` 3 处 + `text_domain_test.go` 1 处，共 4 行删除） |
| **navigation** | 译文写入事务化 + 读侧工程优先 | **规划，未实施**；R1（`bc9ceb11-c742-44bd-bac0-07e96828bd91`）正在写 `pkg/i18n/content_write.go` | `UpsertTx`/`requireTx` 已交付**仅源码、未跑 test/build/PG**（`T1-final-fix-report.md:4/:7/:88`）；`navigation_handle.go`/`navigation_translations_handle.go` 未动；`public/test/pkg/i18n/content_read_tx_test.go` 已出现但 R1 未报告，不计完成；D-9.1~D-9.4 **已裁定**（§4），任务书**由规划代理 `1de0e098` 独占**（草案与独立 R1 简报收敛），主审不再与其同时改主任务书 |
| **admin** | 权限/角色/菜单/部门/字典页 | **待核实**（BareSlot 8、POST 28，多为抽屉片段） | 未读语义 |
| **ai** | provider/model/token 片段 | **待核实**（BareSlot 5、POST 20） | 未读语义 |
| **content** | 文章页 + scoreview 消费者 | **待核实**（BareSlot 5、`content_page.go:1905` 仍用 scoreview） | 商品记录明示"文章契约缺工程过滤" |
| **project** | 工程页 + scoreview 消费者 | **待核实**（BareSlot 5、`project_page.go:312`） | 同上 |
| **order / purchase / inventory** | 订单与库存写链路（页面多、写端点 6 处） | **待核实** | 事务/批量原子性是重点，非本批 |
| **block / page / contenttemplate / workbench** | 构建与可视化域 | **待核实** | 与发布内核耦合，需单独判据 |

**未核实项一律不作为"待做的完整模块清单"**；每域开工前必须先出 §2 的层归属表与调用方清单。

---

## 6. 本批允许的实现边界与交付证据

**允许实现停在哪一步**：

- 允许：文件级改动完成、测试源码写好、gofmt、`git diff --check`、静态符号复查。
- 禁止（**实现代理**）：跑 build/test/vet/门禁/DB/浏览器、提交、合并、推送、清理工作树、改 `progress.md`。
- **例外**：集中验收由主审单人执行（§2 角色 E），该角色本来就跑验收命令；上一条限制只约束实现代理。

**交付需要哪些证据**：

| 证据 | 形态 | 状态 |
| --- | --- | --- |
| 层归属表（每处改动属哪层） | 文档表格 | **每域开工必填** |
| 接口迁移表（旧入口→新入口、字段、成败协议、权限点） | 文档表格 | publication SEO 已有（任务书"冻结接口"节）；navigation 已有（草案 §4.3/§4.7） |
| 文件 owner 表 | 本文 §2 | 已有（publication/navigation）；待扩域时补 |
| 删除符号清单 + 全调用方复查 | 报告 | **待补**（publication 域复审时出；已删符号见 §6.1 第一行） |
| 测试源码清单 | 文件列表 | publication 已写 7 个：`internal/module/publication/service/seo_audit_result_test.go`、`internal/module/publication/service/seo_audit_test.go`（既有，已按新事实类型改）、`internal/module/publication/inbound/http/seo_audit_handle_test.go`、`public/test/publication/feature/seo_audit_json_test.go` 1246 行、`internal/templates/static/js/ui/seo-audit.test.cjs` 675 行、`internal/templates/static/js/ui/write-latest.test.cjs` 363 行、`internal/templates/static/css/seo_audit_hidden_contract_test.go`；另 `internal/architecture/publication_seo_audit_boundary_test.go` 262 行、`public/test/pkg/i18n/content_write_tx_test.go`、`pkg/binding/json_limited_test.go` |
| 集中验收日志 | `/tmp/...` 路径 + 退出码 + skip 清单 | **已有**，见 §6.1 / §6.2（`/tmp/wp-seoaudit-acceptance-20261030-1008/` + `round2/`） |

### 6.1 publication SEO · 已通过项（真实日志，逐条可核对）

目录：`/tmp/wp-seoaudit-acceptance-20261030-1008/`（首轮 E）与 `/tmp/wp-seoaudit-acceptance-20261030-1008/round2/`（定向复验）。

| 项 | 日志 | 退出码 | 实际覆盖 | 被本轮复核判定 |
| --- | --- | --- | --- | --- |
| 构建 | `01-build.log` | `G1_RESULT build_all=0 build_cmd=0` | `go build ./...` 4424ms + `go build -o /tmp/wp-seoaudit-acceptance ./cmd` 2799ms | 通过，未被后续改动推翻前有效 |
| service 包 | `round2/f1-service.log` | 0（959ms） | `ok go_wp/internal/module/publication/service 0.014s` | 通过 |
| inbound/http 包 | `round2/b-http-arch.log` | 0（2726ms） | `ok go_wp/internal/module/publication/inbound/http 0.950s` | 通过 |
| 架构断言 `^TestSEOAudit` | `round2/b-http-arch.log` | 0（331ms） | `TestSEOAuditResultTypesCarryFactsOnly` / `TestSEOAuditHasNoPageExit` / `TestSEOAuditRouteShape`（含 missing / json / other_page / duplicate / multiline_slot / page_slot 六个子测）全部 PASS | 通过 |
| 架构断言（9 个函数组合） | `04-architecture.log` | `G4_RESULT=0` | controller 六项 + `TestTemplateTranslateKeysStayInUIDomain` + `TestMsgKeysStayInUIDomain` + `^TestSEOAudit` | 通过 |
| publication feature 全链 | `round2/f5-feature.log` | 0（8994ms） | `ok go_wp/public/test/publication/feature 6.315s`，`GOWP_REQUIRE_PG=1 GOWP_REQUIRE_REDIS=1` 真隔离 | 通过；含 `TestSEOAuditRequiresSessionCSRFAndBothPermissions`（`public/test/publication/feature/seo_audit_json_test.go:940`） |
| analytics feature | `03-feature.log` | 内层 ok | `ok go_wp/public/test/analytics/feature 10.936s`（同批 publication feature 8 条因 Redis 号未登记失败，与 analytics 无关） | 通过 |
| Redis 号分配断言 | `round2/f5-feature.log` | 0（2278ms） | `TestTestRedisDBAllocationMatchesCallers` ok | 通过；`public/test/support/admin_session.go` 已领号 `"public/test/publication/feature": 8` |
| JS 组件测试（seo-audit） | `round2/f3-seo-audit-final.log` | `exit=0` | `tests 21 / pass 21 / fail 0 / skipped 0`，106.5ms | 通过 |
| JS 组件测试（write-latest） | `round2/f3-js.log` 第二段 | 0 | `tests 15 / pass 15 / fail 0` | 通过 |
| i18n seed 门禁 | `round2/f2f4-gates.log` | 0 | `admin/workbench 被引用但未 seed 的 i18n key 数：0`（首轮 0→21 已由迁移 `public/migrations/697_seo_audit_i18n.sql` 补齐，63 行，`ON CONFLICT (item_key, lang) DO NOTHING`） | 通过 |
| 写表单字段门禁 | `round2/f2f4-gates.log` | 0 | `✓ 写表单字段名全部对得上（59 个表单 vs 111 条端点契约）` | 通过 |
| go vet | `round2/others-gates.log` 第 1 段 | 0（9412ms） | `✓ go vet 通过（含测试文件编译）` | 通过；首轮 F1 的 `it.Message undefined` 已随 `internal/module/publication/service/seo_audit_test.go` 修改消失（该文件现为已跟踪修改） |
| 静态扫描 | `round2/others-gates.log` 第 2 段 | 0（3612ms） | `✓ Go 静态扫描通过（共 2 组包）`，含 `ok go_wp/public/test/architecture 0.666s` | 通过 |
| 写端点契约生成 | `07-write-contracts.log` | 0（4352ms） | `go run ./cmd/write-contracts` → `internal/templates/static/js/ui/write-contracts.json` +1 行 | 已生成，属实际生成物变更（任务书 E:97 允许） |
| 迁移 + 重建 | `08-migrate-retry.log` / `08-seed-and-rebuild.log` | 0 / 0（2419ms） | `go run ./cmd -migrate-only` 装上 697 seed（`admin.seo.audit.*` 36 key × 2 语言 = 72 行）；首次失败因手工插入的 casbin 策略缺 `v3`（`invalid policy rule size: expected 4, got 3`），清 2 行后成功 | 通过 |
| 首轮全量门禁基线 | `05-check-all.log` | `G5_RESULT=1`（`✗ 门禁未通过（5/38）`） | 5 个失败：`check-i18n-keys-seeded.sh`、`check-static-scans.sh`、`check-go-vet.sh`、`check-js-controls.sh`、`check-write-fields.sh` | 5 项中 4 项已有 pass 证据（上表）；`check-js-controls.sh` 见 §6.2 |
| DB_SCRIPTS（权限/写端点/菜单/按钮/GET 鉴权） | `05-check-all.log` | 通过 | `✓ 页面写端点鉴权齐备（POST 注册点 377 个：声明式授权 230、显式 Casbin 129、访客 JSON 0、豁免 18）`；`✓ 写表单端点全部已注册（80 个端点 / 路由表 382 条）`；`✓ 没有缺口`；`✓ 权限点与路由双向同源`；`✓ 菜单 path 全部命中真实页面路由`；`✓ 模板按钮码与菜单节点绑定完整` | **已通过，本批不重复验收**（§3 规则） |

**G6 定向浏览器验收**（`round2/g6-resume-verification.md`，环境 8099 + PG `wp_seoaudit_accept` + Redis 6390/db13）：

- 样式定向测试 exit0（2 passed）、样式包 exit0（6 passed）、图谱 update exit0。
- 禁用缓存后 1440/768/375 真实表单矩阵：初始 placeholder、empty/clean/issues 互斥、overflow 均 0；坏契约与模拟 403 保留旧 issues 并展示失败提示。
- `title_duplicate` 路径 /a、/b、/c 完整；键盘 Enter 展开、再 Enter 收起成功；HTML 样式标题无 img 节点。
- 逆序响应分阶段读取：新 22 落地、旧 11 返回仍 22；摘除后 33 返回仍 22；新响应后按钮可用。
- 真实端点点击成功、empty 唯一可见、URL 不变；缺 CSRF 的真实 POST 返回 403 + `application/json` + CSRF 校验失败。
- 截图：`g6-desktop-fixed.png`（1440×1000）、`g6-768-fixed.png`、`g6-375-fixed.png` 为视觉证据；`g6-hidden-css-fixed.png` 有遮挡，**不作视觉证据**。
- 页面定向 `TestSEOPage` 3 passed（job bash-424 exit0），含无权限动作隐藏。
- 记录在案的方法学修正：初始 `requestAnimationFrame` 脚本超时 → 以 MutationObserver 终态矩阵为准；初始 microtask 竞态探针无效 → 以分阶段 22/22/22 为准。模拟 403 仅作消费者证据，不是真实权限链证据（真实链由 `f5-feature.log` 覆盖）。

### 6.2 publication SEO · 未覆盖 / 仍未通过（不得并入通过数）

| 项 | 状态 | 证据 |
| --- | --- | --- |
| `check-js-controls.sh` 全绿 | **仍未通过** | `round2/others-gates.log` 第 3 段：`✗ seo-audit.test.cjs 未通过` / `✗ 前端控件行为测试未通过（29 个文件，逐文件串行）`，exit=1（3472ms）。单文件定向跑 21/21 pass，与整脚本串联跑不一致，**根因未定** |
| 首轮 `f3-js.log` 记的 `write-latest.test.cjs` 8 条 `WBUI is not defined` | **已消失** | 同文件同批次第二段 `tests 15 / pass 15`，exit=0；`others-gates.log` 只点名 `seo-audit.test.cjs`。首轮 `99-summary.md` 的 F3 清单已被 `round2` 取代，**不沿用** |
| 首轮 `f3-seo-audit-full.log` 的 `exit=1` | **已消失** | 该日志为 `## DIAGNOSTIC re-run (single file, full stdout)`；之后 `f3-seo-audit-final.log` 21/21 pass exit=0 |
| `round2/f3-js.log` 第一段的 `actual: 0 / expected: 1` | **被后两份日志取代** | 该段 exit=1；`f3-seo-audit-final.log` exit=0。三者并存时以文件名含 `final` 的为准，但**不宣称整脚本门禁已过** |
| 全量 `go test ./...` / `-race` | **未执行（按预算不执行）** | 合并进 main 前一次 |
| 全量 `check-all.sh` 复核 | **未做**：只跑过 `--with-db` 首轮 + 逐脚本定向复跑 | 首轮 `05-check-all.log` 只留 plain 组 38 脚本的结论，`DB_SCRIPTS` 组未单列独立日志；其结论仅在 `05-check-all.log` 混合输出中 |
| `schema/CURRENT.sql` 快照 | **未见重生成证据** | 697 只动 seed 行，不改结构；`schema-snapshot.sh --check` 按任务书 :99 不跑 |
| navigation 域 | **无任何验收** | 未实施 |

**本矩阵对旧结论的更正**：此前"publication 零验收证据、本树无任何本批结果"**是错误的历史回退**，与 `round2/` 实际日志冲突，作废。正确的表述是：**集中验收已执行一轮 + 定向复验多轮，13 项通过、`check-js-controls.sh` 整脚本仍未通过，其余按 §6.2 单列为未覆盖。**`round2/g6-resume-verification.md` 内部另有自己的"未重跑全量/未宣称全模块完成"限定，与本表口径一致，不冲突。

**本代理本轮未运行任何验收命令**——这是"本轮未运行"，不是"历史上没人运行"。

---

## 7. 已裁定台账与在办项（不再列待用户答复）

用户已授权自主推进，本节只记录**已裁定**与**在办**，不写成问题清单等答复。

### 7.1 已裁定（生效）

| 原编号 | 原问题 | 裁定 |
| --- | --- | --- |
| D-9.1 | 是否接受"本批不建执行期租约校验，旧 worker 可跑完并完成激活" | **不接受**。改为锁内复验 + 激活线性化（§4 已裁定表） |
| D-9.2 | `RebuildStalePages` 是否保留前置跳过判定 | **取消外部时间/stale 跳过**，改由单条原子领取 SQL 判定 |
| D-9.3 | menu_stale 与 presentation 同时重建同一实例是否要求互斥 | **要求**。必要跨 source 互斥**纳入本域最小依赖** |
| D-9.4 | 菜单译文与激活时序是否必须同事务 | **译文 + stale + 可靠入队同事务**；但**不是**译文与 FS 激活强塞同一 PG 事务 |
| 原 §7.5 | `check-permission-gaps.sh` / `check-write-endpoints.sh` 是否必需 | **必需**：本批 API/权限变更涉及的 `DB_SCRIPTS` 项集中验收；**已有通过且输入未变的不复跑**（证据 §6.1 末行） |
| 原 §7.6 | `pkg/i18n/content_write.go` ownership 冲突 | **不冻结**。R1（`bc9ceb11-c742-44bd-bac0-07e96828bd91`）为唯一 owner 正在写入，T1 已停写；**停写后**才到 D 屏障与集中验收 |

### 7.2 在办项（主审自主推进，不等答复）

| 在办项 | owner | 完成判据 |
| --- | --- | --- |
| navigation 下一批任务书（§4 已裁定项落地为可执行方案） | 规划代理 `1de0e098` | 出稿并由主审审；本文**只引用**，不自行另造并发设计 |
| `pkg/i18n/content_write.go` 事务读写收口 | R1 `bc9ceb11-c742-44bd-bac0-07e96828bd91` | 停写 → 进 D → 集中验收；在此之前不对其宣称冻结或通过 |
| `check-js-controls.sh` 整脚本失败根因 | 本批 JS 组（FE） | **历史结论：整脚本 fail 不得当「未修」记账**。已有单文件定向证据 `round2/f3-seo-audit-final.log`：`tests 21 / pass 21 / fail 0 / skipped 0`；整脚本串联 fail 根因仍未定位（疑似 29 文件串行全局状态或超时）。记为**最终本批受影响 JS 组集中验收必须覆盖项**，不宣称已重跑整脚本 |
| publication 域删除符号清单 + 全调用方复查 | REV | 列出已删 `publication_seo_audit_resp.go` / `seo_audit_result.go`（http 侧）/ `seo_audit_result_test.go`（http 侧）/ `seo_audit_result.html` 的全调用方复查结论 |
| 续跑限制的事实记录（历史） | 主审 | `create_goal`/续跑工具返回 `the model cannot resume a paused goal; the user must resume it` —— 属宿主侧持久任务限制的**历史事实**。**该限制已不成立为当前阻塞**：本轮 `get_goal` 核实 goal 为 `revision 3 / active / armed / round 2`（Ruling：旧 paused 限制只作历史保留，不作为当前阻塞理由）。仍不得记为「自动续跑已成功」，也不得据此停下写入 |
| PROTO 契约增量（navigation 下一批） | PROTO `3cb8fa86-6eb7-41c9-9ffb-15fa94010b83` | 唯一新增 navigation `dto.UpdateEnvelope` 及专用测试；**不改 handler**。完成后进 D 屏障 |
| FE 写入基座 opt-in | FE `f9cf6452-a552-4c0d-9cad-4c61fdf26e25` | 唯一改 `write-base.js` + 对应测试 + `navigation_translations.html`；`data-write-items` opt-in，**不改 rows 协议** |
| 后续域总盘（只读） | R `0fc50c17-d810-4537-b0cf-7e6e567fed38` | 只读盘点总规范后续域，唯一产出报告 `global-next-domains-inventory.md`；不改代码/矩阵 |
| navigation 草案与 R1 简报收敛 | PLAN `1de0e098`（独占） | 独占 `navigation译文MVC下一批规划草案` 与独立 R1 简报；主审与之**不同时改主任务书** |

### 7.3 环境判据（承接首轮偏离，已修正为规则）

- 所有命令必须显式 `PGDATABASE=wp_seoaudit_accept` + `TEST_REDIS_ADDR=127.0.0.1:6390`；应用监听 8099（8080 被 `gowp-builder-au` 占用）。
- `config.yaml:89-96` 与应用 db 号（db13）不得指向在用实例 `127.0.0.1:6379`；测试逻辑库由 `public/test/support/admin_session.go:46-55` 的 `testRedisDBByDir` 分配（1..15），未登记目录 `testRedisDBForCaller`（`:69-72`）直接 `t.Fatalf`。
- **禁止**对任何在用 Redis 库执行 `flushdb`/`flushall`；`public/test/support/` 现状无 flush 调用点。
- 门禁脚本取 `GOWP_DATABASE_DBNAME`（不是 `PGDATABASE`），`GOWP_AUDIT_CONFIG` 必须绝对路径（`go test` 的 cwd 是包目录）。
