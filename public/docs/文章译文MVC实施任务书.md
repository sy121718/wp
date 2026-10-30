# 文章译文 MVC 实施任务书

> 执行技能：subagent-driven-development（固定成员分工；全部实现与测试源码完成后，由单一负责人集中验收）。
> 本文件只规划，不含实现；任何"新能力"若未在本文冻结签名，一律标 **待审定**，不得当成已批准。

**目标：** 把「文章译文工作台」的读写从页面 handler 迁到 service 事实层：请求严格白名单、失败逐行事实、工程范围由服务端校验、译文/页面 stale/实例 stale/可靠任务同事务，前端改用统一写基座。
**架构：** service 产出事实与事务边界，handler 只做绑定 → 调用 → 响应；模板只声明事实与结构，行为在 `ui/write-base.js`。
**技术栈：** Go、Gin、Jet、原生 JS、PostgreSQL（RLS）。
**依据：** AGENTS.md（分层判据、事务、文案、门禁）、public/docs/其他模块MVC职责分离规范任务书.md（§6.5 工程范围、§6.6 事务、§7 阶段、§8 所有权、§9.2 验收矩阵、§10 伪完成方式）、public/docs/商品模块MVC改造执行记录.md（同形迁移的既有记录）。

## 全局约束与工作树

- 工作树 `/home/sky/project/go/wp/.worktrees/mvc-publication-seo`，分支 `refactor/mvc-publication-seo`。
- 实现期只允许：读源码、写实现与测试源码、`gofmt`、静态差异自检。**不运行** build / test / vet / 门禁 / DB / 浏览器；不提交、不合并、不推送。
- 不动其他模块已冻结的写域；跨域需求（presentation、page、router/assembly、permission）由对应负责人接线，本域只提交所需的最小契约。
- 不新增可空适配层掩盖缺口；能力缺失时返回明确缺口事实（`project_filter_unavailable` 之类）而不是"空列表"。

## 现状（源码证据，全部行号为实测）

| 事实 | 位置 |
| --- | --- |
| 写路径全在 handler：`c.PostForm` / `PostFormArray` 三组并行数组按位置对齐 | `internal/module/content/inbound/http/content_page.go:1433-1454` |
| 读现状用**全局**作用域 `LoadDetails`，失败后 `before = {}` 继续写（吞错） | `content_page.go:1511-1515` |
| 写入 `Upsert` 自开事务，与页面/实例失效无关 | `content_page.go:1529` |
| 保存后**零** stale 标记（全仓 grep 无 `MarkStaleForI18n` 调用） | `content_page.go:1543` |
| 成功走整页提示页 `shell.RenderJump`（200） | `content_page.go:1573-1585` |
| 读侧分页过滤只有 `EntityType + Keyword`，无工程维度 | `content_page.go:1693`、`content_page.go:1714` |
| 读侧回填译文用**全局** `LoadTargets`，失败只记日志 | `content_page.go:1773-1776` |
| 视图类型定义在 http 层（`articleTranslationRow/Group/Handle`） | `content_page.go:1364-1392` |
| 校验 `validateArticleTarget`（语境/可翻译/富文本同形） | `content_page.go:1629-1641` |
| 路由：GET 页面 + POST 保存均 `adminui.PageSlot`，保存复用 `/api/content/update` | `content_page_router.go:102-121` |
| `contents` 表无 `project_id`（对照 `products` 有） | `public/migrations/042_content.sql`；`public/migrations/081_product_tables.sql:17` |
| 文章 → 工程的唯一真实关联 = presentation 实例 | `presentation/service/presentation_publish_record.go:30-48`（`ListInstances(projectID, entityType)` 含未发布实例） |
| 现成可复用的发布登记只读端口（含 draft 行） | `presentation/contract/publish_record.go:73-80` `PublishRecords(ctx, projectID, entityType, entityIDs)` |
| i18n 侧已有工程优先读 + Tx 写 + 项目锁 | `pkg/i18n/content_write.go:203`（`LoadDetailsForProject`）、`:310`（`…Tx`）、`:444`（`UpsertTx`）、`:366`（`LockProjectContentWrite`，键 `i18n.content:` + 规范 uuid） |
| page 已有译文失效 Tx 端口（逐工程、页面与 peer 同 tx、errors.Join） | `page/service/page_lang.go:344-434` |
| presentation 只有整站 `MarkStaleForI18nTx`，**没有** 按依赖的 Tx 变体 | `presentation/model/presentation_model.go:444`、`:1081`（自开事务） |
| build 队列 source_type 白名单只有 page / presentation / menu_stale | `public/migrations/698_navigation_menu_stale_sequence.sql:176-177`；`build/service/build_service.go:141-160` |
| 商品侧「stale 失败只标记」不是推荐范式 | 规范 §6.6:163；`product/service/product_translations_page.go:238-247` |
| 文章未纳入同工程标题唯一性（既有缺口记录） | `product/service/product_seo_score.go:250-254`（`project_filter_unavailable`） |

## 冻结接口（本批）

### 1. 事实结果（service 出口）

```go
// internal/module/content/contract/article_translation.go（新文件）
package contentcontract

// ArticleTranslationWriteResult 一次整表保存的**成功**事实。
//
// 只在全部成功时返回；任何一行被拒或任一失效/入队步骤失败，整个事务回滚并返回错误，
// 本结构不出现在响应里（成功回执与失败回执不共用同一个类型）。
type ArticleTranslationWriteResult struct {
    Written         int      `json:"written"`         // 真正写入的行数（同值跳过不计）
    ChangedContexts []string `json:"changedContexts"` // 写入行的语境名，按提交顺序
}
```

- **无错全成**：不存在"部分成功 + 逐行说明"的成功回执。任一行被拒 ⇒ 400，逐行事实走 `data.items`（`response.ErrorRows`），本结果不返回。
- **失效/入队失败 ⇒ 整笔回滚**：标记页面、标记实例、入队任一失败，事务不提交（译文行也不落库），对外是 500 类失败，不出现"已保存但未标记"的降级标志。
- 读侧失败（现状读取）⇒ 直接返回 error（**不得**降级为"全部变更"再写）。
- 明确否决：不引入 `RowErrors`（成功结构里不携带逐行拒绝）、不引入 `StaleMarkFailed`（商品屏 `product/service/product_translations_page.go:238-247` 的降级范式在本批不复用，规范 §6.6:163 已把它标为非推荐范式）。

### 2. 请求形状（dto 严格白名单）

```go
// internal/module/content/dto/article_translation_req.go（新文件）
type ArticleTranslationSaveReq struct {
    ProjectID string                      `json:"project" form:"project"` // 必填，服务端校验存在且属于工程集合
    Lang      string                      `json:"lang" form:"lang"`       // 必填，须在 i18n.AvailableLangs()
    Rows      []ArticleTranslationSaveRow `json:"rows" form:"-" `
}
type ArticleTranslationSaveRow struct {
    Context string `json:"context"` // 必须命中 i18n.ContentContext("article", field)
    Hash    string `json:"hash"`    // 必须与**服务端重算**的 hash 一致
    Target  string `json:"target"`  // 空串 = 本行不动；缺该键 = 形状错（拒）
}
```

严格口径与导航屏同形（可借鉴 `navigation/dto/navigation_update_req.go:494-533`，但**不共用类型**）：数组、每项对象、三键必须都在、值必须 JSON 字符串、`> 1000` 行拒、顶层键只允许 `{project, lang, rows, csrf_token}`。

### 3. 工程范围端口（本批新增的最小只读事实）

```go
// internal/module/content/contract/article_scope.go（新文件，消费者侧声明）
// ArticleProjectEntityIDs 按工程列出**该工程真实存在实例关联**的文章实体 ID（含未发布实例）。
// 只在库内查询，不遍历其它工程，不取"第一个工程"。
type ArticleProjectEntityIDs interface {
    ProjectArticleEntityIDs(ctx context.Context, projectID string) (ids []string, err error)
}
```

- 命名刻意不叫 `PublishedEntityIDs`：它**不承诺已发布**（草稿实例也算关联）。
- 生产实现（**待审定**）：可由既有 `presentationcontract.PublishRecordPort.PublishRecords(ctx, projectID, "article", nil)` 派生去重 ID 列表，或由 presentation 侧新增一条等价窄端口。二者取舍交 presentation 负责人裁定；本域只依赖上面这条接口，不猜其内部签名。
- 未注入时：读侧 `HasScope=false` 并返回 `project_filter_unavailable` 事实（不是空列表），写侧直接拒绝（400）。

### 4. 写端口（service 依赖的三件事，全部同 tx）

```go
// internal/module/content/contract/article_translation.go
type ArticleTranslationStalePorts struct {
    // Pages 标记本工程受影响页面待重建（按 i18n 依赖源）。
    Pages interface {
        MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string) error
    }
    // Instances 标记本工程受影响自动发布实例待重建。
    Instances interface {
        MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string) (n int64, err error)
    }
    // Enqueue 在调用方事务内为受影响页面/实例入队可靠重建任务（复用现队列 source 语义）。
    Enqueue interface {
        EnqueueArticleTranslationRebuildTx(ctx context.Context, tx *gorm.DB, projectID string, pageIDs, instanceIDs []string) error
    }
}
```

- `Pages` 与 `Instances` 的生产实现已存在（`page/service/page_lang.go:344-434`、`presentation/model/presentation_model.go:444`），差别只在把 `I18nStalePeer` 的两条方法直接接到这里。
- `Enqueue` 是**本批唯一真正缺失**的能力：`build.Service.Enqueue` 自开事务（`build/service/build_service.go:141-160`），无法参与调用方事务。**待审定**项：(a) 给 build 增加 Tx 变体（与 `EnqueueMenuStaleTx` 同形但不套 menu kind 取号）；(b) 或先只做"标 stale + 依赖失效扇出自动入队"，把可靠入队降级为**待审定缺口**并在报告里明示。二者择一由主负责人裁定，本域不预实现。
- 明确禁止：把文章译文套进 `menu_stale` 的 kind 取号流程；也不新增 `source_type`（迁移 698 的 CHECK 只有三值）。

### 5. 事务与回执语义（冻结）

一次保存 = 一个事务，顺序固定：

1. `uuid` 归一 `project` → 校验工程在 `projects.List` 集合内（否则 400）。
2. `i18n.LockProjectContentWrite(ctx, tx, projectID)`：与导航/商品译文写**同一把** advisory 锁（`i18n.content:<uuid>`）。
3. 服务端重建来源：对该工程的文章集合逐篇按 `TranslatableFields("article")` + `ShouldTranslateContent` 重算 `(context, hash)`；提交行必须命中，未命中 → 该行拒绝（`TranslationsErrSourceChanged`）。
4. `LoadDetailsForProjectTx` 读现状（**读失败即失败**，不降级）；同值且 `Engine == manual` 的行跳过。
5. `UpsertTx` 写入工程行（`ProjectID` 必填；**不写/不覆盖**全局行）。
6. `Pages.MarkStaleForI18nTx` → `Instances.MarkStaleForI18nTx` → `Enqueue`（同 tx，零命中允许，不调用不允许）。
7. COMMIT 后返回 `ArticleTranslationWriteResult`。

- **失败即回滚，无降级标志**：第 3～6 步任一失败（含逐行拒绝、页面/实例标记失败、入队失败）整笔回滚，译文行**不落库**；不存在 `StaleMarkFailed` 这类"写成功但没标记"的中间态字段（该范式已否决，规范 §6.6:163 明确它是反例）。
- **成功回执只含写入事实**：`Written` + `ChangedContexts`；逐行拒绝只出现在失败响应的 `data.items`，两者不共用同一结构。
- 多工程共享同一全局译文行（`project_id IS NULL`）：读侧回落，写侧永不覆盖；同一 `hash` 在不同工程各写各的工程行（唯一键 `(project_id, source_hash, context, lang)` NULLS NOT DISTINCT，迁移 195）。

## 前端冻结方案

- 保存表单改为：`data-write="update"` + `data-write-url="/api/content/articles/translations/save"` + `data-write-json="rows"` + `data-write-items` + `data-write-reload`。
- 行容器改为 `<template name="rows" data-row>`，内含 `context` / `hash` / `target` 三个精确 name 的控件（`write-base.js:218-265` 的 `jsonFields` 口径）；不再使用 `rowTarget` / `rowContext` / `rowHash` 的并行数组。
- 成功：`data-write-reload` 重载当前页（保留 `lang`/`project`/`keyword`/`page`/`limit` 的 query），回执文案由 JSON `message` 走 toast；**不再**整页提示页。
- 失败：`data.items` 逐行列出 `Label: reason`（`write-base.js:783-820` 的 `optInLine` 口径），输入内容保留在 DOM。
- 无 JS：不退化提交（保存按钮 `disabled data-requires-js`），与导航屏一致（`navigation_translations.html:50-51`）。
- 表单属性必须落在 `scripts/check-write-attributes.sh` 白名单内（`data-write-json` / `-items` / `-reload` 均已登记）；不得使用 htmx 写属性。

## 文件所有权与任务断言

### A：后端事实与请求（content 域，后端负责人）

拥有 `internal/module/content/contract/article_translation.go`、`contract/article_scope.go`、`dto/article_translation_req.go`、`enums/content_admin_enums.go`（既有文件，仅补 key）、本域 service/DTO/enums 的测试源码。

- [ ] 写 `ArticleTranslationSaveReq` 严格解析表驱动测试：缺键、非字符串、超 1000 行、空数组、空 target 保留、未知顶层键拒绝。
- [ ] 写 `ArticleTranslationWriteResult` 断言：成功结构只有 `Written` 与 `ChangedContexts`（按提交顺序）；**不存在** `RowErrors` / `StaleMarkFailed` 字段（编译期断言 + 序列化断言）。
- [ ] 写工程校验测试：工程不在集合内 → 400 事实；端口未注入 → 读侧 `project_filter_unavailable`、写侧拒绝。
- [ ] 写"服务端重算 hash"测试：伪造 `hash` 的行被逐行拒绝，且**零写入**。
- [ ] 写"读现状失败不写"测试：`LoadDetailsForProjectTx` 返回错误时 `UpsertTx` 调用次数为 0。
- [ ] `gofmt`、自检；报告文件、签名、未决项；不运行任何测试。

### B：service 事务与失效闭环（content 域，后端负责人）

拥有 `internal/module/content/service/article_translation_write.go`（新）、`service/content_service.go`（如需注入端口）、对应 `_test.go`。

- [ ] 按"冻结项 5"实现七步编排；端口缺失 fail closed（明确错误事实，不是空写）。
- [ ] 断言同 tx：用测试桩记录 `MarkStaleForI18nTx`（pages / instances）与 `Enqueue` 的调用顺序为 `page → instance → enqueue`；三处任一返回错误时**整笔回滚**：库内译文仍为旧值、无任务行、页面/实例未被标记（不是"记个标志位继续提交"）。
- [ ] 断言零命中仍调用（与 navigation 同判据）。
- [ ] 断言条目：`UpsertTx` 写入项的 `ProjectID` 恒等于请求工程；**不出现** `project_id IS NULL` 的写入。
- [ ] 断言：同值 manual 行 `Written` 不增加、`UpsertTx` 不调用。

### C：HTTP 与路由（属主负责人 + 主负责人）

拥有 `internal/module/content/inbound/http/article_translation_handle.go`（新，拆出译文相关）、`content_page.go` 中译文相关段的删除、`content_page_router.go`、`internal/routers/assembly_publish.go` 的 content 装配块。

- [ ] 新增 `POST /api/content/articles/translations/save`：`binding.JSON`/`binding.Form` 统一入口，权限点复用 `content:update`（不新增权限点，与现状一致），出口 `response.Success` / `response.ErrorRows(c, 400, …)` / `response.ErrorStatusAuto`。
- [ ] GET `/admin/articles/translations` 保留页面出口（`pageview.Render` 页面成功；失败 JSON），删掉 POST 页面路由与整页提示页。
- [ ] 装配：注入 `ArticleProjectEntityIDs`、`Pages`、`Instances`、`Enqueue` 四端口；缺一即 `panic`（装配缺陷，与 `assembly_publish.go:260-276` 同风格）。
- [ ] 门禁台账：`pendingControllerBinding` / `pendingControllerQuery` / `pendingControllerMethods` 中 `content_page.go` 的**译文相关**条目必须真实减项；不得新增豁免。

### D：模板与前端（前端负责人）

拥有 `internal/templates/admin/content/article_translations.html`、必要时 `internal/templates/admin/partials/*`。

- [ ] 表单迁 `data-write` + `data-write-json="rows"`；行改 `<template name="rows" data-row>`；保存按钮 `disabled data-requires-js`。
- [ ] 工程选择与 `project` 事实一致（当前模板 `. ["ProjectID"]` 与 handler 读取路径不一致，须统一：GET 读 `binding.Query`，模板块用同一字段）。
- [ ] 删掉 `csrf_token`/`keyword`/`page`/`limit` 的隐藏域中被写基座接管的部分；保留筛选所需的 GET 表单。
- [ ] 空态三分：无工程 / 关键词无匹配 / 无可翻译文章，与导航屏同形（`navigation_translations.html:81-98`）。
- [ ] 断言 `check-write-attributes.sh` 的写法（属性名、裸 flag、无 hx 属性）。

### E：feature 测试与集中验收（主负责人）

拥有 `public/test/content/feature/article_translation_write_test.go`（新）。

- [ ] 真实 PG（生产迁移建表）验证：两工程同 hash 各写各工程行、全局行不被覆盖、读侧回落到全局。
- [ ] 验证事务：注入失效端口失败 → 译文行**未提交**（库内仍为旧值）。
- [ ] 验证权限：无 `content:update` 时 403 JSON，无重定向；未登录 401。
- [ ] 验证逐行拒绝：伪造 hash → 400 + `data.items` 逐条，含语境名；库内零写入。
- [ ] 命令：`GOWP_REQUIRE_PG=1 go test -count=1 -race ./internal/module/content/... ./public/test/content/feature`、`bash scripts/check-all.sh`；集中验收由主负责人执行。

## 主审重点

1. 工程范围真实性：`project_filter_unavailable` 不得作为本批终点（必须有按工程查实例关联实体的落地路径）。
2. 同事务：译文 + 页面 stale + 实例 stale + 可靠入队必须同进同出；"读失败降级为全部变更"是明确缺陷。
3. 严格白名单与逐行拒绝：伪造 hash / 缺键 / 未知顶层键必须 400 且零写入；逐行事实只走失败响应 `data.items`，**不得**混入成功回执。
4. 全局行不可写：任何写入都带 `ProjectID`；同一 hash 多工程互不覆盖。
6. 不得出现降级标志：`StaleMarkFailed` / "已保存但未标记"一类中间态一律否决；失效或入队失败必须整笔回滚。
7. 不得用截断冒充工程全量：按工程分页须为真实窄查询或窄端口，禁用"全量 ID + 内存分页 + 上限截断"。
5. 前端：不插入后端 HTML、不隐式导航、失败保留输入；`data-write-json` 行模板结构正确。

## 未冻结项（不得当成已批准，也不得自行降级）

1. **按工程分页读法**（未冻结）：`contents` 侧现有 `List` 只有 entityType + keyword + limit/offset（`content/service/content_service.go:160-186`），没有工程维度，也没有按 id 集合的读法。
   - **已否决**：全量取 ID → 内存分页 → 按上限截断（截断后的行数不能冒充"本工程全部文章"，属规范 §10「不以第一页/子集冒充全量」）。
   - 待审定的方向（择一，由主负责人裁定）：(a) 在 `contract` 增加按工程 + id 集合的窄分页读法（model 侧一条 SQL：`entity_type='article' AND id IN (...) AND (title/slug ILIKE ...)` 带稳定排序与唯一键兜底）；(b) 由 presentation 提供"按工程分页列实体 ID"的窄端口，content 只按当页 id 取行。
   - 未冻结前，本屏**不做实现**；不得用"先能跑"的过渡方案落地。
2. `ProjectArticleEntityIDs` 的生产实现：复用 `PublishRecordPort.PublishRecords(projectID,"article",nil)` 派生去重 ID，vs presentation 新增等价窄端口。
3. 可靠入队 Tx 能力：`build.Service.Enqueue` 自开事务（`build/service/build_service.go:141-160`），需新增可在调用方事务内入队的形态；与 `EnqueueMenuStaleTx` 同形但**不套 menu kind 取号**，也不新增 `source_type`。未冻结前不得实现"标 stale 就算完成"。

## 当前状态

规划已定稿（本文件）。业务代码未改，未运行构建 / 测试 / 门禁。A/B/D 的实现必须以"未冻结项"全部裁定为前提；C/E 待主负责人排期。
