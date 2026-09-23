# workbench 域 `c.String` 失败出口审计（51 处）

> 方法：**只读静态侦察**，未改动任何文件。判据来源：`AGENTS.md`「响应与错误处理」、
> `docs/02-P-project-page-err-batch.md`（判据与出口形态表）。
> **主会话已独立复核**的结论在每节标注 `[已核实]`；未核实的标 `[待运行时验证]`（见 §7）。

## 1. 关键纠正：`02-L` §0.7.2 的预分类不成立

`docs/02-L-admin-rework-worklist.md` §0.7.2 曾把 workbench 51 处整体归为
「片段/接口出口，非本轮判据」（即 P2 规范问题）。**该结论对其中的 23 处无效。**

`[已核实]` `/workbench?id=` / `?block=` / `?template=` / `?instance=` 四个入口在模板里
**全部是浏览器直接打开的链接**，不是 fetch：

```
internal/templates/admin/pages.html:112        <a class="btn btn-sm" href="/workbench?id={{p.ID}}">
internal/templates/admin/dashboard.html:104    <a class="btn btn-sm" href="/workbench?id={{p.ID}}">
internal/templates/admin/blocks.html:178/213/250 <a class="btn btn-sm" href="/workbench?block={{b.ID}}">
internal/templates/admin/product_detail_template.html:169 <a href="/workbench?template={{t.ID}}&amp;entityType=…">
internal/templates/admin/product_edit.html:338  <a class="btn btn-primary" href="/workbench?instance={{…}}">
```

这些 `c.String(4xx, "缺少页面 id")` **就是脱离页壳的页面响应** —— 浏览器里只有一行纯文本，
侧栏、页头全没。性质与 `02-P` 已收口的 project 域 20 处完全相同，**不是 P2**。

## 2. 分类统计

| 类别 | 处数 | 位置 |
|---|---|---|
| (a) 硬编码中文 | 42 | `workbench_handle.go` 30 处（48,56,61,95,103,113,158,163,188,193,198,223,228,244,249,253,259,276,281,286,336,341,354,368,374,387,400,406,433,437）；`workbench_preview_handle.go` 8 处（25,30,40,45,57,62,66,96）；`workbench_instance.go` 4 处（31,37,69,92） |
| (b) 裸 i18n key | 3 | `inspector_handle.go:155,161`；`workbench_preview_handle.go:100`（实参均为 `workbenchenums.MsgInternalError`） |
| (c) 已走归口 | 6 | `workbench_err.go:239,246,251`；`workbench_instance.go:46`；`workbench_handle.go:457,461` |
| (d) `.Error()` 真泄漏 | **0** | — |

`[已核实]` (b) 不是纸面问题：`c.String` **不经过任何翻译层**（对比
`pkg/response/response.go:49` 的 `ErrorWithMessage` → `translate(c, …)`），
用户拿到的是 `MsgInternalError` 这串英文；词条在 `058_i18n_seed_enums.sql` 已 seed，
缺的只是取词一步。

`[已核实]` (d) 为 0：`rg -n 'c\.String\([^)]*\.Error\(\)' internal/module/workbench/` 无命中；
`workbench_instance.go:129` / `workbench_handle.go:455` 虽调用 `err.Error()`，但只作
`strings.Contains` 的分类输入，原文不入响应，随后走归口 422 —— **形状正确**。

## 3. 一处形态不一致造成的静默失败

`workbench_instance.go:92`：`InstanceSave` 在能力未装配时
`c.String(http.StatusServiceUnavailable, "实例编辑能力未装配")` ——
**这是同文件里唯一的纯文本分支，其余分支都走 `c.JSON`**。

`[已核实]` 前端 `internal/templates/static/js/workbench/methods/api.js`：

```js
:85  return fetch(target.save.path, {method:'POST', headers:{…'application/json'}, body:…})
:94      if (r.redirected) { return { redirected: true, url: r.url }; }
:95      return r.json();          // ← 对 text/plain 响应会抛异常
```

`r.json()` 解析纯文本抛异常 → `then` 链断掉 → 到不了 `:22-26` 的
`if (!res.ok) { saveState='error'; alert(res.j.message || '请求失败') }`，
**用户看不到任何提示**。

可达性低（前提是 `h.instances == nil`，装配缺失），但**改动成本约一行**、与同文件其余分支对齐，值得顺手做。

## 4. `workbench_err.go` 现状评估

三个出口全部受控：`workbenchFacingText(c, key)`（白名单 → 当前语言译文，未命中落空串）、
`workbenchCompileFallbackText(c)`（归口，key `MsgCompileFailed`）、
`writePreviewCompileRejected(c, document, kind, err)`（编译失败 422 的三级分类）。

**自身受控性：是。** 白名单 8 条的键取自 `workbenchenums` 常量；取值经 `shell.TranslateFor(c)`；
`workbenchCompileFallback = "预览编译失败"` 是与 058 词条值逐字一致的刻意兜底
（文件头注释论证了「i18n 未初始化时响应体一致」这一取舍）。

**覆盖率低是 (a) 有 42 处的原因**：它覆盖的是「预览/编译 422」这一族（6 处），
**完全没有覆盖** 400 参数校验 / 404 目标不存在 / 500 序列化与装配失败 / 503 能力未装配这四类
（即 51 处中的 45 处）。

## 5. 分批方案与顺序依赖

| 批次 | 范围 | 处数 | 目标形态 | 前置条件 |
|---|---|---|---|---|
| **批 1**（P0 形态真缺陷） | `workbench_handle.go` 20 处 + `workbench_instance.go:31,37,69` | 23 | 页面导航路线：`303` 回来源列表页 + `?err=`（过读侧白名单）或页壳内错误态 | **`303 + ?err=` 的读侧白名单落在回跳目标页**（`/admin/pages`、`/admin/blocks`、`/admin/products`…）—— 那些文件正被其它并行任务改，需等它们落定 |
| **批 2**（P1 裸 key） | `inspector_handle.go:155,161` | 2 | 改文案来源：`shell.TranslateFor(c)(MsgInternalError, 中文兜底)`；状态码与形态不变 | 无（文件干净） |
| **批 2'**（同型） | `workbench_preview_handle.go:100` | 1 | 同上 | ⚠ 该文件**正在被改**，需让开 |
| **批 3**（P1 形态不一致） | `workbench_instance.go:92` | 1 | `c.String(503, …)` → `c.JSON(503, Response{…})`，与同文件其余分支对齐 | 无 |
| **批 4**（P2 文案规范化） | `workbench_handle.go` 10 处 + `workbench_preview_handle.go` 8 处 | 18 | **保持 `text/plain` 形态**（iframe body 直显，形态本身合理），文案改走 enums + `workbench_err.go` 出口 | 两个文件都在**批 1/批 2 的同一批改动**里，合并做 |

**为什么批 1 不能用 `shell.PageError`**：它内部是 `response.ErrorWithMessage(c, 500, MsgInternalError)`
（`internal/web/shell/errors.go:34`）→ 返回 **JSON**，等于把「白页一行字」换成「白页一段 JSON」。
`docs/02-O-trade-site-audit.md:329` 就同一形态记过这个结论。`02-P` §2 的判据表可直接复用。

**批 4 的形态判据**：`/workbench/preview` 系列走两条路 —— `submitCanvas` 是原生表单 submit 进
iframe（`canvas.js:441`，`target='wb-canvas'`），纯文本显示在画布区域是刻意设计；
`fetchCanvasHTML` 是 fetch 但 `!res.ok` 直接 `Promise.reject`（`canvas.js:499/512`），**不读 body**。
两条路上纯文本都是安全的，改动只应发生在文案来源。

## 6. 冲突面（`[已核实]`）

workbench 域已有**未提交的在途改动**：

```
M internal/module/workbench/inbound/http/editor_bridge.go
M internal/module/workbench/inbound/http/router.go
M internal/module/workbench/inbound/http/workbench_preview_handle.go
M internal/templates/static/js/workbench/{generated-contracts.js,index.js,methods/{canvas,nodes,panels,shortcuts}.js}
?? internal/module/workbench/inbound/http/editor_bridge_test.go
?? internal/templates/workbench_slot_frame_test.go
```

`workbench_handle.go`、`workbench_instance.go`、`inspector_handle.go` **不在**改动清单里 ——
批 1 的 20 处、批 2 的 2 处、批 3 的 1 处落在干净文件上；**批 2' 与批 4 需与
`workbench_preview_handle.go` 的在途改动协调**。

另外 `internal/web/shell/{notice.go,notice_test.go,shell.go}` 也在改。
`[已核实]` 其内容是「nilsafe 回落从 `/` 改成 `/admin`（`/` 现在归前台首页）」，
与 `shell.FacingNotice` / `FacingQueryText` **无关**，不影响其它域的读侧白名单实现。

## 7. 需运行时验证的点（不要用静态推断代替）

1. `/workbench?id=` 等 400/404 在浏览器里的真实呈现（是否确为无页壳白页）—— 静态可推断
   （`c.String` 走 `text/plain`），但仍应实测一张截图钉住证据。
2. 画布 iframe 内 422 纯文本的可见性，尤其 `409「草稿版本已更新」` —— 它落在 iframe 里，
   作者能否读到未验证。
3. `inspector_handle.go:155/161` 的可达性（依赖 `builder.ComponentSchemas()` 或
   schema `json.Unmarshal` 失败，前者可能是启动期即便坏的状态）。
4. `workbench_preview_handle.go:100` 的 `default` 分支可达性（需 `CompilePreview` 返回
   既非 `ErrPreviewInvalidDocument` 也非 `ErrPreviewCompileFailed` 的错误）。
5. `builder.ValidatePageTolerant` 的文案值域：`workbench_err.go:239` 把 `problem.Msg` 拼进响应，
   需确认其文本**不含**节点 id / 内部路径；若含，则该处属「受类型标记保护的间接泄漏」。
6. 批 1 的前置：`internal/web/shell/errors.go` 现有四个出口**全是 JSON**，
   没有「渲染页壳的错误页」出口 —— 这决定批 1 是走 303 回跳还是新建页面级出口。

## 8. 另一处顺带发现（不在 51 处内）

`inspector_navigation.go:91` 用 `response.ErrorWithMessage(c, 503, workbenchenums.MsgInternalError)` ——
因 response 层会 `translate`，**不构成裸 key 缺陷**；而 `:126` 直接
`return workbenchenums.MsgInternalError` 由调用方决定出口，**需确认消费侧是否经过翻译**（未核）。

`outline_handle.go` 与 `seo_score_handle.go` 本次扫描无 `c.String` 命中。
