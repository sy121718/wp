# 项目域后台页面「裸文本失败出口」收口批次记录

> 范围：`internal/module/project/inbound/http/`（主题管理 / 主题设置 / 站点设置）。
> 判据来源：`AGENTS.md`「响应与错误处理」（不许直出内部错误的三种形态）、
> `internal/module/admin/inbound/http/admin_err.go`（已收口样板）。
> 索引见 `docs/02-L-admin-rework-worklist.md` §0.7.2 / §0.9。

## 1. 缺陷形态

21 处出口，两类，都不在既有门禁的候选集里（见 §5）：

```go
c.String(http.StatusBadRequest, "缺少主题 id")            // ① 硬编码中文，不 i18n
c.String(http.StatusInternalServerError, "MsgInternalError") // ② 裸归口 key，不翻译
```

浏览器里没有任何页面 —— 侧边栏、页头、表单全部消失，只有一块纯文本。用户既看不到自己
填过什么，除了「后退」也没有动作可做。②更隐蔽：页面显示的是 `MsgInternalError` 这串英文，
运营只会以为后台坏了。

第三处（`buildSiteSettingsData`）是**写了 500 JSON 响应后再 `c.HTML` 渲染同一请求**：
响应头已经发出，浏览器停在 JSON 上，后面那次渲染白做（Gin 打 `headers already written`）。

## 2. 正确形态

**判定表只有一份，两个出口共用**（`project_err.go` 的 `projectErrStatusText`）：

```go
themeError(c, err)        // JSON 出口（/api/theme/*）：状态码给机器读
projectErrParam(c, …)     // 页面出口：译文给人读（?err= / 模板槽）
```

为什么本域不需要 admin 那样的**手写白名单**：project service 的哨兵消息**本来就是 enums 的 key**
（`errors.New(projectenums.ErrThemeNotFound)`），所以「这是不是本域文案」这件事由
`errors.Is` 在编译期锚定，抄错名字直接编译失败。admin 用的形态判定在这里不够用 ——
它只认「像 key 的字符串」。

页面出口的三条判据：

| 场景 | 出口 | 理由 |
|---|---|---|
| 页面表单 POST 失败 | `303` 回来源页 + `?err=<译文>` | 把人送回原处改一处再试；PRG 避免刷新重复提交 |
| 页面 GET 缺参 / 目标不存在 | `303` 回该域列表页 + `?err=` | 本页不知道自己该显示什么，停在原地只能给一块错误页 |
| 列表装载失败 | **降级渲染**（空列表 + 提示，HTTP 200） | 页面结构必须保留：换工程、走别的菜单都还得能用 |

读侧**整体白名单**（`projectPageErrText` → `shell.FacingNotice`）：候选 = 写侧会产出的
全部译文（`projectPageErrKeys` 逐 key `TranslateMessage`）。手拼 `/admin/themes?err=任意文本`
落空串 —— 未命中**不落归口文案**，否则等于给攻击者说的话盖了系统的章。

## 3. 顺带抓到的存量缺陷

**`ErrProjectRequired` 从来没有词条**（`sys_i18n` 计数 0，任何迁移 SQL 都没提到它）。
enums 里早就定义了这个 key，谁把它渲染到页面上都会显示裸 key。本批的 theme 列表页正好要用它
（新建主题缺工程），被 §4 的对账测试当场抓出，随迁移 403 补上中英词条。

## 4. 守卫

| 测试 | 位置 | 守什么 |
|---|---|---|
| 判定表逐哨兵对账（16 case） | `project_err_test.go` | 哨兵 → (状态码, key) 映射错了 = 用户能自己修的问题被说成系统故障 |
| **读侧候选覆盖判定表全部产物** | 同上 | 漏登记一个 key = 写侧发了提示、页面上静默无提示（不报错、不记日志） |
| 读侧拒绝伪造串 | 同上 | 手拼 `?err=` 不得渲染成「系统提示」 |
| 回跳 URL 转义与分隔符 | `project_err_i18n_test.go` | 不转义 → 提示被 `&`/`#` 截断；分隔符错 → `?err=` 变成路径的一部分 |
| 页面提示 key 都在迁移里登记 | 同上 | 键存在但词条没登记 → 页面显示裸 key |
| 模板可渲染（含 `</html>`） | `internal/templates/site_structure_i18n_test.go` | 错误分支缺键会让**整页中断**（HTTP 200 + 后面整块 HTML 消失） |

## 5. 全站摸底：这不是一个「只剩 project 域」的问题

`c.String(http.Status*, …)` 全站 80 处，其中 1 处是 `mail_tracking.go:79` 的
`c.String(http.StatusOK, "<!doctype html>…")`（正常返回 HTML 页面，不是失败出口），
**失败出口（4xx/5xx）共 79 处**。

分类的判据是「该文件注册的是页面路由还是接口/片段路由」—— 两类的目标形态不同，
不能按同一套扳。

### A 类：页面 handler 的失败出口（**真缺陷**，与 project 同型）

**14 处 / 12 个文件**，全部注册 `/admin/*` 页面路由，失败会脱掉页壳：

| 文件 | 行 | 当前文案 | 性质 |
|---|---|---|---|
| `page/inbound/http/pages_handle.go` | 266 / 284 | `"项目名称不能为空"` / `"项目与页面路径不能为空"` | 硬编码中文 |
| `block/inbound/http/block_page_handle.go` | 366 / 372 | `"参数不合法"` / `"全局块不存在"` | 硬编码中文 |
| `product/inbound/http/product_detail_template_page.go` | 334 | `"%s", errTemplateDepsMissing` | 裸常量 |
| `page/inbound/http/site_slot_handle.go` | 100 | `siteSlotInternalText(c)` | 归口函数，但出口仍是 `c.String(500, …)` |
| `order/inbound/http/{order,coupon,return}_page_handle.go` | 153 / 172 / 107 | `orderenums.ErrInternal` | **脱页壳**（该常量是中文文案 `"操作失败，请稍后重试"`，**不是 key** —— 本行原稿写成「裸归口 key」是错的，已由 `02-R` §8 纠正） |
| `product/inventory/inbound/http/inventory_{source,purchase}_page_handle.go` | 61 / 84 | `shell.MsgInternalError` | 裸归口 key（不翻译） |
| `masterdata/inbound/http/masterdata_change_page_handle.go` | 108 | `shell.MsgInternalError` | 同上 |
| `contenttemplate/inbound/http/content_template_handle.go` | 65 | `shell.MsgInternalError` | 同上 |
| `analytics/inbound/http/analytics_page_handle.go` | 59 | `shell.MsgInternalError` | 同上 |

裸归口 key 那一半的症状与 project 域当初一模一样：页面显示 `MsgInternalError` 这串英文。

### B 类：接口 / 片段出口（形态合理，**文案未归口**）

**65 处**，服务 htmx / fetch 消费，形态本身不是缺陷（接口响应就该是片段），
问题是文案未走 `enums`/i18n：

| 域 | 处数 | 说明 |
|---|---|---|
| workbench | 51 | 编辑器 AJAX 端点。已有 `workbench_err.go` 归口助手，但硬编码文案与裸 key 混在一起，**需逐个分类** |
| runtimefragment | 8 | **访问面**（公开可达）。其中 2 处是 `c.String(400, perr.Error())` / `err.Error()` —— **真泄漏，比后台泄漏严重** |
| admin | 4 | `dev_login.go`，开发专用端点（仅环回可达） |
| mail | 2 | `mail_tracking.go` 的退订链接失效提示（访客可见） |

### 既有门禁为什么一直是绿的（两个盲区）


`scripts/check-no-internal-error-leak.sh` 报「✓ 未发现直出内部错误的调用点」，但：

1. **候选集是「含 `.Error()` 的行`**（`grep -F '.Error()'`）—— 硬编码文案（形态 ① 的一半）
   与裸归口 key 根本不在候选里，20+80 处从来没被扫过。
   判据盯的是「err 原文有没有进响应」，而本批的问题是「**响应本身就不是页面**」。
2. **扫描范围是 `internal/module/*/inbound/http`** —— `internal/module/runtimefragment/endpoint.go`
   不在任何 `inbound/http` 子目录下，那 8 处（含 2 处 `.Error()`）整批漏过。

**两条都不能靠加豁免解决，要改判据与范围**；但改判据会立刻命中 80 处存量，
所以正确顺序是：**先分类（哪些是有意协议、哪些是缺陷），再扩门禁**。

## 6. 环境事故与教训（与本批同批发生）

**air 构建成功但不替换运行进程**：8080 上跑的是 17:00 启动的进程（`/proc/<pid>/exe -> tmp/gowp-dev (deleted)`），
air 在 19:51、19:56 都成功产出新二进制，子进程却始终是 17:00 那个
（`/bin/sh -c …/gowp-dev` 包装：kill 掉 sh 后真正的服务进程变孤儿继续占端口）。
`tmp/build-errors.log` 为空说明不是编译失败。最终处置：终止孤儿进程释放端口，
由用户重启 air 恢复热重载。

两条教训：

1. **磁盘热读的模板 + 需重编译的 Go，模板改动必须对旧 handler 安全**。
   本批给 `theme.html` 加了新键 `.Err` 并直接参与判断，而 handler 还是旧的 ——
   `/admin/themes` 立刻 500（`there is no field or method 'Err' in gin.H`）。
   改成 `{{if isset(.Err) && .Err != ""}}`、空态判据由 handler 算好的 `NoProjectEmpty` 决定之后，
   新旧 handler 下都安全（这正是 `internal/templates/CLAUDE.md` 那条规矩的由来）。
2. **`pkill -f 'tmp/gowp-dev'` 会杀掉自己**：当前 shell 的命令行里含这个字符串。
   用 `pgrep -x <进程名>`（精确匹配进程名）或按 pid 处理。

## 7. 验证证据（独立 Chrome + dev-login，非本机 ego 单例）

| 用例 | 断言 | 结果 |
|---|---|---|
| `/admin/themes` | 无提示条、页面完整 | `alertCount=0`、`</html>` ✓ |
| `/admin/themes?err=<伪造串>` | **不渲染** | `alertCount=0` ✓ |
| `/admin/themes?err=主题服务内部错误` | 渲染该句 | `alerts=["主题服务内部错误"]` ✓ |
| POST `/admin/themes/create` 缺工程 | 303 → `?err=工程 ID 不能为空` | 最终 URL 含该参数 ✓ |
| POST `/admin/themes/activate` 缺 id | 303 → `?err=缺少主题 id` | 同上 ✓ |
| POST `/admin/themes/delete` 缺 id | 303 → `?err=缺少主题 id` | 同上 ✓ |
| `/admin/themes/settings?id=…&err=` 合法/伪造 | 显示 / 不显示 | ✓ |
| `/admin/settings?project=…&err=` 合法/伪造 | 显示 / 不显示 | ✓ |

迁移 403：首次应用 `INSERT 0 16`，幂等复跑 `INSERT 0 0`，`ConditionSQL` → 1。

## 8. 建议的后续批次（按风险）

1. **`runtimefragment/endpoint.go` 的 2 处 `.Error()`**（访问面、公开可达）—— 最高的单点风险，改动最小。
2. **门禁扩范围**：把 `internal/module/**` 里所有 handler 目录纳入（不只 `*/inbound/http`），
   先把这 2 处修掉再扩，否则一开就红。
3. **workbench 51 处分类**：区分「AJAX 端点的有意纯文本协议」与「该走归口的失败出口」，
   再决定每类的目标形态（多半是 `workbench_err.go` 里已有的那套，不是页面提示条）。
4. **门禁扩判据**：`c.String(<4xx|5xx>, …)` 的实参是**字面量或 enums key** 时也要报 ——
   与「含 `.Error()`」并列成第三条形态。前提是 1~3 做完。
