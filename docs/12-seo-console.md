# 12 · SEO 控制台（后台菜单）分析与实施分解

> 本文**只做分析与分解，不实现**。目标是把 `/home/sky/project/seo` 工作区的 SEO 运营数据
> 做成 go_wp 后台的一个独立菜单（下称「SEO 控制台」）。
>
> 与 `02-E-seo-scoring-engine.md` 的分工：02-E 是**编辑期只读评分器**（在 workbench 侧栏给
> 标题/描述/结构/内容打分，帮人改稿）；本文是**运营数据看板**（GSC/GA4/收录/体检/CLI 报告）。
> 两者共享同一个工作区的资产，但落点不同：一个改稿，一个看数。分层原则一致 —— 都不进构建管线。

## 0. 结论先行

| 问题 | 结论 |
|---|---|
| 值得做吗 | 值得。数据已经按日/周/月稳定落盘，且有一份现成的 Go 看板可直接搬 |
| 数据从哪来 | 三条链路：① 工作区 `monitoring/<date>/json/` 的采集产物（GSC/GA4/收录/Sitemap/Yoast）② `seo` CLI 的结构化报告（4 档位、20+ 报告类型）③ PageSpeed Insights API（Lighthouse lab + CrUX field） |
| 推荐接法 | **搬 seo-dashboard 的 store 层进 go_wp**（同技术栈：Go + Jet v6），采集端 `upload` 改指向 go_wp 新端点 |
| 要不要重写 SEO CLI | **不要**。CLI 的产物已经是落盘 JSON，go_wp 只消费、不实现分析；重写等于长期追一个演进中的 npm 包 |
| 体检怎么做 | go_wp 控制面**异步**调 PSI API（key 存 project settings），落表后展示四类分数 + 核心审计；**绝不进构建产物** |
| 主要成本 | 约 6 人日（见 §9），其中一半是 store 层搬迁与看板页 |

## 1. 现有资产（三处，实测）

### 1.1 seo 工作区 `/home/sky/project/seo`

多站点 SEO 工作区：一个站点 = 一个域名目录（`amigovape.com/`、`uaevape.store/`、`vapo.co.nz/` …）
+ `site.yaml`（业务/市场/语言/连接标识）+ git-ignored 的 `config.local.json`（凭据）。

站点目录内的数据布局：

```text
<domain>/
├── site.yaml                 站点配置（domain / market / 语言 / GSC property / GA4 property）
├── config.local.json         凭据（pagespeed_api_key、WordPress、GSC/GA4 凭据）— 不进 Git
├── monitoring/<date>/json/   逐日采集产物（下面 §2 的绝大多数数据都在这）
│   ├── daily-summary.json    逐日指标 + 收录快照 + Top 页面
│   ├── gsc-data.json         GSC 页面级聚合
│   ├── sitemap-status.json / sitemap-urls.json
│   ├── yoast-tasks.json
│   ├── pagespeed-api-*.json  PSI 原始 payload（mobile / desktop）
│   ├── crawl-results-summary.json
│   └── seo-cli/              SEO CLI 报告（manifest-<cadence>.json + 各报告 JSON）
├── data/index-reason-urls.json  未收录 URL 明细（GSC 浏览器通道采集）
└── changes/ research/ content/ audits/  变更记录、研究、内容、审计
```

跨站数据：`data/seo-activity-log.json`（动作日志）、`data/seo-nz-au-ae-daily-verified-contract.md`
（**口径权威**：UV = GA4 users，曝光 = GSC daily impressions 全站总曝光）。
脚本能力清单：`scripts/SCRIPT-INVENTORY.md`（1015 行，逐脚本说明参数与产物）。

### 1.2 seo-dashboard（现成的 Go 看板，可直接搬）

`/home/sky/project/seo/dashboard/` 是一个**独立单二进制看板**：

- 后端 Go 1.26 + `CloudyKit/jet/v6` 模板（`go:embed`）+ ECharts 5（已本地化，不依赖 CDN）
- 存储 SQLite（`modernc.org/sqlite`，无 CGO）；构建用 GoReleaser
- 两个命令：`serve`（看板 + API）、`upload`（读工作区 JSON 打包上传，Bearer token 鉴权）

**这一点是整份分析里最关键的发现**：它的技术栈与 go_wp 几乎完全一致（Go + Jet v6 + go:embed +
后端渲染 + ECharts），差异只有两个 —— 存储是 SQLite 而非 PostgreSQL，以及它没有鉴权体系。

10 张表（`dashboard/internal/store/types.go` 实测）：

| 表 | 内容 |
|---|---|
| `daily_metrics` | domain + date 逐日 GA4/GSC（sessions/users/pageviews/avg_session_duration + clicks/impressions/ctr/position） |
| `snapshots` | 采集日快照（索引数 / Sitemap / Yoast） |
| `keywords` | 关键词（query + clicks/impressions/ctr/position） |
| `pages` | 页面级聚合 |
| `index_reasons` | 未收录原因 + 计数 + 校验状态 |
| `index_reason_urls` | 原因 → 具体 URL 明细 |
| `top_pages` | Top 页面 |
| `yoast_tasks` | Yoast 待办 |
| `seo_cli_reports` | SEO CLI 报告原文（JSON） |
| `activity_log` | 跨站动作日志 |

11 个查询 API（`/api/domains` `daily` `snapshots` `keywords` `pages` `index-reasons`
`index-reason-urls` `activities` `seo-cli` `seo-cli/report`），
**domain 为空 = 跨站聚合**（CTR/排名按曝光加权）。

看板页（按其 README）：总览 8 个 KPI 卡 + 流量趋势 + 搜索表现 + 收录分布饼图 + 未收录原因柱图
（可点开抽屉看具体 URL）+ 动作日志 + 关键词/页面排行 + SEO CLI 报告抽屉（JSON 语法高亮）。

### 1.3 `seo` CLI（v0.2.8，63 命令 / 7 类）

`/home/sky/.local/share/pnpm/bin/seo`（npm 包，pnpm 全局）。`scripts/lib/seo_cli.py` 是一个
48 行的子进程适配器：`seo <args> --json` → 解析 JSON。

七类（实测 `seo help all`）：

| 类 | 命令 |
|---|---|
| Start here | `start` `report` |
| Projects | `projects list/add` `sites` `doctor` |
| Act on a report | `refresh-priorities` `quick-wins` `second-page` `technical-watch` |
| Agent / power | `report --json` `export diagnose` `mcp install` `skill list` `reports list` |
| Deeper analysis | `decaying` `cannibal` `ctr-underperformers` `query-cluster` `page-opportunities` `content optimize` `perf audit` `internal-links` `community-intent` `ai-referrals` `seo-to-ai-query` |
| Technical / data | `audit-page` `crawl` `agent-readiness` `ai-readiness` `llms audit` `entity-readiness` `okf export` `crawl-queue` `crawl-reports` `rules` `crawl-diff` `index-coverage` `index-watch` `link-recover` `redirect-trace` `gsc-query` `url-inspect` `analytics google properties/report` `updates` |
| Reports / ops | `monthly-report` `report-narrative` `update-postmortem` `schedule cron` `monitoring` `change-log` `tests` `content-groups` `pseo` `auth` `cache` `privacy` `telemetry` `reset` |

**定位判断**：CLI 既是采集器（GSC/GA4 查询、URL Inspection、crawl）也是分析器。工作区的脚本
（`run-seo-cli-suite.py`）把它的输出按档位落盘成 JSON + manifest。因此 go_wp 的正确姿态是
**消费已落盘的报告**，而不是重写 CLI 的能力（§7 给出逐项判断）。

### 1.4 网站体检脚本（用户体验）

`scripts/run-pagespeed-audit.py`（554 行）：调 **Google PageSpeed Insights API** 对单 URL 跑
mobile/desktop，保存原始 payload + 中文 Markdown 摘要。**已放弃本地 Lighthouse CLI** ——
因为 PSI 一次调用同时给 Lighthouse lab 数据与 CrUX field 数据，本地跑还要维护 Chrome 环境。

实测一份产物（`vapecentralau.com/monitoring/2026-08-30/json/pagespeed-api-*.json`）的结构：

```text
payload.categories   → performance 0.95 / accessibility 0.85 / best-practices 0.96 / seo 0.92
payload.audits       → 153 项，每项含 id/title/score/scoreDisplayMode/displayValue/numericValue
payload.loadingExperience → 页面级 CrUX（无样本时为空，脚本写"未采集"）
```

## 2. 能拿到哪些数据（字段级）

### 2.1 逐日流量（GA4 + GSC）

来源：`<domain>/monitoring/<date>/json/daily-summary.json` 的 `series[]`。

```text
series[]: { date, ga4{ sessions, users, pageviews, avg_session_duration_sec },
            gsc{ clicks, impressions, ctr, position },
            keywords[], availability{ ga4, gsc, keywords } }
```

`availability` 是**采集可用性标记** —— 展示时必须区分「值为 0」与「这次没采到」，
否则图表会在缺数据的日子画出真实的 0（这是最容易误导读者的错误）。

### 2.2 收录

- 快照：`indexed / not_indexed / total / report_updated_date`（GSC「网页索引编制」报告）
- 未收录原因：`reason / source / validation / count`
- **URL 级明细**：`<domain>/data/index-reason-urls.json`（浏览器通道采集，
  原因名与 GSC 报告的中文原因一一对应）—— 这是「点柱条看具体是哪些 URL」的数据来源，
  也是看板上最有价值的一层。

### 2.3 关键词与页面

- 关键词：`query / clicks / impressions / ctr / position`（可跨站合并）
- 页面：`gsc-data.json` 的页面级聚合（注意 keys 数组结构，dashboard 的 upload 里有专门处理）
- Top 页面：`pages_top`

### 2.4 Sitemap 与 Yoast

- Sitemap：`valid / child_count / url_count / status / submitted / errors`
- Yoast：待办任务列表（`yoast-tasks.json`）

### 2.5 SEO CLI 深层报告（按档位）

由 `scripts/run-seo-cli-suite.py` 驱动，实测档位与报告映射：

| 档位 | 报告（spec 名 → 语义） |
|---|---|
| daily | `search-performance-overview`（56 天概览 + 近 7 天）、`traffic-anomaly`（90 天异常）、`link-recovery`（失效高价值 URL）、`ai-referrals`（AI 引荐流量） |
| weekly | `quick-wins`（排名 4-10 低 CTR）、`ctr-underperformers`、`striking-distance`、`second-page`（10-20 位）、`decaying-pages`（环比掉点击）、`cannibalisation`（同查询多 URL）、`query-clusters`、`community-intent`、`seo-to-ai-query`、`refresh-priorities`、`internal-links` |
| monthly | `monthly-action-plan`、`monthly-report`、`narrative-report`、`update-correlation`（对照 Google 官方算法更新） |
| technical | crawl 相关（`crawl-queue` / `crawl-diff` / rules 命中） |
| focused | `audit-page`、`redirect-trace`、`page-opportunities`、`content-optimization`、`internal-links`、`performance-audit` |

落盘位置：`monitoring/<date>/json/seo-cli/{manifest-<cadence>.json, <report-id>.json, descriptions/}`。
`manifest` 含 `schema_version / domain / date / cadence / project_id / gsc_site / ga4_property /
reports[]（每项 id + status）/ status / summary{success,failed,total}`。

**注意 `status: partial` 是真实存在的**（实测某日 link-recovery failed）—— 看板必须显示
「哪些报告没跑成」，否则读者会把「失败」当成「没问题」。

### 2.6 网站体检（PageSpeed / Lighthouse / CrUX）

| 维度 | 内容 | 来源 |
|---|---|---|
| 类别分数 | performance / accessibility / best-practices / seo（0-1） | `payload.categories` |
| 核心指标 | FCP / LCP / TBT / CLS / Speed Index / TTI / server-response-time | `payload.audits[<id>].numericValue + displayValue` |
| 资源规模 | total-byte-weight / mainthread-work-breakdown / bootup-time / dom-size | 同上 |
| 可优化项 | unused-javascript / unused-css-rules / uses-optimized-images / render-blocking-resources / uses-long-cache-ttl / modern-image-formats / font-display … | 全部 153 项里 score < 1 的可操作项 |
| SEO 检查 | `is-crawlable / document-title / meta-description / http-status-code / canonical / link-text / hreflang / robots-txt / image-alt` | 与 go_wp 02-E 评分器互补（那里是编辑期静态评判，这里是线上实测） |
| 真实用户 | CrUX `loadingExperience`（overall_category + 第 75 百分位 + 用户分布） | PSI 同一次响应 |

实测 153 项 audits 里，与「用户体验」直接相关的核心项有 24 项命中（上面列出的那批）。
**lab 与 field 必须分开呈现**：Lighthouse 是实验室单次跑分，CrUX 是真实用户 28 天滚动聚合，
把两者混成一个数字是常见的误导来源（PSI 自己的报告也是分开的两块）。

### 2.7 动作日志

`data/seo-activity-log.json`（跨站）→ 看板的「动作日志」页；go_wp 侧可作为「SEO 操作审计」。

## 3. 与 go_wp 的关系

### 3.1 站点映射（要新建一张映射表，不是合并）

go_wp 的 `projects`（站点工程）与 seo 的 `domain` 是**两个不同口径的实体**：
前者是内容/构建的容器（含语言、主题、槽位），后者是线上域名 + 市场 + GSC/GA4 property。
不合并，只映射：

```text
seo_sites: domain(PK) / market / project_id(→ go_wp projects, 可空) / gsc_site / ga4_property / start_url / enabled
```

`project_id` 可空是刻意的：可以先接数据、后绑定工程。

### 3.2 分层与不变量（与 02-E 一致）

- SEO 控制台的数据是**控制面只读运营数据**：**永不进构建管线、永不进访问面产物**
- 它不改变「访客请求不查库」的不变量（后台页当然查库，但那是控制面）
- 与 02-E 的评分器**不共享实现**：评分器是纯函数（规则来自工作区 rubric），控制台是数据展示

## 4. 三个方案对比

| 方案 | 做法 | 成本 | 主要问题 |
|---|---|---|---|
| A · 代理 seo-dashboard | go_wp 加菜单，服务端转发 `/api/*` | 1 天 | 依赖 dashboard 常驻；两套 UI 与两套鉴权；go_wp 侧拿不到数据主权（做不到跨菜单关联） |
| **B · 搬 store 层（推荐）** | 把 `dashboard/internal/store`（10 表 + upsert + query）搬进 go_wp 的一个模块，表改 PostgreSQL；采集端 `upload` 指过来 | 5-6 天 | SQLite→PG 的类型映射要逐列核对；需要迁移与权限点 |
| C · 重写 SEO CLI 能力 | 在 go_wp 内实现 GSC/GA4 查询与各分析报告 | 3-4 周起 | 长期追不上 CLI 演进；GSC/GA4 授权与配额治理要重做一遍；收益低 |

**推荐 B**，理由三条：

1. **同技术栈**：dashboard 已经是 Go + Jet v6 + go:embed，搬过来不是移植而是「换存储」。
   它甚至用了与 go_wp 相同的模板引擎 —— 页面结构可以照着改后端样式即可。
2. **鉴权与权限点统一**：dashboard 只有 Bearer token（上传）+ 无鉴权的查询 API。
   go_wp 有现成的 Session + CSRF + Casbin 三层链与权限点/菜单迁移流程（本次 BIZ 系列刚做过一遍）。
3. **数据主权**：进了 go_wp 才能与站点工程、页面、发布状态关联（「这个页面属于哪个工程、什么时候发布的、
   SEO 表现如何」是 dashboard 永远做不到的事）。

方案 A 仍有价值：作为**过渡**（先接上，验证数据链路），但不作为终态。

## 5. 菜单与权限设计

```text
后台菜单「SEO」
├── 总览         /admin/seo              8 个 KPI 卡 + 流量趋势 + 搜索表现 + 收录分布 + 未收录原因柱图
├── 关键词       /admin/seo/keywords     跨站合并排行（可切换站点/日期范围）
├── 页面         /admin/seo/pages        页面级聚合排行
├── 收录         /admin/seo/index        索引快照 + 原因明细 + 抽屉看 URL 列表
├── 体检         /admin/seo/pagespeed    四类分数 + 核心指标 + 机会清单（mobile/desktop 切换）
├── 报告         /admin/seo/reports      SEO CLI 报告卡片 + JSON 抽屉（按档位/日期筛选）
└── 动作日志     /admin/seo/activities   跨站动作流
```

权限点（沿用既有迁移流程）：

| 权限点 | 用途 |
|---|---|
| `seo:view` | 看板与查询 API（全部 GET） |
| `seo:upload` | 接收采集端上传（机器调用，见 §6.2） |
| `seo:audit` | 触发 PSI 体检（有外部配额成本，单独授权） |
| `seo:manage` | 站点映射与 API key 配置 |

页面路由沿用既有口径：`GET /admin/seo*` 挂 Session + CSRF（不挂 Casbin），写操作走
`builtin.CasbinMiddlewareForPath("/api/seo/...")`。

## 6. 数据接入（推荐方案的落地形态）

### 6.1 表结构（SQLite → PostgreSQL 的映射）

搬 10 张表，类型映射要注意三处：

| SQLite | PostgreSQL | 说明 |
|---|---|---|
| `TEXT` 存 JSON（`seo_cli_reports.payload`、`activity_log.meta`） | `jsonb` | 便于后续按字段检索（也可先存 text，保持与原实现一致） |
| `TEXT` 存日期（`YYYY-MM-DD`） | `date` 或保留 text | 保留 text 更稳（原实现把日期当字符串比较/排序，改类型要同步改全部查询） |
| 浮点（ctr / position） | `numeric` 或 `double precision` | 聚合按曝光加权，用 double 即可；展示层做舍入 |
| 主键（domain+date 复合） | 复合唯一索引 | 原实现靠 upsert 语义，迁移后要显式建唯一约束 |

多站聚合（domain 为空）必须保留 —— 它是看板的核心能力（8 个 KPI 卡就是跨站口径）。

### 6.2 上传协议（采集端不改数据结构）

复用 dashboard 的 `UploadPayload` JSON（`internal/store/types.go` 已定义 18 个字段），
上传端只需把 `--server` 从 dashboard 改成 go_wp：

```text
POST /api/seo/upload            Authorization: Bearer <seo_upload_token>   body ≤ 8 MiB
POST /api/seo/upload/activity   同上
```

新增 `seo_upload_tokens` 表：token 的 sha256 + 备注 + 启用状态（**只写能力**，不可查询数据）。
请求体上限、幂等（`(domain, date)` upsert）、失败返回明确状态码。

### 6.3 看板渲染

ECharts 已在工作区本地化（`dashboard/static/echarts.min.js`），复制进 go_wp 的后台静态资源即可
（注意：**只在 SEO 页加载**，不要进访问面产物 —— 它约 1 MB）。

## 7. SEO CLI 功能拆解（逐项判断）

如果走「方案 C 拆 CLI」的路线，必须逐项判断值不值得。结论：**只有三类值得考虑**。

| CLI 能力 | 是否搬进 go_wp | 理由 |
|---|---|---|
| `report / quick-wins / second-page / decaying / cannibal / query-cluster / internal-links / ctr-underperformers` | ❌ 不搬 | 产物已落盘（`seo-cli/*.json`），搬进 go_wp 等于把同一份分析做两遍且必须跟着 CLI 版本走 |
| `crawl / crawl-queue / crawl-diff / rules` | ❌ 不搬 | 爬虫有独立的运行环境与配额；go_wp 的定位是看数据，不是跑爬虫 |
| `gsc-query / url-inspect / analytics google report` | 🤔 可选（二期） | 这是**采集**能力。若要在 go_wp 里「点一下拉最新数据」，需要搬授权与配额治理 —— 建议先不搬，继续由工作区脚本采集后上传 |
| `perf audit` / PageSpeed | ✅ 值得（见 §8） | 单次调用、语义清晰、与「用户体验」直接对应，且 go_wp 侧有明确展示位置 |
| `sites / projects list / doctor / auth` | ⚠️ 部分 | `sites`（GSC 属性列表）可用于**站点映射的选择器**（拉出来让管理员选，避免手抄 property 字符串） |
| `mcp install / skill list / telemetry / cache / reset / privacy` | ❌ 不搬 | CLI 自身的运维面，与 go_wp 无关 |
| `updates`（Google 官方算法更新列表） | ✅ 值得（二期） | 纯列表数据，用于对照流量波动（`update-correlation` 的解读基础） |

**关键结论**：CLI 的正确用法是「在工作区里跑，把结构化产物上传给 go_wp」，
而不是「在 go_wp 里重新实现 CLI」。这样 CLI 升级时 go_wp 不需要改代码（只要 upload payload 稳定）。

## 8. 体检（用户体验）接入设计

### 8.1 为什么值得单独做

PSI 的数据里有一半是 go_wp 自己算不出来的（实验室渲染 + 真实用户 CrUX），
而且它与 go_wp 的核心价值观直接相关：**访问面产物就是用户实际加载的东西**。
「这个页面在手机上跑多少分、LCP 卡在哪一段」是构建/发布决策的直接输入。

### 8.2 调用约束（决定它是异步的）

| 约束 | 值 | 影响 |
|---|---|---|
| PSI 响应时间 | 10-30s（含排队） | **不能**在页面请求里同步调 → 后台任务 + 轮询/刷新 |
| 免费配额 | 25k 次/天、240 次/分钟（按 IP） | 加限流与缓存（同 URL + strategy 的 TTL，建议 6-24h） |
| 需要 API key | `PAGESPEED_API_KEY`（无 key 也可但配额更低） | key 存 `projects.settings.pagespeedApiKey`，**不进构建产物** |

### 8.3 表与展示

```text
seo_pagespeed_runs: id / project_id / url / strategy(mobile|desktop) / observed_at
                    / score_performance / score_accessibility / score_best_practices / score_seo
                    / metrics jsonb（FCP/LCP/TBT/CLS/SI/TTI/TTFB）
                    / opportunities jsonb（score<1 的可操作审计，按可节省排序）
                    / crux jsonb（loadingExperience + originLoadingExperience）
                    / payload jsonb（原始 payload，事实来源）
                    / lighthouse_version / final_url / http_status
```

页面（`/admin/seo/pagespeed`）：URL 输入（可从工程页面列表选）+ mobile/desktop 切换 +
四类分数卡 + 核心指标条 + **机会清单**（按可节省时间/流量排序）+ CrUX 真实用户块（无样本写「未采集」，
不要显示 0）+ LCP 分解（元素 / 发现方式 / 阶段）。

**必须显式标注 lab / field**：Lighthouse 分数是实验室单次，CrUX 是真实用户 28 天聚合。

### 8.4 与 02-E 的关系

02-E 的评分器评判的是**编辑期可改的东西**（标题长度、关键词密度、内链数、图片 alt…）；
体检评判的是**线上实际的渲染与加载**。前者的输入是 Document，后者的输入是已发布的 URL。
两者都在控制面只读，都不进构建管线。

## 9. 实施分解

| 阶段 | 产出 | 验收 | 估时 |
|---|---|---|---|
| P0 骨架 | 迁移：`seo_sites` / `seo_upload_tokens` + 权限点（4 个）+ 菜单；模块目录 `internal/module/seo/` | 后台能看到 SEO 菜单（空页）；权限点生效 | 0.5 d |
| P1 数据层 | 搬 store 层（10 表 → PG）+ upsert/query 全套 + `POST /api/seo/upload{,/activity}` | 用真实 `upload --days=30` 灌一遍历史数据，行数与 SQLite 版一致 | 1.5 d |
| P2 查询 | `/api/seo/{domains,daily,snapshots,keywords,pages,index-reasons,index-reason-urls,activities,seo-cli,seo-cli/report}` | 与 dashboard 同参数同返回结构（便于对照验证） | 1 d |
| P3 看板页 | 总览（8 KPI + 趋势 + 收录分布 + 未收录原因抽屉）+ 关键词/页面/收录页 | 三视口 + 四输入验收；跨站聚合与单站切换正确 | 1.5 d |
| P4 报告与日志 | SEO CLI 报告卡片 + JSON 抽屉 + 动作日志页 | 能看出 `status: partial` 的失败报告 | 0.5 d |
| P5 体检 | PSI 异步任务 + `seo_pagespeed_runs` + 体检页 + 限流缓存 | 真跑一个 URL，lab/field 分开呈现，配额可控 | 1 d |

合计约 6 人日。P0-P2 完成即可上线（数据能看），P3-P5 增量交付。

## 10. 风险与未决

1. **口径漂移**：`data/seo-nz-au-ae-daily-verified-contract.md` 是口径权威（UV = GA4 users 等）。
   搬 store 层时必须把这套口径连同注释一起带过去，否则两个看板会给出不同数字。
2. **上传端安全**：Bearer token 泄漏 = 数据可被污染（不是提权，但足以让看板不可信）。
   token 只写不读、可轮换、限制 body 大小、按 domain 校验。
3. **SQLite → PG 的类型差异**：原实现的宽松类型（日期当字符串、JSON 当 TEXT）在 PG 下要逐列核对；
   建议**先保持原类型**（text/jsonb 混合），不要趁机重构 schema。
4. **ECharts 体积**：约 1 MB，只在后台 SEO 页加载；确认它不进 `internal/templates/static/js/ui/` 的
   那个「按需内联进产物」的清单（`ui_script.go` 的 `uiBlocks`）。
5. **多站数据的归属**：dashboard 是「多站一板」；go_wp 是多工程模型。映射表可空的设计让两者解耦，
   但要明确**数据可见性**：一个后台账号能否看到全部站点的 SEO 数据？（当前 dashboard 是全可见，
   go_wp 若要按工程收口，需要在 query 层加工程过滤。）
6. **是否保留独立 dashboard**：搬完之后，dashboard 可以退役（少一套运维），也可以留作只读备份。
   建议：数据链路稳定一个发布周期后再退役。
7. **未决**：体检是否要**自动触发**（例如页面发布后自动跑一次）？这会让配额消耗与发布频率挂钩，
   需要先有配额治理（P5 只做手动触发 + 缓存）。
