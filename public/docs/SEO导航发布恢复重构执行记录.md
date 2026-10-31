# SEO 导航 / 发布恢复重构执行记录

> 分支 `fix/seo-nav-publish`（起点 `main@40991017`，即 `refactor/product-taskbook` 与 main 合并后的状态）。
> 本机工作树 `/home/sky/project/go/wp/.worktrees/product-refactor`。
> 目的：把 main 快照 `78bd5e0f`「保存SEO导航MVC及发布恢复未完成快照」留下的爆红与未闭合竞态修完，
> 使其达到与商品模块重构同一条验收线（build / 全量测试 / 门禁 / 台账）。

## 1. 起点与判据

main 快照自述「第七轮构建仍失败，已知发布竞态未闭合」。实测起点：

- `go build ./...` 失败 18 处（语法级 2 + 类型级 16），由 `ecc24a17` 补齐缺口后转绿。
- 测试侧：internal 28 例红（含 architecture 6、routers 2）、public 179 例红（10 个包），
  其中 `public/test/page/{unit,feature}` 为 **600s 超时**而非断言失败。
- 门禁：`check-write-fields.sh` 红（main 自带，见 §5）。

判据全部取「最终迁移后的真实 schema + 生产装配入口」，不用 `init_builder_schema.sql` 直接抄列名
（该文件有多列已被 155/205/305 等迁移改名或删除）。

## 2. 生产缺陷（12 处，提交 40899d5f）

| # | 位置 | 缺陷 | 后果 |
|---|---|---|---|
| 1 | `internal/module/build/model/build_job_seq.go:46-58` | `seqRow` 从未写入入参 projectID/kind，gorm 把零值显式写进 VALUES | `invalid input syntax for type uuid: ""`，所有创建导航/菜单项失败 |
| 2 | 同上 `:58` 的 OnConflict SET | `next_version + 1` 未限定表名 | `column reference "next_version" is ambiguous (42702)`，波及 179 例 |
| 3 | `internal/module/page/service/page_publish.go:1524` | 已持 `ProcessPathLocker` 分片锁时再经 `publisher.UpdateURL` 取同一把**非重入**锁 | 自死锁，page/feature 600s 超时 |
| 4 | `page_publish.go:3225-3277` convergeSource | pending 批次「全部 fail closed」时 failed==0、handled>0，无进展仍继续领同一批 | 无限重放（45s 内 87796 次 ClaimPendingReceipts），page/unit 600s 超时 |
| 5 | `page_publish.go:1460-1489` | 改址只在 FS 切换前查**新**路径占用，旧路径归属靠事务内 RenameReservedTx 才判 | 先破坏他人访问面再失败，且无法回滚 |
| 6 | `page_publish.go:2147-2180` settleRouteWrite | 旧路径已归他人时未归一成 errOldPathTaken | 回执被误判成功，301 落错目标 |
| 7 | `page_publish.go` settleOldPath（原 commitOldPathSettle） | 在 gorm 事务回调内调 `tx.Commit()`，回调返回后 gorm 再 Commit | `sql.ErrTxDone` 被原样返回，**每次**旧路径处置都判失败 |
| 8 | keepPublishReceiptPending 之后的 6 处 | keep pending 后仍无条件 complete | 回执被结案，重放永不发生 |
| 9 | `presentation/service/presentation_publish.go` verifyDeactivatedPathOwner | 传 nil tx 调 `RouteOwnerTx` | `gorm.(*DB).Session(0x0)` SIGSEGV |
| 10 | `presentation_publish.go:466-486` | 回执登记时 artifactID 尚未赋值（事务内才生成） | switch_active 回执登记必失败（ErrInvalidParam） |
| 11 | `presentation_publish.go` oldPathFSTargetAllows{Redirect,Deactivate} | 把常规 kind=page 判成外来占位 | 301 每次被跳过，update_url 回执永停 pending |
| 12 | `presentation/model/presentation_model.go:854` PersistPublishInputURLTx + freezePublish | 事务内同写 stale=true，freezePublish 未同步 Stale | 提交段指纹校验把「自己刚置的 stale」判成 errPublishStateMoved，每次改 URL 白重试 +2 代 |

另修：
- `presentation_publish.go` receiptIdentityVerdictTx 增「账本 receipt_id == links[lang]」一档 ——
  账本存的是 switch_active 回执 id，update_url 回执 id 永不出现；缺此档旧路径重放永远判 superseded。
- `convergeSupersededAttemptTx` 在持行锁事务内调自开事务的 MarkStaleWithoutVersion（自锁挂死）
  → 新增 `MarkStaleWithoutVersionTx`。
- `page/service/page_redirect.go:230-252`：RouteOwnerTx 只认调用方事务，改 TransactionScoped 只读探测。
- `publication/service/publication_receipt.go`：`CapturedInputVersion` 的 omitempty 改 omitzero
  （jsonx 走 encoding/json/v2，omitempty 不省略 0）。
- `presentation_model.go:833-836`：`var rows []struct{...}` + `Model(&rows)` 生成 `UPDATE "" SET ...`
  （SQLSTATE 42601）→ 改 `InstanceEntity`。
- `publication/contract/publication_service.go`：新增 RouteOwnerResp / PendingReceiptResp 类型别名，
  page/presentation 不再直连 publication/dto（门禁 TestNoCrossModuleDTOImport）。

## 3. 测试口径纠正（提交 db572521，43 文件）

四类，全部是「测试写错了」而非放宽断言：

1. **schema**：`presentation_artifacts` 已无 `build_input_manifest`（305_manifest_dedup.sql:39 删除）且需 `lang`；
   `presentation_dependencies` 只有 `last_checked`；`presentation_publications` 从无 `create_time`；
   `receipt_data` 一律 `SELECT receipt_data::text`；`jsonb_build_object` 形参必须 `?::text`（否则 42P18）。
2. **装配**：navigation 写入口 `SetWritePorts` 缺任一端口即 fail closed（`navigation.err.translation.unavailable`）；
   菜单失效已由 MenuStaleDispatcher 改为**同事务端口**（`markMenuStaleInTx` / `markNavigationStaleInTx`），
   旧 dispatcher spy 恒 0 → 夹具改 `navStaleCounter` 装饰 Pages/Items 计数。
3. **语义**：stale 只由成功激活清除（page_tx.go:175 MarkPublishedLangTx；MarkStagedLangTx 注释明确「构建不等于发布」）
   → 「构建后仍 stale」「激活后不应 stale」；收敛按全表领取回执（page_publish.go:3237）
   → 一切 recovered/rolledBack 总数等式必须按回执 id 断言；注入点必须命中生产入口（ActivateTx 而非 Activate）；
   整批一个事务 → 失败后账本 0 行、产物一并回滚，现场改为真实部分状态或只钉最终一致。
4. **契约**：未登录/无权限一律 JSON（401/403），不因 Accept 或 HX-Request 跳转
   （internal/middleware/builtin/auth.go、casbin.go 注释即契约）；i18n 空输入零 SQL 判据由
   `SELECT current_query()`（PG 18 返回自身，恒假红）改为 GORM 回调计数；
   `db.Session(&gorm.Session{NewDB:true})` 不克隆 Statement（gorm.go:261-294），必须带 Context。

## 4. 验收证据

| 步骤 | 命令 | 结果 |
|---|---|---|
| 构建 | `go build ./...` | exit 0 |
| 静态检查 | `go vet ./...` | exit 0 |
| internal+pkg 全量 | `go test -count=1 ./internal/... ./pkg/...` | exit 0（/tmp/int-all-clean2.log） |
| public 全量 | `go test -count=1 ./public/...` | exit 0，66 包全绿（/tmp/pub-all2.log） |
| 分项复核 | `go test ./public/test/navigation/feature` | ok 52.164s |
| 分项复核 | `go test ./public/test/page/unit` | ok 52.4s |
| 分项复核 | `go test ./public/test/presentation/{unit,feature}` | 双双 ok |
| 分项复核 | `go test ./public/test/pkg/i18n` | ok 18.444s |
| 门禁 | `bash scripts/check-all.sh` | 29/30，唯一红见 §5 |

## 5. 遗留

- `scripts/check-write-fields.sh`：`order/order_ship_form.html` 的 `lines` 字段扫描口径与
  `binding.JSON` 不符。**main 基线即红**（40991017 与 019357d1 同样红），不属本批引入，未改。
- 本分支尚未合并回 main；main 工作区有 4 个未跟踪资料（FULL_SYSTEM_AUDIT_2026-10-26.md、
  GO_WP_FULL_SYSTEM_TASKBOOK_2026-10-27.md、internal/templates/zz_tmp_media_probe_test.go、
  public/mockup/workbench-demo/），不纳入本分支。

## 6. 提交

1. `40899d5f` fix: 修复发布管线快照遗留的生产缺陷（事务、锁序、回执身份）
2. `6c9ee3d8` fix: 对齐门禁登记、装配清单与模板出口（含 697 → 697b 迁移补丁位）
3. `db572521` test: 按最终 schema 与真实装配修夹具，钉住新增的一致性判据
