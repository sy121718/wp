# navigation 译文 MVC 下一批实施任务书（单一正文 · 待主审审定，未实施、未冻结）

> 状态：**待主审审定**。本文件是唯一有效的任务书正文；历史裁决只记在
> `.superpowers/sdd/navigation译文MVC下一批规划草案/progress.md`（本轮主审裁定原文见 2026-10-30 R3 轮）。
> 本轮**未跑** build / test / vet / 门禁 / DB / 迁移，**未改源码**，**未派代理**。
> 工作树：`/home/sky/project/go/wp/.worktrees/mvc-publication-seo`（分支 `refactor/mvc-publication-seo`，HEAD `87902f12`；
> 该提交**冻结目录为空** —— 脏工作树中的 T1/R1 产物均为未提交改动，本代理不碰）。
> 行号一律以该工作树**当前源码**为准，本轮逐条亲读。
> 逐条源码取证与「被推翻结论」清单见同目录 `规划核实报告.md`；两份可先行冻结的简报：
> `frozen-T1-pkg-i18n.md`（T1，pkg/i18n 写入器）、`frozen-R1-pkg-i18n-tx-read.md`（R1，事务内读现值 + 工程内容写锁）。

---

## 0. 结论：本批到底要改什么

一句话：**把 `/api/navigation/update` 的译文写从「handler 三段自动提交 + 发布靠租约猜」改成
「输入变更时在同一事务内确立输入代次（`input_version`），发布在持锁窗口内复验代次后才换可见指针」**。

### 0.1 主审本轮裁定（逐条，全部落进正文，不再有「待裁定」行）

| # | 裁定（要点） | 本任务书落实 | 落点 |
|---|---|---|---|
| ① | **废弃 `epoch = cur+1`**，改实体 `input_version`：输入变更同事务原子 +1；编译前捕获版本；激活锁住实体行复验相等；持锁窗口内 FS rename + DB 指针提交；**激活不推进版本**；多语言同批均可成功 | 全文按此重写。判据源 = `pages.input_version` / `presentation_instances.input_version`，由**输入变更事务**推进，激活只**验证** | §2.2、§2.4、§2.6 |
| ② | REV「两段短锁不成立」的**推理错误**要纠正：其最终方案正是「捕获后编译、提交段同锁复验」；正文明确**两个短阶段可以**，第二阶段「复验 → FS → 提交」**不可拆锁** | §2.3 单列一节写明：阶段 1（捕获，无发布锁）与阶段 2（复验+FS+提交，同一持锁窗口）；REV §2 原命题作废，保留其 §3 方案 | §2.3 |
| ③ | **model 禁止 fs 回调**：service 事务边界内调「model 锁读 → publisher 激活 → model 账本与指针」；model 只碰本表 | `ActivatePageLangTx` 拆成 `LockPageForActivationTx`（纯读+锁）+ `WritePageActivationTx`（只写本表）；FS 由 service 在两句之间调用 | §2.4、§3.3 |
| ④ | 实体 `input_version` 原子 +1；**禁止**把 `(project, kind)` 独立任务序列直接赋给实体版本（会倒退）；build job 的 `input_version` 只是**任务序列**，二者语义独立 | §5.3 写死：`build_job_seq.next_version` 归 `build_jobs.input_version`；实体版本用 `input_version = input_version + 1`，永不做 `SET input_version = <seq>` | §2.6、§5.3 |
| ⑤ | build owner **aa68 在写 `698_navigation_menu_stale_sequence.sql`**；新实体迁移用 **699 先查占号**；禁止改 698 / build 代码 | §4.4 迁移改为 **699**，先 `ls public/migrations/699_*` 查占号（当前最大 697）；`698` 与 build 模块**整体列为其他 owner 在途，本批不碰** | §4.4、§4.5 |
| ⑥ | R1 按新简报三接口；**非法/空统一 `Required`**；旧 `Load*` 逐字 | `ErrContentProjectRequired` 同时覆盖空与非法（在途 `ErrContentProjectIDInvalid` 的差异按此裁定收敛）；三接口见 §3.1 | §3.1 |
| ⑦ | FS/DB 失败**不能仅凭版本相等就重放旧回执**：同输入版本可多次激活/不同产物；必须**同实体锁下**验证「当前链接仍等于 own to」且「DB 未被其他成功激活替代」才向前完成，否则结案 superseded 且**不改 FS/DB** | §2.7 重写收敛判定为四步锁内判定（版本 / 链接身份 / DB 归属 / 双命中→superseded） | §2.7 |
| ⑧ | 版本不等也**不能只 rolled_back** 而放任线上仍是 own 陈旧产物；需可靠重建 / pending 闭环 | §2.7.3 二选一（重放前向 or 重建入队），失败保留 pending | §2.7 |
| ⑨ | 优先沿既有回执；**精确列出可用字段**或新增 `receipt_data` 身份；不得空说「DB 未替代」而无执行判断 | §2.7.1 字段表逐列标注是否已存在；新增 `receipt_data` 身份键 `ownArtifactID / fromArtifactID / capturedInputVersion / attemptNo` | §2.7.1 |
| ⑩ | **拒绝无条件 rename 回 `from`**；必要的持久 activation attempt 身份**可纳本域依赖**，不推通用平台 | §2.5 删除「用 from 回滚」；attempt 身份 = `page_publications.artifact_id` + 回执 `receipt_data`，本域自足 | §2.5、§2.7.1 |
| ⑪ | 正常与恢复**同锁**；同实体跨 source 的**实际调用点**；别混 page 与 presentation | §2.8 逐条列真实入口：page 三个（`publish` / `rollback` / `recoverSwitchActiveReceipt`），presentation 两个；两者实体不同、锁不同行 | §2.8 |
| ⑫ | **菜单 CRUD 如何经既有 stale 推进版本要核实** | §2.9：核实结论 = **现行菜单 CRUD 不经队列执行器**（`Fanout` 同步 `RebuildStale`），因此本批**不给菜单 CRUD 加版本推进**，只给「译文写」与「依赖标记」两处推进；menu_stale 执行器归 build owner | §2.9 |
| ⑬ | 任务书正文**无签字选项**、**无「实施时核实」**；已确定的直接给 | §7 决策全删；全部结论写成可执行条目 | §7 |
| ⑭ | 给 BE 明确拆分写域；已有 DTO / FE / ASM / source 各在途，**勿发明冲突接口** | §4.1 变更表按 owner 分列，并标注在途文件与「不得改」清单 | §4.1、§4.5 |

### 0.2 被推翻、本文件不再支持的结论（**不得再引用**）

1. `epoch = cur+1` + `WHERE publish_epoch < epoch` —— 提交序号冒充输入新旧号（REV R1 反例；主审裁定 ①）。
2. 「失败 → 用 P2 读到的 `from` 产物做 FS 回滚」—— 在锁外覆盖另一个成功者（REV R2；主审裁定 ⑩）。
3. 「两次短锁内比较版本不成立」—— REV §2 的推理错误；正确的两段式见 §2.3（主审裁定 ②）。
4. `ActivatePageLangTx(..., fs func() error)` —— model 内持锁做 FS，分层违规、事务跨越 IO（主审裁定 ③）。
5. 「`menu_stale` 把 `build_job_seq` 的号赋给实体 `input_version`」—— 会让实体版本倒退（主审裁定 ④）。
6. 迁移编号 698 —— 已被 build owner 占用（主审裁定 ⑤）。
7. 「四个写入口都有 `ProjectID`」—— 假：`navigation_translations_handle.go:344-352` 构造 `ContentWriteItem` 未赋 `ProjectID`，`content_write.go` 把空工程归一成 `NULL` = 全局行。
8. 「译文写是逐行部分成功」—— 假：`rowErrors` 非空即 400 + return，零写入。
9. 「`max(update_time)+行数` 可做版本键」—— 否（非单调，A→B→A 回旧值）。
10. 「读侧已工程优先」—— 假：工作台读译文走 `LoadTargets`（只看 `project_id IS NULL`）。
11. 「`LeaseToken` 能保护发布副作用」—— 假：`build_worker.go:48-88` 执行期间不校验，只在 `MarkSucceeded/MarkFailed` 走 `leaseGuard`（`build_model.go:197-206`）。
12. 「`pipeline.Publisher.mu` 是跨进程线性化点」—— 假：`p.pages` 是进程内 map，`Build`/`Publish` 两个独立临界段。
13. 「静默跳过 / 部分成功可接受」—— 否（AGENTS.md「写操作」：批量全成全败）。
14. 「page 侧事务外 `update_time`/`stale` 前置跳过可当 fencing」—— 否（主审上一轮 ②）。

---

## 1. 已核实的调用链（本轮亲读，行号为当前值）

### 1.1 译文写链（现状 = 三段自动提交）

| # | 环节 | 位置 | 事实 |
|---|---|---|---|
| 1 | 路由 | `navigation_router.go:19-30` → `navigation_handle.go:69-114` | 一次有界读体 → 判别 → 分支 → `svc.Update` |
| 2 | 译文入口 | `navigation_translations_handle.go:225-268` | 白名单 → project → lang → rows → 行形状 → ≤1000 → 取**包级** `navigationTranslationWriteInst`（nil → 500，`:77-89`） |
| 3 | 写核心 | `navigation_translations_handle.go:285-440` | `projects.List` 线性验工程 → `h.build` 重跑读页面 → 逐行校验 → `LoadDetails`（`:380`，失败只记日志）→ `Upsert`（`:405`）→ `pages.MarkStaleForI18n`（`:417`，失败只记日志）→ `menus.InvalidateMenuLabels`（`:421`） |
| 4 | 三处写 | 同上 | `Upsert` 自开事务；`MarkStaleForI18n` 逐工程自开事务；`InvalidateMenuLabels` → `Fanout.InvalidateKeys`（`dependency.go:248-315`）自开事务 + `go runRebuild()` |
| 5 | 读工作台 | `navigation_page_handle.go:855-960` | `:901` 只遍历 `{"header","footer"}`；`:947` `LoadTargets`（全局行） |

### 1.2 发布链（page / presentation）

- **page `Build`**（`page_publish.go:143-232`）：`syncKernel` → `publisher.Build`（`:184`）→ `ensureArtifactRow`（`:190`，事务外）→ 一个事务（`:205-223`）写依赖 + 计划 + `MarkStagedLangTx`。
  - `MarkStagedLangTx`（`page_tx.go:185-204`）**同时写 `pages.stale=false` 与 `update_time`** → 构建失败也会清 stale。
- **page `publish`**（`:327-536`）：`fromArtifactID := s.publishedArtifactIDOf` → `oldPath := s.publishedPathOf` → `beginPublishReceipt`（`:451`）→ `publisher.Publish`（`:460`，**FS symlink 激活**）→ `applyPublishActivation`（`:1552-1588` 一个事务：`MarkPublishedLangTx` + 计划 + `DeactivateTx` + `ActivateTx`）→ `deactivatePaths`（`:498-504`，事务外 FS）→ `completePublishReceipt`（`:509`）。
  - 失败分支已写死：FS 失败 → `abortPublishReceipt`；DB 事务失败 → `keepPublishReceiptPending`；旧路径下线失败 → `keepPublishReceiptPending`（`:471-494`）。
- **page `recoverSwitchActiveReceipt`**（`:1837-1873`）：`activeArtifactHashAt`（事务外读链接）→ `pageArtifactHashByID` → 相等才 `applyPublishActivation`（**无行锁、无版本比较、读写跨事务**）。
- **presentation `commitPublish`**（`presentation_publish.go:270-341`）：`lockInstance`（进程内 64 分片 mutex）→ `GetInstance` 重读 → `frozen.fingerprint.equal(...)` 不等 → `errPublishStateMoved`（零写入）→ `previousArtifacts := s.publishedArtifactsByLang`（`:296`，事务外读）→ `persistMultiLangArtifacts` → 逐语言 `publishOneLang`（`:299-312`）→ `finalizeMultiLangBatch`（`:316`）。
- **presentation `publishOneLang`**（`presentation_render.go:946-988`）：`beginPublishReceipt` → `s.activate(...)`（FS）→ `registerRoute` → `s.m.MarkPublishedLang`（`:968`，**直接 `m.db` 自动提交**）→ `completePublishReceipt`。
- **presentation `finalizeMultiLangBatch`**（`:996-1008`）：`UpdateInstancePointers`（**单独提交**，`presentation_render.go:1008`）。
- **presentation `recoverOneReceipt`**（`presentation_publish.go:1155-1215`）：同样事务外读链接 + 条件写库，无锁无版本。
- `instanceStateFingerprint`（`presentation_publish.go:335-346`）十项，**刻意不含 `update_time`**，也**不含译文/菜单/revision**。
- `lockInstance`（`presentation_service.go:158`）注释前提「单实例部署」。

### 1.3 队列与租约

- `Claim`（`build_model.go:158-192`）：单条 `UPDATE … RETURNING` 发 `lease_token=gen_random_uuid()`，**autocommit**。
- `claimCandidates`（`:135-143`）：同 `(source_type, source_id)` 的 running 互斥；**不含工程、不含 kind**。
- `execute`（`build_worker.go:48-88`）：执行器前后**都不校验租约**；`MarkSucceeded/MarkFailed` 才走 `leaseGuard`，不符 → `ErrLeaseLost` 只 `Warn`。
- 池：`StartWorkers(context.Background(), 2)`（`assembly_publish.go:402`）；执行器 `page`（`:374-390` → `RunPageBuildJob`）与 `presentation`（`:391-401` → `RebuildInstance`）同池。

### 1.4 依赖扇出与菜单

- `Fanout.InvalidateKeys`（`pipeline/dependency.go:248-315`）：**同步**执行 `MarkStaleByDependency`，然后 `syncRebuild ? runRebuild() : go runRebuild()`。
- `MenuStaleAdapter.InvalidateMenu`（`pipeline/navigation_stale.go:32-38`）只做 `Fanout.InvalidateKeys(ctx, MenuKey(projectID, kind))`；`invalidateMenu`（`navigation_service.go:304-327`）**写后派发、失败只记日志**。
- `markStaleForI18nIn`（`page_model.go:367-378`）与 `markStaleByDependencyIn`（`page_dependency_model.go:133-151`）的 `Updates` 都只写 `{"stale": true, "update_time": at}`。
- `pages` 上**不存在**任何版本列（`grep input_version` 全仓 0 命中）。

### 1.5 既有可复用先例（本批必须复用，不新造口径）

1. `page_lang.go:398-435`：「逐工程 `TransactionScoped`：本模块标 stale + peer 标 stale 同事务」。
2. `product_outbox_model.go:46-52`：`pg_advisory_xact_lock(hashtext(?))` 事务级串行先例（单工程写锁用它）。
3. presentation 的 `errPublishStateMoved` + `publishAttempts=3`（`presentation_render.go:887-939`）：锁内指纹校验 + 零写入 + 重试。
4. 回执 + 收敛（`ConvergePendingReceipts`/`NotifyPendingReceipt`：`page_publish.go:2180/2296`；`presentation_publish.go:720/861`）；`FinishPendingReceipt` 带 `receipt_state = 'pending'` 幂等守卫（`publication_model.go:325-330`）。

---

## 2. 核心设计：输入代次、锁、FS/DB 分界、恢复

### 2.1 事实分层

| 事实 | 位置 | 是否线性化点 |
|---|---|---|
| `pipeline.Publisher.mu` | `publisher.go:528-615` | **否**：进程内 map，只串行本进程；`Build`/`Publish` 两段 |
| `lease_token` | `build_model.go:158-206` | **否**：只保护 job 状态写入 |
| `instanceStateFingerprint` | `presentation_publish.go:335-346` | **是（仅 presentation）**，但不含译文/菜单输入 → 本批由 `input_version` 补齐 |
| `os.Rename` 符号链接替换 | `pipeline/publication.go:124-133`（`Activate`） | **是**：访客看到的就是这个指向 |
| `pages.input_version` / `presentation_instances.input_version` | 迁移 699 新增 | **是（判定源）**：输入代次，只增不减 |
| `page_publications` / `pages.active_artifact_id` / `presentation_instances.active_artifact_id` | `page_tx.go:154-183`、`presentation_model.go:601-614` | **是（DB 侧可见指针）**：受同一把锁保护 |

### 2.2 输入代次（`input_version`）的语义

- 语义只有一个：**「这个实体当前的编译输入代次」**。由**输入变更的那个事务**在同一条 `Updates` 里 `input_version = input_version + 1`。
- **谁推进**（全部同事务原子 +1，禁止在别处 `SET`）：

| 实体 | 推进点 | 现有函数/行 |
|---|---|---|
| `pages` | 草稿保存 | `SaveDraftWithRevisionTx`，`page_tx.go:59-105` 的 `Updates` |
| `pages` | 译文变更扇出 | `markStaleForI18nIn`，`page_model.go:367-378`（`MarkStaleForI18nTx` 与 `MarkStaleForI18n` 共用） |
| `pages` | 依赖源变更 | `markStaleByDependencyIn`，`page_dependency_model.go:133-151`（`MarkStaleByDependency` / `MarkStaleByDependencyTx` 共用） |
| `presentation_instances` | 依赖源 / 模板 / 覆盖文档 / 译文扇出 | presentation 模块内对应的 stale / 模板 / 快照写入（在途 owner 定位，见 §4.6） |

- **谁不推进**：激活（发布）。激活改变的是可见指针，不是编译输入。因此同一批多语言发布时，每种语言读到的 `input_version` 都相同 → **全部可成功**（主审 ① 末句）。
- 不引入内容哈希做代次：A→B→A 时哈希回旧值，与「又一次真实变化」不一一对应。

### 2.3 两段式（纠正 REV §2 的推理错误）

**结论：两个短阶段可以。** 拆解如下：

```text
阶段 1（捕获，无发布锁，可与编译并行）
  C1  读 pages.input_version（普通读）           → captured
  C2  编译产物（Jet 渲染 + 落盘，最慢的一段）
  C3  ensureArtifactRow：建不可变产物行           → ownArtifactID（内容寻址，无需锁）
  C4  beginPublishReceipt：写 receipt_data 身份   → receiptID
      （ownArtifactID / fromArtifactID / capturedInputVersion / attemptNo）

阶段 2（提交，一个持锁窗口内完成，三步不可拆）
  P1  BEGIN（project 作用域）
  P2  SELECT … FOR UPDATE 锁实体行，复验 input_version == captured
        不等 → ROLLBACK，零写入，activated=false
  P3  FS: Activate（symlink + rename）          ← 仍持锁
  P4  DB: 语言账本 upsert + 可见指针 + stale=false + 计划/路由（同一事务）
  P5  COMMIT                                    ← 锁到此才释放
```

- REV §2 的原命题「两段短锁内比较版本不成立」**作废**：它把「两个事务各锁一次」当成了唯一读法。
  正确读法是「阶段 1 不持锁、不做判定；阶段 2 只有**一次**加锁，且**复验 → FS → 提交**在同一窗口内」。
- **不可拆的部分只有一个**：P2 → P3 → P4。把「复验」和「FS」分开、把「FS」和「提交」分开，才会重新出现 TOCTOU；
  这正是当前 `publish` 的缺陷（FS 在 `:460`、DB 在 `:481`、锁根本不存在）。
- 阶段 2 持锁时长 = 一次 `os.Rename`（`pipeline/publication.go:126`）+ 一次事务提交，**与编译时长无关**。
- 进程内 mutex 只能作性能优化（避免同进程两个事务互等行锁），正确性只由 P2 的行锁给出（§2.10）。

### 2.4 精确接口（model 只做本表，无 fs 回调）

**page/model（`internal/module/page/model/page_tx.go`）**

```go
// LockPageForActivationTx 在调用方事务内锁住页面行并读出激活判定所需的全部事实。
//
// 这是本域唯一的发布判定入口：行锁由本句持有到事务结束，调用方必须在**同一个 tx**
// 上完成 FS 切换与指针写入，否则锁定无效。
// 纯读：不发 UPDATE，不写任何表（model 只碰本模块表）。
func (m *Model) LockPageForActivationTx(ctx context.Context, tx *gorm.DB,
    projectID, pageID string) (snap PageActivationSnapshot, err error)

// PageActivationSnapshot 激活判定的只读事实。
type PageActivationSnapshot struct {
    PageID           string
    InputVersion     int64  // pages.input_version（本事务内锁定读）
    ActiveArtifactID string // pages.active_artifact_id（单值镜像，最近发布语言）
}

// WritePageActivationTx 在调用方事务内写本语言的激活状态与页面镜像。
//
// 前提：同一事务已先调用 LockPageForActivationTx 并校验通过；本方法不重复判定，
// 只写 page_publications（upsert）+ pages（active_* / published_at / stale=false）。
// 返回 false = 谓词未命中（调用方必须回滚并视为激活失败）。
func (m *Model) WritePageActivationTx(ctx context.Context, tx *gorm.DB, projectID string,
    rec PublicationRecord, expectInputVersion int64) (written bool, err error)

// WritePageForwardCompletionTx 收敛「向前完成」专用：仅在 DB 侧尚未被其他成功激活替代时写入。
//
// 与 WritePageActivationTx 的差别只有一条谓词：额外要求
//   (page_publications.artifact_id IS NULL OR page_publications.artifact_id <> $other)
// 由调用方传入它已判定的「其他成功激活者」集合（通常为空集，即只要求版本相等）。
func (m *Model) WritePageForwardCompletionTx(ctx context.Context, tx *gorm.DB, projectID string,
    rec PublicationRecord, expectInputVersion int64) (written bool, err error)
```

- `WritePageActivationTx` 内部谓词（GORM `Where`，**不新增裸 SQL**）：

```sql
UPDATE pages
   SET active_artifact_id = $artifactID, active_path = $path, published_at = $at,
       stale = false, update_time = $at
 WHERE id = $pageID AND project_id = $project AND deleted_at IS NULL
   AND input_version = $expectInputVersion
```

  0 行 → `written=false`（本次输入已过期）→ 调用方 `ROLLBACK`。
  `input_version` **不被本语句修改**（主审 ① 末句：激活不推进版本）。
- 事务内的真实顺序由 service 编排（主审 ③）：**service 开事务 → model 锁读 → publisher 激活（FS）→ model 写账本与指针 → COMMIT**。
  model 之间只相隔一次外部调用，但锁是 P2 的 `FOR UPDATE` 持有的，跨越 FS 调用仍然有效（同一 tx、同一连接）。
- `markStaleForI18nIn` / `markStaleByDependencyIn` / `SaveDraftWithRevisionTx` 的 `Updates` map 追加一项
  `"input_version": gorm.Expr("input_version + 1")`（原子 +1，**不读改写**）。

**page/service（`page_publish.go`）** —— `publish` 改为：

```text
P0  编译前：page.InputVersion 已在 plan/stagedArt 上捕获（§2.4.1）
P1  beginPublishReceipt（事务外；receipt_data 写 ownArtifactID / fromArtifactID /
      capturedInputVersion / attemptNo）
P2  TransactionScoped：
      snap := model.LockPageForActivationTx(tx, projectID, page.ID)
      snap.InputVersion != captured → 直接 ROLLBACK（不调 FS），ErrInputMoved
      err := s.publisher.ActivateStagedArtifact(page.ID)      ← FS，仍在锁内
      written := model.WritePageActivationTx(tx, projectID, rec, captured)
      !written → ROLLBACK，ErrInputMoved
      其余原 applyPublishActivation 的 DB 步骤（计划 / DeactivateTx / ActivateTx）照旧在本事务
P3  COMMIT（锁释放）
P4  deactivatePaths（事务外，可重放）
P5  completePublishReceipt
失败：P2 内 FS 已成功后任何失败 → 不补偿 FS，保留 pending（§2.5）
```

- 这会**消掉 `s.publisher.Publish` 的单一调用点语义**：`Publisher.Publish` 保留为「`StageArtifact` + `ActivateStagedArtifact` 的组合」以兼容 `Rollback`/`publishAllLangs` 之外的既有调用点；本批 `publish` 改用拆分后的两步（§3.3）。
- 由于 `publisher.ActivateStagedArtifact` 只做 `p.pub.Activate` + 进程内状态更新（`publisher.go:581-615` 的后半段），它**不读写数据库**，放在事务内安全（但仍是 IO，持锁时长 = 一次 rename）。

#### 2.4.1 编译前捕获点（精确到字段与行）

| 实体 | 捕获位置 | 字段 |
|---|---|---|
| page·人工发布 | `publish` 开头 `getExistingPage` 之后（`:331-334`） | `page.InputVersion`（`PageEntity` 新增列） |
| page·队列重建 | `planPageRebuild`（`:2510-2546`） | `pageRebuildPlan.InputVersion`（新字段，取自 `plan.Page.InputVersion`） |
| page·构建 | `Build`（`:143-232`）写 `page_stagings` 时 | 暂存行**不需要**存版本：发布时重新捕获即最新（见 §2.6 不变式） |
| presentation | `freezePublish`（`:181-240`） | `publishFreeze.inputVersion = fresh.InputVersion`（`*inst = *fresh` 之后取） |

**不变式**：捕获必须**晚于**该次输入变更（否则会捕获到旧值而把新输入误判为可激活）。page 侧 `getExistingPage` 是发布起点，
presentation 侧 `freezePublish` 在锁内重读，二者都满足。

**残余风险（明写，不掩盖）**：编译期间若输入再变，`input_version` 已 +1，P2 复验失败 → 本次失败、保留 stale，由下一轮重建收敛。
这是**安全方向**（宁可失败不贴陈旧）；`RunPageBuildJob` 现有语义「重建永远以当前草稿为准」只在**同一代次**内成立。

### 2.5 FS 与 DB 的分界（否定补偿）

```text
P2  复验（不产生外部副作用）
P3  FS Activate —— 唯一外部副作用，在锁内
P4  DB 写（同一事务）
P5  COMMIT
失败：P3 之后任何失败 → **不做 FS 回滚**，回执保持 pending
```

- **删除「用 `from` 产物 rename 回去」**（主审 ⑩）。理由（源码级）：`from` 永远比并发成功者旧；
  任何「失败就换回 from」的补偿在锁释放后执行，必然覆盖别人刚成功的产物（REV R2）。
- 不补偿的方向是安全的：线上此时是**本次刚 rename 的产物**，DB 落后；由收敛按 §2.7 判定是否向前完成。
- 即使本次产物此后被更新的发布覆盖，也只是「线上 = 更新产物」，不会倒退。

### 2.6 `input_version` vs build job 序列（主审 ④）

| 项 | 语义 | 存储 | 谁写 |
|---|---|---|---|
| `pages.input_version` / `presentation_instances.input_version` | **实体编译输入代次**，单调 +1，只增不减 | 实体行 | 输入变更事务（§2.2） |
| `build_jobs.input_version` | **任务序列号**（同一 `(context_key)` 下严格更大） | 任务行 | 入队事务，取自 `build_job_seq.next_version` |

- **禁止**把 `build_job_seq` 的号赋给实体列。反例：实体 `input_version` 已被输入变更推进到 7，而 `build_job_seq`
  因任务表被 retention 清理后重新从 1 开始 → `SET input_version = 1` 使实体版本倒退，随后旧产物（捕获版本 5）反而通过复验。
- 二者的关联只有一处：入队事务**同时**推进受影响实体的 `input_version`（§2.2 表格中的推进点）并发号；两个号**不互相赋值**。
- **不做**「任务号也冻结进激活判定」：任务号会被回收/去重/合并，不满足单调性；判定只用实体版本。

### 2.7 收敛：同锁、四步判定、不重放旧回执

#### 2.7.1 回执可用字段（逐列，含是否已存在）

| 需要的身份 | 现状 | 结论 |
|---|---|---|
| 回执主键 | `publication_receipts.id`（`ReceiptEntity.ID` int64） | 已存在 |
| 归属 | `source_type` + `source_id`（`ReceiptEntity.SourceType/SourceID`） | 已存在 |
| 路径 / 语言 / 动作 | `path` + `receipt_data.lang` + `action` | 已存在（`publishReceiptData`，`publication_receipt.go:56-64`） |
| 目标产物 | `to_artifact_id`（`ReceiptEntity.ToArtifact`） | 已存在 |
| 原产物 | `from_artifact_id` | 已存在，但**不用于回滚**（仅审计） |
| 旧路径 | `receipt_data.oldPath` | 已存在 |
| **own 产物身份** | 无独立保证（`to_artifact_id` 可为空，见 `update_url` 例外） | **新增** `receipt_data.ownArtifactID`（switch_active 恒非空） |
| **捕获的输入版本** | 不存在 | **新增** `receipt_data.capturedInputVersion` |
| **attempt 身份** | 不存在 | **新增** `receipt_data.attemptNo`（进程内单调 + 登记时间；用于区分同一输入版本的多次激活尝试） |
| 端点 hash | 事务外按 `to_artifact_id` 反查 | 保留，仅作交叉校验 |

- `receipt_data` 已是 `jsonb`（`ReceiptEntity.ReceiptData`，`publication_model.go:59`），**不加列**、不改表结构；
  写入侧是 `publishReceiptData`（`publication_receipt.go:56-64`），加字段即可，`BeginPublishReceipt` 的 DTO 无需改（走 `OldPath/Redirect` 同一条路的地方新增可选字段）。
- `PendingReceiptResp` 已透出 `FromArtifactID/ToArtifactID/Path/Lang/OldPath/Redirect/CreatedAt`（`publication_resp.go:49-64`）；
  **需新增** `ReceiptData` 原文（或 `OwnArtifactID / CapturedInputVersion / AttemptNo` 三个直出字段），否则收敛侧读不到身份。

#### 2.7.2 判定（同一事务、同一把实体行锁上顺序执行）

```text
G1  锁：model.LockPageForActivationTx(tx, projectID, pageID)   ← 与正常发布同一把锁
G2  版本：snap.InputVersion == rc.CapturedInputVersion ?
      否 → 进 G4（**不**直接 rolled_back，也不改 FS/DB）
G3  链接身份：s.activeArtifactHashAt(path) 必须 == ownArtifactID 的 hash
      否 → 结案 superseded（receipt_state='superseded'），**不改 FS、不改 DB**
      是 → 进 G4
G4  DB 归属：读 page_publications(page_id, lang)
      - artifact_id == ownArtifactID            → DB 已是本次结果 → completePublishReceipt
      - artifact_id == NULL 或 <> ownArtifactID
          · 版本相等（G2 通过）                 → WritePageForwardCompletionTx → 向前完成 → complete
          · 版本不等（G2 失败）                 → 双命中风险：链接已是 own，但 DB 属于别人或被别人替代
                 - DB.artifact_id <> own 且非空  → superseded（DB 已有更新的成功者）不改 FS/DB
                 - DB.artifact_id 为空 或 仍是从前那个 from
                                    → **重建闭环**：入队 page 重建（§2.7.3），回执保持 pending
```

- 「仅版本相等」的理由（主审 ⑦）：**同一输入版本可以被多次激活不同产物**（例如同版本下重试产出另一份字节，
  或两次 rename 交错）。版本相等只说明输入没变，不说明「线上/DB 该是谁」。所以 G3 + G4 的身份比较**必须存在**，
  且与 G1 的锁在同一事务里完成。
- **superseded** 是新增终态（`publication_receipts.receipt_state` 增加一个枚举值，在 publication 模块 enums 声明；
  迁移只加 CHECK，不改列）。它表达「本次尝试被更权威的结果取代」，与 `rolled_back`（没生效）区分。
- 「不改 FS / 不改 DB」在 superseded 分支是**硬约束**：不调用任何 `Activate`、不写 `page_publications` / `pages`。

#### 2.7.3 版本不等且线上仍是 own 陈旧产物的闭环（主审 ⑧）

- 场景：G2 版本不等（输入已推进），G3 链接仍是 own 产物。此时线上是**陈旧字节**，DB 未被替代。
- 处置：**在 G1 的同一事务内**做两件事之一：
  1. **向前完成**：执行 `WritePageForwardCompletionTx` 把 DB 补成 own（DB 落后于 FS，向前补齐是安全方向），
     然后**立即入队一次 page 重建**（`EnqueuePageBuild(ctx, pageID, projectID, lang, "", 0, "")`）以把陈旧字节换成新输入产物；
     重建走正常发布链（含 §2.4 的锁与复验），不引入第二套机制。
  2. **不做向前完成**：直接入队重建，回执保持 pending，下一轮收敛时 G2 仍不等 → 重建完成后再由新回执结案。
- **禁止**只标 `rolled_back` 就收场：那会让「线上陈旧、DB 落后、无任何待办」永久静默。
- 重建入队失败 → 回执保持 pending（不改 FS/DB），下一轮重试入队；这不是「无限重试」，因为重建一旦成功，
  新发布的回执会正常结案，本回执下一轮 G3 会发现链接已是新产物 → superseded。

### 2.8 同实体跨 source 的真实调用点（主审 ⑪）

**先纠正一个错误前提**：`pages` 与 `presentation_instances` 是**两类不同实体**，各自的执行器也只操作自己的实体
（`assembly_publish.go:374-401`：`page` → `RunPageBuildJob`，`presentation` → `RebuildInstance`）。
「同一实例被 page 与 presentation 两个模块重建」**在源码里不存在**，任务书旧稿 §2.4 第 169 行据此断言「行锁使两者串行」是错的。

真实需要「同锁」的是**同一实体、多条入口**：

| 实体 | 入口 | 现状 | 本批 |
|---|---|---|---|
| `pages` | 人工 `publish`（`page_publish.go:327`） | 无锁，FS 在 DB 之前 | 改为 §2.4 顺序 |
| `pages` | 依赖/队列 `RunPageBuildJob`（`:2600`）→ `rebuildPage`（`:2556-2585`）→ `publish` | 同一条 `publish` | 无需额外改动（天然同锁） |
| `pages` | `Rollback`（`publisher.Rollback` `pipeline/publisher.go:619-649`，回执恢复 `recoverRollbackReceipt` `:1913`） | FS 激活 + `applyRollback` | **必须一并走 §2.4 锁**：`recoverRollbackReceipt` 改为「锁 → 复验 → FS → 写」 |
| `pages` | `recoverSwitchActiveReceipt`（`:1837-1873`） | 无锁、事务外读链接 | 改为 §2.7 |
| `presentation_instances` | `commitPublish`（`presentation_publish.go:270`） | 进程内 `lockInstance` + 指纹 | 保留进程锁，增加 `input_version` 复验（§4.6） |
| `presentation_instances` | `recoverOneReceipt`（`:1155-1215`） | 事务外读链接 + 条件写 | 改为同形态锁内判定（§4.6） |

- `menu_stale`（build owner 在途）若将来由本域消费，**唯一允许的落点**是既有入口：page → `RunPageBuildJob`；
  presentation → `RebuildInstance`。不允许旁路写指针或另写 FS。

### 2.9 菜单 CRUD 与既有 stale 的版本推进（主审 ⑫）

核实结论（本轮亲读）：

1. 现行菜单 CRUD（`navigation_service.go` 的 Create/Update/Delete）**不经过任何队列执行器**：
   它在写成功后直接 `invalidateMenu` → `MenuStaleAdapter.InvalidateMenu` → `Fanout.InvalidateKeys` →
   `MarkStaleByDependency`（同步标 stale）→ `go runRebuild()`（`dependency.go:298-312`）。
2. 因此**菜单 CRUD 的「版本推进」= 依赖标记时那一次 `input_version + 1`**（`markStaleByDependencyIn`，§2.2 第三行）。
   这是本批唯一给菜单路径加的版本点，**不需要**在 navigation 的 CRUD 里再加一次。
3. `build_job_seq` / `menu_stale` 任务形态属 build owner 在途（`698_navigation_menu_stale_sequence.sql` + `build_job_seq.go`）。
   本批**不消费、不注册执行器、不改 build 代码**；其任务号与实体版本的关系按 §2.6 严格分离。
4. 现状缺陷（本批**修**）：`invalidateMenu` 失败只记日志，菜单已经改了、页面却不重建且无任何可见错误 →
   在 §4.2.4 的事务内改为「同事务标记 + 可靠入队」后，失败会随事务回滚并返回给用户。

### 2.10 进程锁（部署前提）

- `lockInstance` 与本批 page 侧**不新增**进程锁。
- 部署前提写死：进程内锁只在单实例部署提供互斥；**正确性只由 §2.4 的行锁 + §2.4/§2.7 的版本复验保证**。
- 任何将来新增的进程内锁内部**必须重新读 DB 并重做判定**（现状 `commitPublish` 已满足：锁内 `GetInstance` 重读）。

---

## 3. 精确接口清单（最小集）

### 3.1 pkg/i18n（owner E，按新简报三接口）

```go
// 既有，一行不改
func (w *ContentWriter) LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
func (w *ContentWriter) LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
func (w *ContentWriter) LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]ContentTargetInfo, error)
func (w *ContentWriter) LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]ContentTargetInfo, error)
func (w *ContentWriter) Upsert(ctx context.Context, items []ContentWriteItem) (written int, err error)

// 新增 1：事务内读译文文本
func (w *ContentWriter) LoadTargetsForProjectTx(ctx context.Context, tx *gorm.DB,
    projectID, lang string, hashes []string) (map[string]string, error)

// 新增 2：事务内读译文明细
func (w *ContentWriter) LoadDetailsForProjectTx(ctx context.Context, tx *gorm.DB,
    projectID, lang string, hashes []string) (map[string]ContentTargetInfo, error)

// 新增 3：工程内容写锁
func LockProjectContentWrite(ctx context.Context, tx *gorm.DB, projectID string) error

// 唯一 sentinel：空工程与非法工程**统一**归到它（主审 ⑥）
var ErrContentProjectRequired = errors.New("译文写入锁必须指定工程")
```

- 在途实现目前对非法 UUID 报 `ErrContentProjectIDInvalid` —— 按主审裁定收敛为上表（空 / `"   "` / `not-a-uuid` / 多余字符 → 同一个 `ErrContentProjectRequired`）。
- 锁键在 **pkg 内**归一：`uuid.Parse(projectID).String()`；键 = `"i18n.content:" + 规范UUID`，`pg_advisory_xact_lock(hashtextextended(?, 0))`。
- `requireTx` 先行（`content_write.go:627`）：非事务句柄 → `ErrContentTxRequired`，零 SQL。
- 旧 `Load*` 签名 / SQL / 归一核心**逐字不动**。

### 3.2 navigation（owner A/B）

```go
// dto（新文件 navigation_translation_req.go / navigation_update_envelope.go）
type TranslationUpdateReq struct {
    Project string
    Lang    string
    Rows    []TranslationWriteRow
}
type TranslationWriteRow struct { Context, Hash, Target string }

// contract（新文件 navigation_translation.go）
type PageStalePort interface {
    MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)
}
type PresentationStalePort interface {
    MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)
}
type MenuStaleEnqueuePort interface {
    EnqueueMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string) (inputVersion int64, err error)
}
type TranslationWriteResult struct {
    Written      int
    ChangedKinds []string
}
type TranslationWriteService interface {
    WriteTranslations(ctx context.Context, req *navigationdto.TranslationUpdateReq) (*TranslationWriteResult, error)
}

// service（新文件 navigation_translation_write.go / navigation_source.go）
func (s *Service) WriteTranslations(ctx context.Context, req *navigationdto.TranslationUpdateReq) (*navigationcontract.TranslationWriteResult, error)
func (s *Service) OpenSourceIndex(ctx context.Context, projectID string) (SourceIndex, error)
```

- **三个端口各一方法**：page 与 presentation 的既有签名不同（`page_tx.go:231` 返回 `([]string, error)`，presentation 无 Tx 变体），
  **不得**合成「一个两方法 interface」。
- `n` 只作日志 / 测试断言，navigation service 不用它做业务判断。
- `EnqueueMenuStaleTx` 的 `inputVersion` 是**任务序列号**（§2.6），navigation 只透传给 build，**不得**写进任何实体列。

### 3.3 page（owner F）

```go
// page/model（page_tx.go）
type PageActivationSnapshot struct {
    PageID           string
    InputVersion     int64
    ActiveArtifactID string
}
func (m *Model) LockPageForActivationTx(ctx context.Context, tx *gorm.DB, projectID, pageID string) (PageActivationSnapshot, error)
func (m *Model) WritePageActivationTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord, expectInputVersion int64) (bool, error)
func (m *Model) WritePageForwardCompletionTx(ctx context.Context, tx *gorm.DB, projectID string, rec PublicationRecord, expectInputVersion int64) (bool, error)

// page/contract（page_service.go 的接口扩展）
func (s *Service) MarkMenuStaleTx(ctx context.Context, tx *gorm.DB, projectID, kind string, at time.Time) (n int64, err error)

// pipeline/publisher.go（拆分，不改内核语义）
func (p *Publisher) StageArtifact(pageID string) (hash string, err error)      // 校验 StagedHash + 路径一致性，不动 FS
func (p *Publisher) ActivateStagedArtifact(pageID string) error                // 只做 p.pub.Activate + supersedeActiveLocked
func (p *Publisher) Publish(pageID string) (hash string, err error)            // 保留 = Stage + Activate
```

- `MarkPublishedLangTx`（`page_tx.go:154-183`）**不改**（仍写 `stale=false`，因为它现在只被成功激活调用）；
  `MarkStagedLangTx`（`:185-204`）**删除 `"stale": false`**（构建不等于发布）。
- `PageEntity` 新增 `InputVersion int64 `gorm:"column:input_version"``（不声明列型，**依赖迁移 699 的 DDL** ——
  `model_gorm_tag_test.go` 只禁列型声明，不禁列名）。
- `pageRebuildPlan` 新增 `InputVersion int64`。

### 3.4 唯一真实流转（owner A + B）

```text
T1  projects.Exists(project)                          → 不存在 = 400
T2  lang ∈ i18n.AvailableLangs()                      → 否则 400
T3  canonical := uuid.Parse(project).String()         （归一在小写规范形态）
T4  过滤 TrimSpace(target)==""（不查来源、不计数）；kept 为空 → 200 {"written":0}，**不开事务、不派发**
T5  BEGIN（navigation model.Transaction）
T6    rls.ScopeTx(tx, canonical)
T7    i18n.LockProjectContentWrite(ctx, tx, canonical)
T8    OpenSourceIndex（锁内，未知 hash → 400）
T9    ShouldTranslateContent 判定（不可翻译 → 409）
T10   i18n.LoadDetailsForProjectTx（锁内读现值，工程优先全局回落）
T11   比对：同值且 engine=manual → 该行不改动，不计 written
T12   UpsertTx（ProjectID=canonical，单工程）
T13   逐 kind：PageStalePort.MarkMenuStaleTx → PresentationStalePort.MarkInstanceMenuStaleTx →
         MenuStaleEnqueuePort.EnqueueMenuStaleTx
T14 COMMIT
T15 返回 {Written, ChangedKinds}（回执文案由 pkg/response 取词）
```

- `T13` 的三步在**同一事务**内完成：这是「译文 / stale / 可靠入队同事务」的落点。
  `MarkMenuStaleTx` / `MarkInstanceMenuStaleTx` 内部各自带 `input_version + 1`（§2.2），
  `EnqueueMenuStaleTx` 取任务号（§2.6）。
- **禁止**在事务内调 `Fanout.InvalidateKeys` / `InvalidateMenuLabels`（自开作用域 + `go runRebuild()`）；
  重建/派发在 `COMMIT` 之后由调用方（handler）触发一次，且失败**不回滚已提交的译文**（只记错误并返回 5xx 级的
  「已保存但重建未派发」事实，见 §4.2.5）。

### 3.5 校验顺序表（service，写死，与 §3.4 一致）

| 步 | 判定 | 结果 | 状态码 |
|---|---|---|---|
| 1 | 行形状（三键必在、无多余键、非 null；`target` 可空串） | 整批拒绝 | 400 |
| 2 | `TrimSpace(target)==""` | 忽略本行，不查来源、不计数 | — |
| 2b | 忽略后 empty | `200 {"written":0}`，零派发、**不开事务** | 200 |
| 3 | `Context != i18n.ContentContext("navigation","label")` | 整批拒绝 | 400 |
| 4 | 来源未知（hash 不在当前菜单树） | 整批拒绝，明细含完整 64 位 hash | 400 |
| 5 | `!ShouldTranslateContent` | 整批拒绝 | 409 |
| 6 | 锁内现值相同且 `engine=manual` | 该行不改动，不计 written | — |
| 7 | 事务：`UpsertTx` → 三端口 | 全成或全败 | 2xx / 5xx |

---

## 4. 任务与文件归属（按 owner 拆写域）

### 4.1 变更表

| # | 文件 | owner | 变更 | 依据 |
|---|---|---|---|---|
| 1 | `pkg/i18n/content_write.go` | E | 收敛 `ErrContentProjectRequired`（非法/空统一）；确认三接口 | 主审 ⑥ |
| 2 | `internal/module/navigation/dto/navigation_translation_req.go`（新） | A | 请求 DTO | 分层 |
| 3 | `internal/module/navigation/dto/navigation_update_envelope.go`（新） | A | `Route()` / `DecodeTranslation()` | 唯一判别入口 |
| 4 | `internal/module/navigation/contract/navigation_translation.go`（新） | A | 三端口 + 结果 | 跨模块只走 contract |
| 5 | `internal/module/navigation/service/navigation_translation_write.go`（新） | A | §3.4 流转 | 事务边界在 service |
| 6 | `internal/module/navigation/service/navigation_source.go`（新） | A | `SourceIndex` / `OpenSourceIndex` | 来源定位唯一实现 |
| 7 | `internal/module/navigation/model/navigation_model.go` | A | `+Transaction`（形同 `page_tx.go:38-45`） | 事务句柄下传 |
| 8 | `internal/module/navigation/inbound/http/navigation_translations_handle.go` | B | 删 `writeTranslations` 与包级 `navigationTranslationWriteInst`（`:77-89`） | 包级可变指针 |
| 9 | `internal/module/navigation/inbound/http/navigation_handle.go` | B | `Update` 改按 `Route()` 分派 | 判别下沉 dto |
| 10 | `internal/module/navigation/inbound/http/navigation_page_handle.go` | C | `build` 换 `LoadTargetsForProject` + `OpenSourceIndex`；行加 `Untranslatable` | 读侧工程优先 |
| 11 | `internal/templates/admin/navigation/navigation_translations.html` | C | 不可翻译行改只读说明 | 空 target 语义 |
| 12 | `internal/templates/static/js/ui/write-base.js`（+ `.test.cjs`） | C | `itemsOf` 支持 `data-write-items` | 失败明细 opt-in |
| 13 | `scripts/check-write-attributes.sh` | C | `ALLOWED_SHAPING` 加 `[data-write-items]` | 门禁 |
| 14 | `internal/routers/nav_stale_adapter.go`（新） | B | 三端口适配器 + `var _` 断言 + `pipeline.MenuKey` 组装 | navigation 不 import pipeline |
| 15 | `internal/routers/assembly.go` / `wiring.go` | B | 注入 + 端口自检（**不注册 `menu_stale` 执行器**） | 装配唯一入口 |
| 16 | `internal/module/page/model/page_tx.go` | F | 新增 `LockPageForActivationTx` / `WritePageActivationTx` / `WritePageForwardCompletionTx`；删 `MarkStagedLangTx` 的 `stale=false`（`:200-203`）；`SaveDraftWithRevisionTx` 加 `input_version + 1` | §2.2、§2.4 |
| 17 | `internal/module/page/model/page_model.go` / `page_dependency_model.go` | F | `markStaleForI18nIn`（`:367-378`）与 `markStaleByDependencyIn`（`:133-151`）的 `Updates` 加 `input_version + 1` | §2.2 |
| 18 | `internal/module/page/model/page_model.go`（`PageEntity`） | F | 新增 `InputVersion int64`（新列名，不声明列型） | §3.3 |
| 19 | `internal/module/page/service/page_publish.go` | F | `publish` 改 §2.4 顺序；`recoverSwitchActiveReceipt` 改 §2.7；`recoverRollbackReceipt` 走同锁；`pageRebuildPlan.InputVersion` | §2.4、§2.7、§2.8 |
| 20 | `internal/module/page/service/page_dependency.go` / `contract/page_service.go` | F | `+MarkMenuStaleTx`；`enqueueOverflowBuildJobs` 返回值不再丢弃 | 错误可见 |
| 21 | `internal/pipeline/publisher.go` | F | `+StageArtifact` / `+ActivateStagedArtifact`（拆 `:581-615`），`Publish` 保留组合 | §3.3 |
| 22 | `internal/module/presentation/model/presentation_model.go` | F | `UpdateInstancePointersTx`（`:601-614`）加 `input_version = $expect` 谓词；`InstanceEntity` 新增 `InputVersion` | §4.6 |
| 23 | `internal/module/presentation/service/presentation_publish.go` | F | `freezePublish` 捕获 `inst.InputVersion`；`commitPublish` 用它做提交谓词；`recoverOneReceipt` 同形态判定 | §4.6 |
| 24 | `internal/module/presentation/service/presentation_render.go` | F | `publishOneLang` 的 `MarkPublishedLang`（`:968`）→ `MarkPublishedLangTx`；`finalizeMultiLangBatch` 与账本同事务 | §4.6 |
| 25 | `internal/module/publication/service/publication_receipt.go` + `dto/publication_req.go` / `resp.go` | F | `publishReceiptData` 增 `ownArtifactID / capturedInputVersion / attemptNo`；`PendingReceiptResp` 透出；`superseded` 终态 | §2.7.1 |
| 26 | `public/migrations/699_navigation_input_version.sql` + `register_navigation_input_version.go` | G | 见 §4.4（**编号先查占号**） | 主审 ⑤ |
| 27 | `public/test/navigation/feature/*`、`public/test/pkg/i18n/*`、`public/test/page/feature/*` | D | 用例源码（§6.1） | 验收 |

**明确不做**：不动 `sys_menus`、不动 `Fanout` 行为、不动 SEO G6、不做全系统发布重构、不重写三个旧 `Load*`、
不改 `/api/navigation/update` 路径与权限点（`036_blueprint_navigation_permissions.sql:15`：`navigation:update`）。

### 4.2 分层归属

| 改动 | 层 | 允许 | 禁止 |
|---|---|---|---|
| DTO / 判别 | navigation/dto | 绑定与形状 | 展示字段、gin.H、import service/model |
| 编排 / 事务 / 锁 | navigation/service | 事务边界、事实 | 自造词条（`admin.` / `shell.` 等 key 字面量）、拼成品句 |
| 端口 | navigation/contract | 接口 + 不可变 DTO | import 他人 service/model |
| JSON 出口 | navigation/inbound/http | binding → service → return | 第二段业务逻辑、取词 |
| 激活判定 | page/presentation model | 本模块表、行锁、状态写入 | 业务规则、跨聚合事务、**FS 回调** |
| FS 切换 | page/presentation service（+ pipeline 只读拆分） | 调 publisher 的激活方法 | model 内做 IO |
| 队列能力 | build | 队列语义（**不在本批**） | 模块业务规则 |
| 迁移 | public/migrations | 幂等 SQL + register | Go 业务逻辑 |

### 4.3 事务内的三处写（译文 / stale / 入队）

```text
T5-T14 同一事务：
  UpsertTx（sys_translation）
  → MarkMenuStaleTx（pages：stale=true AND input_version=input_version+1）
  → MarkInstanceMenuStaleTx（presentation_instances：同上）
  → EnqueueMenuStaleTx（build_jobs：context_key + input_version=任务号）
COMMIT 之后（handler，事务外）：
  → navigation.InvalidateMenuLabels（既有 Fanout 派发 + go runRebuild，失败只记日志）
```

- 事务内**不**调 `Fanout`。派发失败不影响已提交的译文（与既有口径一致），但**必须**写一条 error 级日志。
- 若三端口之一失败 → 整个事务回滚：译文零写入、stale 不变、无任务 → HTTP 5xx（batch 全成全败）。

### 4.4 迁移 699（owner G，**编号先查占号**）

**前置动作（硬性）**：`ls public/migrations/699_*`；若已被占用（build owner 或他人），顺延到下一个空号并在
`register_*.go` 的 `Version` 中一致。当前实测最大为 `697_seo_audit_i18n.sql`，`register_seo_audit_i18n.go:11` 为 `"697-seo-audit-i18n"`；
`698` 归 build owner（在途）。

```sql
-- public/migrations/699_navigation_input_version.sql
BEGIN;
-- 实体输入代次：输入变更同事务 +1；激活只验证不推进（主审裁定 ①）
ALTER TABLE pages ADD COLUMN IF NOT EXISTS input_version bigint NOT NULL DEFAULT 0;
ALTER TABLE presentation_instances ADD COLUMN IF NOT EXISTS input_version bigint NOT NULL DEFAULT 0;

-- 回执新增终态 superseded（只放宽 CHECK，不改列型）
ALTER TABLE publication_receipts DROP CONSTRAINT IF EXISTS publication_receipts_receipt_state_check;
ALTER TABLE publication_receipts ADD CONSTRAINT publication_receipts_receipt_state_check
  CHECK (receipt_state IN ('pending','committed','rolled_back','superseded'));
COMMIT;
```

- **不含** `build_job_seq`、`build_jobs.input_version/context_key` 与 `source_type` CHECK —— 那些属 698（build owner）。
- `ADD COLUMN … NOT NULL DEFAULT 0` 在 PG 11+ 不重写表；`ADD CONSTRAINT CHECK` 需全表校验（publication_receipts 规模小，可接受）。
- 注册：`register_navigation_input_version.go`，`CheckSQL` 必须**同时**断言 `pages.input_version` 与
  `presentation_instances.input_version` 存在（单一表名判据会因表已存在而永久跳过，`migrator.go:248`）。
- 迁移改了结构 → 用 `scripts/schema-snapshot.sh` 重生成 `public/schema/CURRENT.sql`（`public/CLAUDE.md`）。

### 4.5 写域拆分与在途冲突（主审 ⑭）

| 域 | owner | 可写文件 | **不得触碰** |
|---|---|---|---|
| R1 | E | `pkg/i18n/content_write.go` | T1 已交付符号与 `public/test/pkg/i18n/content_write_tx_test.go` |
| navigation | A / B / C | §4.1 第 2-15 行 | page / presentation / build 任何文件 |
| page+presentation+pipeline | F | §4.1 第 16-25 行 | build 模块、迁移、navigation |
| 迁移 | G | `public/migrations/699_*` + register | 698 与 build 代码 |
| 测试 | D | `public/test/**` | 生产代码 |

**在途文件（本批禁改，必须复用其形状）**：
- `internal/module/navigation/dto/navigation_update_req.go`（及 `_test.go`）—— 在途 A 已建判别 DTO；
  本批的 `navigation_update_envelope.go` 若与它语义重叠，**以在途文件为准合并**，不得新建第二份判别。
- `internal/module/navigation/contract/navigation_translation_source.go` —— 在途 source 契约（`SourceIndex` 相关）；
  §3.2 的 `OpenSourceIndex` 签名必须**核对它的实际形状**后再定，不得另造。
- `internal/templates/static/js/ui/write-base.js` / `write-contracts.json` / `write-items.test.cjs` / `write-latest.test.cjs` —— 在途 FE；
  §4.1 第 12 行只在其上**增量**，不改既有契约键。
- `internal/templates/admin/navigation/navigation_translations.html` —— 在途 FE 已改，本批只补「不可翻译行只读说明」。
- `pkg/i18n/content_write.go` —— T1（`UpsertTx` / `requireTx`）与 R1 均在途未提交；本批只做 Sentinel 收敛。
- **ASM 报告已给出 source 契约的形态** → 以 `ASM-write-items-contract-report.md` 为准，不发明新接口。

### 4.6 presentation 侧（同形态，最小改动）

- `InstanceEntity` 新增 `InputVersion int64`（列 `input_version`）；推进点 = 该模块的 stale / 模板 / 快照写入（同 §2.2）。
- `freezePublish`（`:181-240`）在 `*inst = *fresh` 之后把 `fresh.InputVersion` 存进 `publishFreeze`。
- `commitPublish`（`:270-341`）保留进程锁与指纹校验；**新增** `:299` 的 `persistMultiLangArtifacts` 与
  `:316` 的 `finalizeMultiLangBatch` 用 `frozen.inputVersion` 做谓词（`UpdateInstancePointersTx` 内 `WHERE input_version = $expect`），
  0 行 → `ErrInputMoved`（新错误，presentation enums 声明，409）。
- `publishOneLang`（`:946-988`）的 `MarkPublishedLang`（`:968`，自动提交）→ `MarkPublishedLangTx`（`:83-101`，已存在），
  与 `finalizeMultiLangBatch` 的指针推进落进**同一事务**（在 `commitPublish` 内包一层 `TransactionScoped`）。
- `recoverOneReceipt`（`:1155-1215`）改为「事务内：`GetInstance` → `inst.InputVersion == rc.CapturedInputVersion` → 链接身份 → 写」；
  不满足 → `superseded`，不改 FS/DB。
- 逐语言 FS `activate`（`presentation_render.go:429-435`）**不搬进事务**（多语言循环 + 单事务会长时间持锁）；
  它与指针的偏差由 `input_version` 变化后的下一次重建收敛（既有 `markBatchUnconverged` + 队列）。

---

## 5. 版本键、锁键与 R1

### 5.1 R1 契约要点（与主审 ⑥ 对齐）

- 旧 `LoadDetailsForProject` / `LoadTargets` / `LoadDetails` / `LoadTargetsForProject` **一行不改**。
- 新增两条 `…Tx` 读：同 SQL / 归一核心、**无 `FOR UPDATE`**、不自开事务、不设 scope。
- **scope 双保险**：调用方先 `rls.ScopeTx`（会话变量）+ SQL 内显式 `project_id` 谓词（`project_id IS NULL OR project_id = $n`）。
  **显式参数不等于 RLS 作用域**，两者缺一不可。
- 契约措辞：「本 API 明确不承担校验 scope」—— 不是「`current_setting` 读不到」（PG 里可读，错的是拿它当校验依据）。
- 错误：空 / 非法工程一律 `ErrContentProjectRequired`；非事务句柄 `ErrContentTxRequired`（`requireTx` 先行）。
- PG 测试目标 `public/test/pkg/i18n`，**不回迁 pkg**。

### 5.2 锁键规范化

```go
// LockProjectContentWrite 在调用方事务内取「本工程内容译文写」的事务级互斥锁。
// projectID 空或非法 → ErrContentProjectRequired；合法则内部 uuid.Parse(...).String() 归一，
// 键 = "i18n.content:" + 规范UUID（小写带连字符）。归一在 pkg 内，不回写调用方。
func LockProjectContentWrite(ctx context.Context, tx *gorm.DB, projectID string) error
```

- 证据（现有入口不保证规范）：`pkg/rls/rls.go:78` 只做 `uuid.Parse` 校验，随后把**原样字符串**写进 `set_config`；
  工程 ID 由 `project/service/project.go:97 uuid.NewString()` 生成（小写），但请求参数未归一。
- navigation service 在 `projects.Exists` 通过后做一次 `canonical := uuid.Parse(req.Project).String()`，
  之后**统一用** `canonical` 传给 `ScopeTx` / `LockProjectContentWrite` / `LoadDetailsForProjectTx` / `UpsertTx`。
- `raw_sql_boundary_test.go` 的扫描根只有 `internal/module`（`:113-116`），**pkg 不在范围 → 不登记白名单、不扩扫描根**。

### 5.3 任务号源（build owner 域，本批只读引用）

- `build_job_seq`（表名已在 `build_job_seq.go:18` 定死为 `build_job_seq`，**无 `tbl_` 前缀**）由 build owner 的 698 建表；
  `NextMenuStaleVersionTx`（`:49-68`）已实现 `INSERT … ON CONFLICT DO UPDATE … RETURNING next_version`。
- 本批**只**依赖一个语义：该号是**任务序列**，进 `build_jobs.input_version` 与 `build_input_hash`，**绝不写实体列**（§2.6）。
- 若 698 尚未落地，`EnqueueMenuStaleTx` 的实现与注册**由 build owner 提供**；navigation 端口只按 §3.2 的签名调用。

---

## 6. 验证

### 6.1 故障 / 并发测试矩阵（等式断言）

| # | 场景 | 注入点 | 断言 |
|---|---|---|---|
| F1 | 事务内第三端口失败 | `MenuStaleEnqueuePort` 抛错 | `sys_translation` 零行、`pages.stale` 未变、`pages.input_version` 未变、`build_jobs` 零任务、HTTP 5xx |
| F2 | 首次翻译同 hash 的两并发 A/B | 两 goroutine 同工程同 lang | 工程级锁串行：终态 = 后提交者值；`sys_translation` 恰好 1 行 |
| F3 | 同 hash 同值重复提交 | 连续两次 | 第二次 `written==0`，`input_version` 不变，零入队 |
| F4 | **编译后输入推进再激活** | 捕获 `v` → 执行 `SaveDraftWithRevisionTx`（`v+1`）→ 激活 | `activated=false`；`page_publications` 未写；`pages.stale` 仍 true；FS 链接**仍是旧目标**（FS 未被调用） |
| F5 | FS 成功后 DB 失败 | `applyPublishActivation` 内故障注入 | 线上 symlink **仍是新产物**（不退回 `from`）；回执 pending；收敛后 `page_publications.artifact_id == own` |
| F6 | 同页两并发发布会话（人工 + 队列） | 两个 `publish` 并发（`-race`） | 行锁串行；最终可见指针 = 后提交且版本相等者；落败方 `activated=false` 且不写任何行 |
| F7 | `MarkStagedLangTx` 后构建失败 | `planPageRebuild` 之后失败 | `pages.stale` **保持 true**（本批修复） |
| F8 | 锁键大小写 | 同一工程传大写 UUID | 与规范形式取到**同一把**锁 |
| F9 | 锁键非法 / 空 | `not-a-uuid` / `""` / `"   "` | 一律 `ErrContentProjectRequired`，事务未污染 |
| F10 | 同时保存 header+footer_mobile | 一次请求 | 两条 menu_stale 任务，`context_key` 不同 |
| F11 | 空 target 与不可翻译行混合 | 模板只渲染可翻译行输入框 | 不触发 409；`written` 只计非空行 |
| F12 | 回执恢复：链接=own，DB 已被别人替代 | 手工造 pending 回执 + 后一次成功发布 | 结案 `superseded`；`page_publications` **未改**；FS **未改** |
| F13 | 回执恢复：链接=own，版本已推进，DB 未替代 | 同上 + 推进 `input_version` | 入队一次 page 重建（`build_jobs` 恰好多 1 行）；回执 pending 或 superseded 二者之一，**不得**只是 rolled_back |
| F14 | 同输入版本两次不同产物激活 | 同 `input_version` 连续两次激活不同 artifact | 第二次同样成功（版本相等即允许）；`page_publications` 终态 = 第二次的 artifact；证明「版本相等不足以重放旧回执」 |
| F15 | 激活不推进版本 | 单次发布 | 发布前后 `pages.input_version` **相等** |
| F16 | 多语言同批 | 一次重建两种语言 | 两种语言均 `activated=true`（激活不推进版本 → 第二语言不因第一语言而失败） |

### 6.2 命令（集中验收，单负责人跑；实现代理只写源码）

```bash
go build ./...
go test ./pkg/i18n/ -count=1
go test ./internal/module/navigation/... -count=1
go test ./internal/module/page/... ./internal/module/presentation/... -count=1
node --test internal/templates/static/js/ui/write-base.test.cjs
bash scripts/check-write-attributes.sh
GOWP_REQUIRE_PG=1 go test ./public/test/navigation/feature/... -run 'TranslationWrite|PublishFencing|ReceiptConverge' -count=1
GOWP_REQUIRE_PG=1 go test ./public/test/page/feature/... -run 'PublishFencing' -count=1
GOWP_REQUIRE_PG=1 go test ./public/test/pkg/i18n/... -count=1
bash scripts/check-all.sh
go test ./internal/architecture/ -count=1
```

- **不跑** build 模块测试（本批不改 build 代码）；全量 `go test ./internal/... ./pkg/... ./public/...` 只在合并进 main 前一次。
- T0（`binding.JSONLimited`）不重跑。

### 6.3 真实浏览器验收（1440×900 / 768×1024 / 375×812）

| 用例 | 判据 |
|---|---|
| B-1 布局 | 无横向滚动；粘性表头不遮首行；输入框可点 |
| B-2 错误明细 | 409 时明细逐条可见（id/name/reasonKey/message），375 视口不裁切 |
| B-3 键盘 | Tab 顺序 = 视觉顺序；Enter 不触发整页提交 |
| B-4 失败不导航 | URL 不变、输入逐字保留 |
| B-5 只读行 | 纯数字菜单行无输入框、有说明；提交后无 400/409 |

---

## 7. 结论汇总（已定，无签字项、无待核实项）

1. 判定源 = 实体 `input_version`；激活只验证、不推进。
2. 两段式可行；阶段 2 的「复验 → FS → 提交」不可拆。
3. model 只锁读 / 只写本表，无 FS 回调；FS 由 service 调 publisher。
4. 失败不补偿 FS、不 rename 回 `from`；收敛按 §2.7 四步判定，superseded 或重建闭环。
5. 迁移 699（先查占号），不碰 698 与 build 代码。
6. 菜单 CRUD 的版本推进 = 依赖标记那一处；menu_stale 任务号与实体版本严格分离。
7. 同实体多入口（人工发布 / 队列重建 / 回滚 / 三种恢复）全部落同一把实体行锁。

---

## 8. 边界（不变）

- 不改 `/api/navigation/update` 路径与权限点（`036_blueprint_navigation_permissions.sql:15`：`navigation:update`）。
- 不改 `Fanout` 既有行为；事务内不调 `Fanout`。
- 不复用 `sys_menus`，不新增 HTML 动态片段。
- SEO G6 已验收，不扩围。
- 不动三个旧 `Load*`；不在 pkg 里加业务约束；不碰主库；不改 698 与 build 代码。
- 本代理只写 3 个文件：本任务书、`.superpowers/sdd/navigation译文MVC下一批规划草案/activation-plan-ruling.md`、`progress.md`（追加一轮）。
