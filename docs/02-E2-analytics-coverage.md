# 02-E2 · 自有访问统计与 GA4 的能力对照

> 这份文档回答的是审计 SEO-021 的第一道决策门：**在讨论接 GA4 数据回流之前，先确认自有
> analytics 到底覆盖了什么、缺什么**。审计原文把这一步写成「先确认自有 analytics 能覆盖哪些
> 需求（PV / UV / 热门路径 / 来源域已有）」—— 本文逐项核对了这句话，其中**「来源域已有」
> 不成立**（见 §3）。结论：在补上自有侧的读路径之前，接 GA4 没有明确收益。

## 1. 采集侧：写进去哪些维度

唯一的写入路径是 `POST /analytics/collect`（`internal/module/analytics/inbound/http/analytics_router.go:47`，
公开路由）。它是整个系统里**访客浏览器唯一能写库**的地方，边界写死在架构约束 BIZ-8 里：接口形状只有
「写一条记录」、不参与任何页面渲染（响应恒 204）、失败一律静默。

落库字段（`internal/module/analytics/model/analytics_model.go:20-31`）：

| 列 | 含义 | 读路径 |
|---|---|---|
| `project_id` | 工程 | ✅ 全部查询的必需维度 |
| `path` | 页面路径 | ✅ 汇总与排行都按它聚合 |
| `lang` | 页面语言（构建期烘进产物） | ❌ **只写不读** |
| `session_id` | 会话标识 | ❌ 只写不读 |
| `visitor_hash` | 访客匿名标识（客户端派生 + 服务端加盐哈希） | ✅ UV 去重 |
| `referrer_host` | 来源域 | ❌ **只写不读** |
| `ua_class` | 设备粗分类 | ❌ **只写不读** |
| `ip_hash` | IP 加盐哈希 | ❌ 只写不读（库里没有 IP 明文） |
| `viewed_at` | 浏览时刻 | ✅ 窗口过滤 |

## 2. 读取侧：后台能回答什么

唯一读接口是 `GET /api/analytics/summary`（`analytics_router.go:54`，挂后台三层链），后台页面在
`internal/templates/admin/analytics.html`（91 行）。实际能回答的：

- **PV / UV**：窗口内总浏览数与独立访客数（UV 按 `visitor_hash` 去重，空值不计）。
- **按日趋势**：`daily` 序列（`views` / `visitors`）。
- **热门路径排行**：按 path 聚合的 `views` / `visitors`，带游标深分页（`pathAfterViews` + `pathAfter`）。
- **任意时间范围**：默认最近 30 天，起止日期都留空则回到默认；结束日期不会超过今天。

取数来源会自动切换（响应里的 `source` 字段回显）：窗口含今天就查明细 `page_views`，窗口完全在过去就查
预聚合 `page_views_daily`。日界按 **UTC** 切分 —— 产物由全球访客访问，用 UTC 才没有跨时区口径歧义。

## 3. 核对结论：三个维度采集了但没有任何读路径

`referrer_host`、`ua_class`、`lang` 三者**只有写入点**：

- `internal/module/analytics/service/analytics_collect.go:71`（lang）、`:74`（referrer_host）、`:75`（ua_class）

全仓搜索这三列的非测试引用，结果只有上面这三行写入；汇总任务只按 path 聚合
（`internal/module/analytics/model/analytics_rollup_model.go:73` 的 `GROUP BY path`），没有任何按来源域、
设备分类或语言聚合的查询，后台页面也没有对应区块。

**所以审计那句「来源域已有」需要更正为「来源域在采集，但没有读路径」**。这不是数据模型问题 ——
维度的选择是对的（来源域、设备粗分类、语言正是静态站最需要看的三个切面），缺的是把它们聚合出来
的那一段：要么在汇总表里加 scope 维（`page_views_daily` 的 `scope` 目前只有 `all` / `path` 两种），
要么在读取侧加按维聚合。同时要注意深分页与索引：按来源域聚合的基数远低于 path，
但按 referrer 直接扫明细在窗口较大时不便宜。

## 4. 与 GA4 的定位差异

两者回答的不是同一类问题，这一点比「谁的指标更全」重要：

- 自有 analytics 回答的是：**静态产物被打开了哪些路径、多少次、多少人来、从哪个域来、什么设备来**。
  它的数据主权在自己手里，没有第三方脚本、没有 Cookie 横幅依赖、没有采样。
- GA4 覆盖的是：**访客进入页面之后做了什么** —— 事件级交互（点击 / 滚动 / 表单）、转化漏斗、
  用户属性与受众、跨域会话拼接、以及与广告平台的联动。这些能力需要 Google 侧的脚本与凭据，
  具体口径以 Google 官方文档为准（本文不复制其声明，避免把外部文档的时效性绑进本仓库）。

**判断标准（写在这里避免以后反复讨论）**：只有当需求落在「页面内行为 / 转化 / 受众」这一类、
且自有侧确实无法用片段打点便宜地做出来时，才值得引入 GA4 回流。仅仅是「想在后台看到更多数字」，
先补自有侧的读路径就够 —— 那是纯本地改动，没有凭据、配额与合规面的开销。

## 5. 若要接 GA4 回流，前置条件

1. **凭据管理**：GA4 Data API 需要 service account（或 OAuth）。凭据走环境变量或密钥管理，
   **不落库明文**；后台只存属性 id 之类的非敏感配置。
2. **取数任务**：拉取要落在后台定时任务里（本仓库已有的预聚合任务是同一模式：`RollupRecent` 按小时跑），
   不能进访客请求路径 —— 那会直接违反「访问面不查库、不执行外部调用」的边界。
3. **落库与保留期**：拉回来的指标要自带保留期策略（参照 `analytics_retention.go` 的既有做法），
   否则又多一张只增不减的表。
4. **迁移**：新表按 `docs/schema-snapshot.md` 的口径建（时间列一律 `timestamptz` + `*_time` 命名，
   主键选型按 AGENTS.md 的判据 —— 纯内部流水用 `bigint identity`）。

## 6. 结论

- 自有 analytics 已经能覆盖 **PV / UV / 按日趋势 / 热门路径排行 / 任意窗口查询**，这部分不需要 GA4。
- 审计设想的「来源域已有」不成立：三个采集维度没有读路径，这是**比接 GA4 更该先做的一步**，
  而且是纯本地改动。
- SEO-021 保持 open 的理由从「缺 GA4 凭据」更正为「先补自有侧读路径；GA4 回流取决于是否真的需要
  页面内行为数据」。

## 相关文档

- [02-E-seo-scoring-engine.md](./02-E-seo-scoring-engine.md) — SEO 评分引擎（rubric / 自研计算器 / 执行计划）
- [02-E1-seo-scoring-rules.md](./02-E1-seo-scoring-rules.md) — 评分规则明细
- [04-runtime-and-delivery.md](./04-runtime-and-delivery.md) — 运行时片段与访问面边界
