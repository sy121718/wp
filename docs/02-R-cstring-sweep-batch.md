# 「失败出口收口」多域并行批次汇总（2026-09）

> 本批是 `02-P`（project 域）之后的**全站铺开**：把 `docs/02-P` §5 摸出的 79 处
> `c.String(4xx/5xx, …)` 与同族问题按域并行收口。
> 执行方式：9 个子代理并行（按**文件冲突**分组，迁移编号 404~410 逐组分配），
> **主会话只做方案、复核与统一收口**（每条代理结论都回到代码里独立验证过）。

## 1. 各域结论

| 域 / 批次 | 处数 | 判定形态 | 关键依据（复核后成立） |
|---|---|---|---|
| **project** | 21 | `303 + ?err=` / 降级渲染 | 见 `02-P`（本批之前完成） |
| **page** | 3（+2 顺带） | 表单 POST → `303 + ?err=`；GET 装载失败 → 降级渲染 | 模板里是**原生 form**（handler 注释写「HTMX 表单提交」，与实现不符）→ 走 PRG |
| **block** | 2 | **JSON**（`pkg/response`），不是 303 | 调用方是工作台 `saveDraft` 的 `r.json()`；纯文本会让解析 reject 落进**连 alert 都没有**的 catch |
| **order** | 3 | 降级渲染 + `LoadFailed` 双档空态 | 三处都是 GET 列表页前置装载失败 |
| **product + inventory** | 3（+4 顺带） | 预览（POST）→ `303 + ?err=`；库存两页（GET）→ 降级渲染 | 预览响应体是新标签页里的 HTML 文档，**没有页壳可保留** |
| **masterdata / contenttemplate / analytics** | 3（+1 顺带） | 降级渲染 | 三处都是只读 GET 页面的 `projects.List` 失败 |
| **runtimefragment**（访问面） | 2 | 保持 `c.String(4xx, …)` 片段语义，**文案受控** | 见 §3 |
| **mail**（访客面） | 2（+1 顺带） | 同上 | 收件人点邮件链接直开裸路由，无 shell 可用 |
| **admin dev_login** | 4 | **判定不改** | debug-only 路由（`assembly.go:385`）、文案已受控、失败时用户未登录无法渲染后台壳 |
| **workbench** | 3（批 2+3） | 2 处换文案来源；1 处纯文本 → JSON | 见 §4 |

**共修 42 处；判定不改 4 处（有据）；另有 5 处「顺带收口」是代理发现任务清单之外的同类缺陷**（都是「同一函数族、同一条件、同一常量，只改一处会造出同页两种形态」）。

## 2. 跨批次冲突：同名 key 的英文值不一致（**并行工作的真实代价**）

两个代理各自发现 `admin.common.list.loadFailed*` 「模板在用但从未登记词条」，各自写进了自己的迁移；
`MsgInternalError` 的 en-US 也被两批同时补。`ON CONFLICT (item_key, lang) DO NOTHING` 保证了
**不报错、不重复**，但**先执行的胜出**（按版本号排序）：

| key | 版本小的批次（胜出） | 被跳过的批次 | 实测最终值 |
|---|---|---|---|
| `MsgInternalError` en-US | 405（block） | 407 | `Internal server error, please try again later` |
| `admin.common.list.loadFailedTitle` en-US | 406（order） | 407 | `This list could not be loaded` |
| `admin.common.list.loadFailedDesc` en-US | 406（order） | 407 | `See the notice at the top for the cause; retry shortly, or use another menu meanwhile.` |

两版英文**语义等价、只是措辞偏好**，zh-CN 值**逐字相同**，因此**不构成缺陷**。
实测证据：`407` 的 SQL 有 7 行，应用时只 `INSERT 0 2`（5 行被跳过），正好对上这张表。
**后续若要统一措辞，改 405/406 的那两行**（407 的行是死代码）。

## 3. runtimefragment：代理推翻了我的任务假设（而我复核后确认它对）

我派任务时说这两处「可能带出表名 / SQL / 文件路径」。代理逐条核了错误产生的全部路径后指出：
`perr` 来自本包 `collectFragmentParams`（三个 `errors.New` 字面量，值域封闭）、
`err` 来自本包 `validateContext` —— **当前都不会泄漏内部结构**。真正的问题是四条：

1. **会回显请求方输入**：`/_fragments/loginPanel?context=<任意串>` 原样出现在响应体
   （`%q` 转义 + `text/plain`，不成 XSS，但「响应内容由请求方裁」破坏了可信边界）；
2. 两处出口**零日志**，参数被拒是静默 400；
3. 这个位置最自然的下一步是接更严格的校验，那一刻 err 就可能带 schema / 表名；
4. 同文件 187 行的渲染错误出口**早已归口**，两种形状并存在同一文件里本身就是隐患。

**修后实测**（公开可达，无需登录）：

```
$ curl '/_fragments/loginPanel?context=SECRET_MARKER_LEAK_PROBE_9137'
HTTP 400  片段语义上下文不合法            ← 不含注入串
$ curl -H 'Accept-Language: en-US' '/_t/u/badtoken_probe'
HTTP 400  This unsubscribe link is invalid or has expired
$ curl '/_fragments/loginPanel?context=probe&lang=en-US'
Invalid fragment semantic context
$ curl '/_fragments/loginPanel?context=probe&lang=<垃圾值>'
片段语义上下文不合法                       ← 兜底链成立，不输出裸 key / 空串
```

**已知边界**（设计如此，非缺陷）：参数失败发生在语言解析**之前**（解析需要 projectId，而参数本身不可信），
所以那一支只认请求显式声明的 `?lang`，**不读 `Accept-Language`** —— 带 `Accept-Language: en-US`
的英文访客在「参数被拒」这一支仍会看到中文。

## 4. workbench：`02-L` §0.7.2 的预分类被我自己的审计推翻

原文把 workbench 51 处**整体**归为「片段/接口出口，非本轮判据」。逐条核后：
**其中 23 处服务的是页面导航**（`/workbench?id=` / `?block=` / `?template=` / `?instance=`），
模板里全部是 `<a href>` 浏览器直达（`pages.html:112`、`dashboard.html:104`、`blocks.html:178/213/250`、
`product_detail_template.html:169`、`product_edit.html:338/340`）—— 它们的纯文本响应**就是脱页壳的页面响应**，
与 project 域同型，是 **P0**。详见 `docs/02-Q-workbench-cstring-audit.md`。

本批只做了**不依赖其它域**的 3 处（批 2 + 批 3）；批 1（23 处页面导航）与批 4（18 处文案规范化）
有顺序依赖 —— `303 + ?err=` 的读侧白名单落在回跳目标页（`/admin/pages`、`/admin/blocks`、`/admin/products`），
那几个文件当时正被其它代理改。

**批 3 的价值大于它 1 行的改动量**：`workbench_instance.go:92` 是 JSON 端点里**唯一**的纯文本分支，
前端 `api.js:95` 的 `r.json()` 对它**抛异常** → `:127` 的 `.catch` 只置 `saveState='error'`、**没有 alert**
（只有 409 重试支路才弹）→ 用户零反馈。改成 `c.JSON` 后走 `:121` 的 `alert(j.message)`。

## 5. 三个门禁盲区（本批实测，都不改动门禁本身）

| # | 门禁 | 盲区 | 证据 |
|---|---|---|---|
| 1 | `check-no-internal-error-leak.sh`（判据） | 候选集是「含 `.Error()` 的行」——**硬编码文案与裸归口 key 从来不在候选里**，本批 42 处里绝大多数它一次都没扫到 | 修前修后它**都是绿的** |
| 2 | 同上（范围） | `TARGETS = find internal/module -type d -path '*/inbound/http'` —— `runtimefragment` 是扁平单包（无 `inbound/http` 子目录），整批漏过 | `grep -c runtimefragment` = 0 |
| 3 | `public/test/enums/unit` | 模块清单**不含 analytics**，所以 `ErrAnalyticsInternal` 缺词条一直没被抓到 | misc 代理实测 |

## 6. 迁移 404~410（已应用 + 已验幂等）

| 编号 | 内容 | 首次应用 | 幂等复跑 |
|---|---|---|---|
| 404 | page 域表单校验（2 对） | `INSERT 0 4` | `0` |
| 405 | block 补 en-US（1 行） | `INSERT 0 2` | `0` |
| 406 | order 装载失败空态（2 对） | `INSERT 0 4` | `0` |
| 407 | 商品详情模板依赖缺失（1 对 + 3 行被跳过） | `INSERT 0 2` | `0` |
| 408 | 三域页面出口（8 对） | `INSERT 0 16` | `0` |
| 409 | 运行时片段出口（5 对） | `INSERT 0 10` | `0` |
| 410 | 邮件访客面（5 对） | `INSERT 0 10` | `0` |

危险语句扫描：7 个文件全部只有 `INSERT ... ON CONFLICT DO NOTHING`（无 DELETE / DROP / TRUNCATE / UPDATE / ALTER）。

**i18n 账本已重算**：`public/test/pkg/i18n/i18n_seed_functional_test.go` 的精确总量断言
`{zh-CN: 3780, en-US: 3665}` → **`{zh-CN: 3863, en-US: 3751}`**（此前停在 3780/3665，
本批 7 个迁移 + 往批 313~317 / 399~402 一起并入；注释里列了各批贡献）。

## 7. 端到端验证（独立 Chrome + 直连 curl，最新二进制）

| 验证点 | 断言 | 结果 |
|---|---|---|
| `/_fragments/…?context=<marker>` | HTTP 400，响应**不含** marker | ✓ |
| `/_t/u/<badtoken>` + `Accept-Language: en-US` | 英文受控文案 | ✓ |
| `/_fragments/…?lang=<垃圾值>` | 兜底不裸 key、不空串 | ✓ |
| `/admin/pages?err=<写侧真实文案>` | 提示条显示该句（白名单命中） | ✓ |
| `/admin/pages?err=<伪造串>` | 落归口文案，**不出现伪造串** | ✓ |
| `/admin/pages?err=<英文文案>`（中文请求） | 落归口 —— 候选按**当前语言**取，与 admin 域同判据 | ✓ |
| `/admin/analytics` / `content-templates` / `masterdata/changes` | 200 + 完整 `</html>`（降级渲染的正常路径） | ✓ |

**注意一个验证陷阱**（我自己先踩了）：读侧未命中时**落归口文案**（`shell.FacingQueryText` 的 fallback），
所以「合法文案」与「伪造串」在**用错测试文案**时会显示同一句话，看起来像"伪造成功了"。
判据必须用**写侧真实产出、且与当前语言一致**的文案，才能区分「命中」与「fallback」。

## 8. 主会话的三处误判与纠正（都写进了任务书，靠代理对抗式核实才暴露）

| 我的判断 | 实测 | 根因 |
|---|---|---|
| `orderenums.ErrInternal` 是「裸归口 key」 | 它是**中文常量** `"操作失败，请稍后重试"`（`order_enums.go:102`）—— 三处真实症状只是脱页壳 | 看名字推断语义，没读值 |
| `analytics.html` 有 26 处提示类槽位 | `rg -c 'role="alert"'` = **1** | 用 class 含 `alert` 等关键词的行数当成了提示槽数量 |
| workbench 51 处整体是「接口出口，P2」 | 其中 23 处是页面导航，P0 | 按「域」一刀切，没逐条看调用方 |

**共同教训**：**grep 到的表象不是语义** —— 常量要看值、类名要看属性、清单要逐条看调用方。

## 9. 遗留（按风险排序）

1. **261 条 enums key 词条有问题**（163 条完全无词条 + 98 条缺 en-US，占 key 形态常量的 66%）——
   `pkg/response/response.go:284` 的 `translate` 未命中时**原样返回 key**，所以这些一旦进响应就是裸 key。
   **是否真进响应需逐域看写侧出口**，建议单开一批。同型实例已确认两个：
   `ErrProjectRequired`（本批随 403 补）、`ErrAnalyticsInternal`（misc 代理补）。
2. **order 域 `?err=` 通道渲染裸 key**：order service 哨兵是 `errors.New(orderenums.ErrXxx)`（值 = key，
   如 `order.err.orderNotFound`），而 `orderFacingError` 命中白名单时**原样返回 `err.Error()`** →
   三页的 `?err=` 会把 `order.err.orderNotFound` 这类 key 渲染进页面。需「写侧翻译 + 读侧译文候选 +
   对账测试」整批（`02-P` §2 的 project 域形态）。
3. **workbench 批 1（23 处）+ 批 4（18 处）** —— 前置条件（回跳页读侧白名单）已在本批落定，可以开。
4. **门禁**：按 §5 的三条扩判据 / 扩范围 / 补模块清单；建议先做 1、2 再开门禁，否则一开就红。
5. `public/test/page/feature`、`public/test/analytics/feature`、`public/test/product/feature` 的
   **既有 build/用例失败**来自工作树里别人未提交的改动（`CompilePreview` 由 5 参变 6 参等），
   本批一律未碰。
