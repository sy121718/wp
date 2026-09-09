# 06-D · 访问面多语言设计（site i18n）

> **本文为设计提案，尚未实现。** 文中 SQL、结构体、函数签名均为待落地方案，不代表仓库现状；
> 所有「现状」事实都标注了来源文件，便于复核。
>
> 同系列：[06-A-plugin-ecosystem-roadmap.md](./06-A-plugin-ecosystem-roadmap.md)（生态路线图）、
> [06-B-dual-track-adr.md](./06-B-dual-track-adr.md)（双轨制 ADR）、[06-C-htmx-extensions.md](./06-C-htmx-extensions.md)。
> 上游依据：[01-overview.md](./01-overview.md) §1.4（i18n 列为非目标并预留 `/{lang}/{path}`）、
> [03-pipeline.md](./03-pipeline.md) §3/§4/§5（构建管线与 PublicationStore）、
> [06-A-plugin-ecosystem-roadmap.md](./06-A-plugin-ecosystem-roadmap.md) §2.1（SEO 基建「多语言预留 hreflang」）。
>
> 目标：让**生产的静态 HTML 也能切换语言**，同时让后台数据、组件 UI 文案、提示都多语言；
> 并且**实体无关**——不只服务文章，商品与未来实体同样适用。

## 章节索引

| 节 | 标题 | 状态 |
|---|---|---|
| §0 | 现状盘点（2025-09 实测） | 实测 |
| §1 | 问题定义与边界 | 有效 |
| §2 | 架构约束分析 | 有效 |
| §3 | 分层模型 | 有效 |
| §4 | BuildContext 加 `lang` 的改造点清单 | 有效 |
| §5 | URL 结构方案对比 | 已决策（方案 A `/{lang}/path`） |
| §6 | CMS 内容多语言数据模型对比（实体无关） | **历史论证**（被 §7 收敛） |
| §7 | 最终决策：`sys_translation` 内容寻址翻译层 + 翻译工作台 | **最终决策（已拍板）** |
| §8 | 字段级 vs 实体级 | 有效 |
| §9 | 缺语言回退策略 | 有效 |
| §10 | 组件 UI 文案的构建期翻译 | 有效 |
| §11 | 动态能力：Runtime Fragment 按请求语言返回 | 有效 |
| §12 | 与现有后台 i18n 的关系 | 有效 |
| §13 | 分阶段实施建议 | 有效 |
| §14 | 待用户决策点清单 | 待决策 |

## 0. 现状盘点（2025-09 实测）

| # | 能力 | 实测现状 | 位置 | 缺口 |
|---|---|---|---|---|
| 1 | 后台 i18n 机制 | 内存缓存 `key → lang → value` + `http_code`；`LoadCache()` 全量查 `sys_i18n(status=1)`；`auto_refresh` 默认 20s 轮询 | `pkg/i18n/i18n.go`、`cache.go`、`loader.go` | 机制完整，**缺数据** |
| 2 | i18n 数据 | `sys_i18n(item_key, lang, item_value, http_code, status)`，`UNIQUE(item_key, lang)`，**0 行数据** | `public/migrations/init_schema.sql:285-303` | 无 seed、无后台 CRUD 页 |
| 3 | 请求期翻译 | 三形态：纯 key / `key\|param` / `key: detail`；未命中原样返回 | `pkg/response/response.go:139-173` | 仅覆盖 JSON 响应与部分页面 |
| 4 | 请求语言解析 | `?lang` → `Accept-Language` 首段 → 默认语言；`normalizeLang` **只识别 zh-CN / en-US**，其他一律回退默认 | `pkg/response/response.go:88-131` | 语言白名单硬编码 |
| 5 | 后台菜单标题 | `sys_menus.title_key` → `i18n.GetText(titleKey, lang)`，未命中回退 title 原文 | `internal/module/admin/service/menu_authz.go:187-192` | 已 key 化（可作范式） |
| 6 | 模块 enums | `admin` 常量值已是 sys_i18n key；`content` 等模块仍是中文常量 | `internal/module/admin/enums/admin_enums.go:2`、`content/enums/content_enums.go:5-18` | 模块间不一致 |
| 7 | 构建期上下文 | `compileConfig` 11 个维度（content/block/set/plugin/collection/navigation/projectID/currentPath/assetProbe/theme/ctx）**无 lang**；Go 侧**没有 BuildContext 结构体**（概念由 compileConfig + 装配层承担） | `internal/builder/builder.go:133-145` | 语言维度缺失 |
| 8 | 构建期文案 | 面向访客的硬编码中文共 **6 处**：gallery(上一张/下一张)、slider(上一张/下一张)、nav(`aria-label="站点导航"`)、video(`title="视频"`)；另有 2 处中文注释不进产物 | `internal/templates/components/*.jet` | 成本极低，先做 |
| 9 | CMS 内容 | `contents(entity_type, slug, revision, data jsonb)`，`UNIQUE(entity_type, slug)`；字段白名单：product{name,description,price,images,seoTitle,seoDescription} / article{title,body,excerpt,featuredImage,seoTitle,seoDescription} / category{name,description,image} | `public/migrations/042_content.sql`、`internal/module/content/contract/content_service.go:15-19` | 无语言维度 |
| 10 | URL 占用 | `page_routes PRIMARY KEY (project_id, path)`，仅精确路径唯一，**无父子前缀互斥** | `public/migrations/init_builder_schema.sql:178-191` | 多语言前缀会放大父子冲突面 |
| 11 | 激活内核 | `Activate` 会 `MkdirAll` 父目录；symlink 上溯深度按路径层数自动计算，多级路径可用 | `internal/pipeline/publication.go:62-112` | 前缀方案在激活层**无需改内核** |
| 12 | 保留路径 | `/admin /api /_fragments /assets /objects` 不可被 Page 占用；路径上限 500 | `internal/pipeline/url.go:19-29` | 语言码是否保留需决策 |
| 13 | sitemap/robots | 已实现，输入=「已激活路径列表」，激活后刷新；无 `hreflang`、无语言分组 | `internal/seo/sitemap.go`、`internal/module/publication/service/publication_control.go:439` | 需扩展命名空间 |
| 14 | SEO head | `BuildSEOHead` 输出 canonical/OG/Twitter/JSON-LD，**无 hreflang**；面包屑首项硬编码 `"Home"` | `internal/builder/seo_head.go:17-58,127` | 需扩展 |
| 15 | 动态片段 | `Request{Type, Context, Params, UserID}` **无 lang**；端点无语言解析、无 `Vary` | `internal/module/runtimefragment/registry.go:17-26`、`endpoint.go` | 需补 |
| 16 | 依赖追踪 | `DependencyKind` 8 种（direct_content / content_collection / menu / media / content_template / global_component / site_setting / runtime） | `docs/03-pipeline.md` §8.2 | 无 i18n 资源类 |

**结论**：后台 i18n 的**机制已就位、数据为零**；访问面 i18n 的**机制完全缺失**，但接入点（compileConfig、Manifest、PublicationStore、URL 前缀）比预想的浅——激活内核与路由表对 `/{lang}/` 前缀天然友好。

## 1. 问题定义与边界

### 1.1 需要翻译的层（四层 + 一个基建）

| 层 | 内容 | 语言来源 | 翻译时机 | 产物形态 |
|---|---|---|---|---|
| L1 后台界面 | 后台页面、提示、JSON 响应、错误码 | 请求语言 | 请求期 | Jet SSR / JSON |
| L2 组件 UI 文案 | 组件模板内固定串（aria-label、按钮默认文案、空态） | BuildContext.lang | **构建期** | 静态 HTML |
| L3 CMS 内容 | 文章标题/正文、商品名/描述、分类名、SEO 字段 | 内容级 i18n | 构建期 | 静态 HTML |
| L4 动态能力 | Runtime Fragment 返回的 HTML | 请求语言 | 请求期 | HTML 片段 |
| L5 SEO 基建 | hreflang / canonical / sitemap / JSON-LD | BuildContext.lang | 构建期 | 静态文件 |

### 1.2 明确不需要翻译的部分

| 类别 | 字段 / 对象 | 理由 |
|---|---|---|
| 数值与标识 | `product.price`、SKU、订单号、`id`、`entity_type`、`slug`（可选）、`revision` | 语言无关事实；翻译它们会产生 N 份真源 |
| 媒体二进制 | `images`、`featuredImage`、`category.image` 的文件本体与内容哈希 | 文件不可翻译（alt 文本另议，见 §8 决策 D6） |
| 构建元数据 | Artifact hash、manifest 的 schema 版本、依赖 revision | 确定性构建的输入标识 |
| 时间与格式 | 时间戳、货币符号、数字千分位 | **构建期按固定 locale 格式化**（见 §8 决策 D8），不引入请求期分支 |

### 1.3 不在范围

- 访问时翻译（模板、JS、DB 查询）——违反核心不变量，见 §2。
- 客户端 i18n JS（`i18next` 之类）——访客面零 JS 是硬边界（`06-C` §三.5）。
- 机器翻译质量与翻译工作流（可做插件，见 `06-A` §3 第 8 项「翻译工作流可插件」）。

**结论**：翻译必须发生在**构建输入**（BuildContext）与**请求语言**两个明确位置，不允许有第三个位置；语言无关字段一律不进翻译管线。

## 2. 架构约束分析

### 2.1 为什么「运行时客户端翻译」违背不变量

| 不变量 / 约束 | 客户端翻译的后果 |
|---|---|
| 确定性构建（AGENTS.md 不变量 5） | Artifact 字节不含语言差异，**同一份字节在不同浏览器渲染出不同内容**——确定性的字面对象虽指 Artifact，但语义等价性被破坏 |
| 访问面零 JS（AGENTS.md、06-C §三.5） | 必须携带 i18n 运行时 + 全量词条 JSON（或按语言分包），访客面首次加载多出 JS 与一次网络请求 |
| SEO | 爬虫拿到的是**默认语言 DOM**；切换后内容不进索引；`hreflang` 无处可挂（同一 URL 多内容） |
| CDN 缓存 | 同一 URL 随 `Accept-Language` 变化 → 必须 `Vary: Accept-Language`，缓存命中率坍塌 |
| 冻结边界 | `Page Artifact` 被定义为「不可变源码快照」；客户端翻译让产物不再是内容的完整表达 |

### 2.2 为什么 `lang` 应该是 BuildContext 维度

- BuildContext 的定义就是「一次构建的固定数据与依赖 revision」，且**创建后禁止继续查库**（`01-overview.md` §5 冻结边界表）。`lang` 与 `projectID` / `currentPath` / `theme` 同类：它是**同一 Page Document 的构建环境**，不修改文档本身。
- 若 `lang` 是请求状态，则「产物必须随请求变化」→ 访问面必然查库/跑模板 → 直接违反不变量 1。
- 多语言不是「一个文档生成一个多语言产物」，而是「**同一文档在 N 个语言环境下做 N 次独立构建**」，各自独立 hash、独立激活、独立回滚。
- 唯一允许的请求期语言分支是 **Runtime Fragment**（§11）——因为它的输出本来就不进 Artifact。

### 2.3 确定性与 lang 的关系

```text
同一 Page Document + 同一 BuildContext（含 lang）+ 同一 Registry + 同一 Compiler
→ 字节相同的 Artifact（每个语言各一份）
```

需要新增的确定性约束：

1. `lang` 必须**显式进入 Manifest**（否则两个语言的产物可能 hash 相同却内容不同，回滚校验会失效）；
2. 一次构建内 `sys_i18n` 词条必须**冻结为快照**（构建开始时刻的缓存版本），不允许构建中途刷新；
3. 语言回退（§9）的结果必须稳定，且**回退来源要记录**，否则补翻译不会触发重建。

**结论**：`lang ∈ BuildContext`；语言差异必须物化进 Artifact 字节；Fragment 是唯一的请求期例外。

## 3. 分层模型

| 层 | 语言来源 | 解析者 | 翻译函数 / 数据源 | 失败可见性 |
|---|---|---|---|---|
| L1 后台界面 | 请求语言（`?lang` → `Accept-Language`） | `pkg/response.requestLanguage` | `pkg/i18n.GetText(key, lang)`（`sys_i18n`） | 用户可见：未命中回退 key 原文 |
| L2 组件 UI 文案 | `BuildContext.lang` | 构建期注入 | 构建期 `i18n.GetText(key, lang)`（内存缓存，零查库） | 构建期告警 + 回退默认语言（**绝不输出 key**） |
| L3 CMS 内容 | `BuildContext.lang` | 内容解析器 | `contents` / 翻译表（§6） | 构建期告警 + 按 §9 策略 |
| L4 动态能力 | 请求语言（显式 `?lang` + `Vary`） | Fragment 端点 | `i18n.GetText` + 内容级查询 | 回退默认语言 |
| L5 SEO 基建 | `BuildContext.lang` | 构建期 / 激活后刷新 | 语言清单（Project 级）+ 已激活路径 | 构建期校验 |

**结论**：L1 与 L2/L3/L5 共用同一张 `sys_i18n`（不同 key 前缀），L4 复用 L1 的语言解析；只有 L1/L4 是请求期，其余全部构建期。

## 4. BuildContext 加 `lang` 的改造点清单

### 4.1 Go 侧结构

| # | 位置 | 现状 | 改动 | 风险 |
|---|---|---|---|---|
| 1 | `internal/builder/builder.go` `compileConfig` | 11 维度，无 lang | 新增 `lang string`；新增 `WithLanguage(lang string) CompileOption` | 低 |
| 2 | `core.RenderContext` | 组件渲染上下文 | 新增 `Lang string`（模板经 `.Ctx.Lang` 读取，**需实测 Jet 字段路径**） | 低 |
| 3 | `internal/pipeline/publisher.go` `CompileFn` | `func(ctx, pageID, docJSON) ([]byte, error)` | 改为 `func(ctx, BuildInput) ([]byte, error)`，`BuildInput{PageID, Lang, Path, DocJSON}` | 中：调用方 3 处（pipeline/page/presentation） |
| 4 | `pipeline.Manifest` | 10 个字段，无 lang | 新增 `Lang string` 字段（JSON tag `lang`） | 中：**改变产物 hash**（见 D2） |
| 5 | `pipeline.PageRecord` | `Path` 即 URL，单记录/页 | 多语言下需按 `(pageID, lang)` 维度持有状态（或让 `Path` 自带前缀 + 记录按 lang 分片） | 高：状态机改动面 |
| 6 | `internal/module/page/service/page_assemble.go` `compileDocument` | `(ctx, page, projectID, currentPath)` | 加 `lang`，并透传给 `WithLanguage`、导航解析器、内容解析器 | 中 |
| 7 | `internal/module/presentation/service/presentation_service.go` `buildAndPublish` | `(ctx, entityType, entityID, urlPath, templateDoc)` | 加 `lang`，按语言取实体翻译行 | 中 |
| 8 | `pipeline.Publisher.Build/Publish/Rollback/UpdateURL` | 按 pageID | 需明确「按语言分别构建/激活」，或引入 `BuildTarget{PageID, Lang}` | 高 |

### 4.2 产物路径组织

| 对象 | 现状 | 多语言方案 | 是否改内核 |
|---|---|---|---|
| ArtifactStore | `{root}/artifacts/{hash}/index.html` | **不变**：语言差异已进 hash，无需按语言分目录 | 否 |
| PublicationStore | `{activeRoot}/{path}` → symlink | **不变**：`/en-US/about` 就是普通多级路径，`Activate` 已 `MkdirAll` 并自动算 symlink 上溯深度 | 否（需实测） |
| `page_routes` | `PK(project_id, path)` | **不变**：`/about` 与 `/en-US/about` 是两行 | 否 |
| `presentation_instances` | `UNIQUE(entity_type, entity_id)` + `UNIQUE(project_id, url_path)` | **必须改**：同一实体每语言一个实例 → 唯一键需含 lang | 是 |
| Manifest | 无 lang | 新增 `lang` 字段 | 是 |

### 4.3 两条路径的差异

| 环节 | 预览（`CompilePreview`） | 发布（`Publisher.Build`） |
|---|---|---|
| 语言来源 | 请求参数 `lang`（默认取编辑上下文语言） | 构建请求 `lang`（来自 Project 语言清单的循环） |
| 是否写 Artifact | 否 | 是（每语言一份） |
| 是否写 Manifest.lang | 否（无 Manifest） | 是 |
| 是否激活 URL | 否 | 是（`/{lang}/path`） |
| 确定性校验 | 无 | 同语言两次构建字节一致 |

### 4.4 需要实测的坑（重要）

1. **父子路径互斥**：`page_routes` 只做精确路径唯一，**没有前缀互斥检查**；而 `LocalPublicationStore.Activate` 在父路径已被 symlink 占位（指向产物目录）时，子路径的 symlink 会落进**不可变产物目录内部**（`publication.go:106-109` 的注释也承认「父子路径互斥属上游职责」）。若语言首页激活为 `/en-US`，同时存在 `/en-US/about`，两者会互相污染。**需实测 + 需决策**（见 D1'）。
2. **根路径约定**：`relActivePath("/")` → `index`，语言根的对应关系（`/site/en-US/` 指向哪）需实测。
3. **Jet 字段路径**：`RenderContext.Lang` 在组件模板里的访问写法需实测（`{{ .Ctx.Lang }}` 或需经 view 结构体）。

**结论**：改造面集中在 `CompileFn` 签名、`Manifest`、`Publisher` 状态机与 `presentation_instances` 唯一键；ArtifactStore / PublicationStore / `page_routes` **不需要改内核**。

## 5. URL 结构方案对比

| 维度 | A 路径前缀 `/{lang}/path` | A' 默认语言无前缀 + 其他语言带前缀 | B 子域 `en.example.com` | C query `?lang=en` |
|---|---|---|---|---|
| 例 | `/zh-CN/about`、`/en-US/about` | `/about`、`/en-US/about` | `en.example.com/about` | `/about?lang=en` |
| 激活内核改动 | 无（多级路径已支持） | 无 | **需 host 维度**：PublicationStore 现为 path-only，无 host 分区 | 无 |
| 路由占用 | 每语言一行，天然 | 同左 | 需新增 host 字段到 `page_routes` | 无法占用（同一 path） |
| SEO / hreflang | 干净，`hreflang` 指向绝对 URL | 干净，默认语言 URL 最短 | 可（跨域 hreflang），但需域名/证书/CDN 配置 | **不可**：同 URL 多内容，canonical 冲突 |
| 缓存 | 每语言独立 URL，CDN 命中率高 | 同左 | 同左（按 host 分区） | **破坏**：需 `Vary`，命中率坍塌 |
| 默认语言回退 | 显式，无歧义 | `/` 与 `/zh-CN/` 双入口 → 需 301 归一，否则重复内容 | 需 host 归一 | 无 |
| 部署成本 | 零（同域同证书） | 零 | DNS + 证书 + CDN 规则 + 内核改造 | 零但代价最大 |
| 本地静态面 | `/site/en-US/about/` 直接可用 | 同左 | 需多 host 路由 | 不可（静态文件无 query 语义） |

**结论**：选 **A（`/{lang}/path`）**；A' 更「漂亮」但引入双入口与重复内容风险，需额外的 301 归一规则，收益不足以抵消复杂度；B 需要给 PublicationStore 与 `page_routes` 增加 host 维度，属内核级改造，留待「多站点/多域」需求出现时再评估；C 直接否决。

hreflang 的构建期输出（需新增，当前 `BuildSEOHead` 无此能力）：

```html
<link rel="alternate" hreflang="zh-CN" href="https://SITE/zh-CN/about">
<link rel="alternate" hreflang="en-US" href="https://SITE/en-US/about">
<link rel="alternate" hreflang="x-default" href="https://SITE/zh-CN/about">
<link rel="canonical" href="https://SITE/en-US/about">   <!-- 每语言自指，不跨语言 -->
```

sitemap 扩展（`internal/seo/sitemap.go` 需加 `xhtml:link` 命名空间与语言分组）：

```xml
<url>
  <loc>https://SITE/en-US/about</loc>
  <xhtml:link rel="alternate" hreflang="zh-CN" href="https://SITE/zh-CN/about"/>
  <xhtml:link rel="alternate" hreflang="en-US" href="https://SITE/en-US/about"/>
</url>
```

**需实测**：hreflang 需要「同一逻辑页面的全部语言 URL」——构建期如何取得？建议在 Manifest 记录 `translationGroup`（或 `pageId`）后由**激活后刷新**阶段统一生成（与 sitemap 同一时机），避免构建期反查其他语言产物。

## 6. CMS 内容多语言数据模型对比（实体无关，历史论证）

> **注**：本节保留为历史论证（说明「为什么不是 A/B」）。最终落地形态见 §7：**内容寻址单表 `sys_translation`**，
> §6.4 的 `content_translations` 表**已被取代**，不再单独建表。

### 6.1 现状约束（三模型都必须满足）

- `contents(entity_type, slug, revision, data jsonb)`，`UNIQUE(entity_type, slug)`；`entity_type` 当前封闭为 `product / article / category`（未来会加实体）。
- 字段白名单在 `contentcontract.fieldWhitelist` 单点维护，`ResolveString` / Binding 校验 / 集合源共用。
- `presentation_instances` 目前 `UNIQUE(entity_type, entity_id)` + `UNIQUE(project_id, url_path)`。
- 依赖追踪 key 形如 `product:100`，`revision` 为字符串相等比较（`docs/03-pipeline.md` §8.1）。
- 实体无关要求：**任何方案都不能依赖「文章特有字段」**（如 `body` 富文本），必须只依赖「字段名 → 是否可翻译」的元数据。

### 6.2 模型 A：列内 JSON（`data` 存 `{lang: {...}}`）

```jsonc
// contents.data
{
  "zh-CN": { "name": "无线耳机", "description": "…", "price": 399, "images": ["ast_1"] },
  "en-US": { "name": "Wireless Earbuds", "description": "…", "price": 399, "images": ["ast_1"] }
}
```

| 维度 | 评价 |
|---|---|
| 结构化实体适配性 | 中：新实体零迁移，但「语言无关字段」会被复制到每个语言块（`price` 出现 N 份），**价格改一次要写 N 处** |
| 查询与索引成本 | 差：过滤/排序需 `data #>> '{en-US,name}'` 表达式索引；`UNIQUE(entity_type, slug)` 需改为「每语言 slug 唯一」→ 表达式唯一索引 |
| 缺语言回退 | 容易（读时逐级取键），但**回退发生在读取点**，构建期需要额外扫描才能发现缺失 |
| 迁移成本 | 最低：无需新表，仅 `data` 结构变化 + 读取层适配（`entityResolver.ResolveString` 需改） |
| 对 ContentTemplate / PresentationInstance 的影响 | 小：模板不含语言；但 `presentation_instances.UNIQUE(entity_type, entity_id)` 无法表达「每语言一个 URL」→ 仍需加 lang |
| 其他 | 单条 jsonb 随语言数线性膨胀；字段白名单校验需按语言嵌套两层 |

### 6.3 模型 B：每语言一行 + `translation_group`

```sql
ALTER TABLE contents ADD COLUMN translation_group uuid NOT NULL DEFAULT gen_random_uuid();
ALTER TABLE contents ADD COLUMN lang text NOT NULL DEFAULT 'zh-CN';
-- 语言无关字段（price/images）在每行重复存储
DROP INDEX uq_contents_type_slug;
CREATE UNIQUE INDEX uq_contents_group_lang ON contents(translation_group, lang);
CREATE UNIQUE INDEX uq_contents_type_lang_slug ON contents(entity_type, lang, slug);
```

| 维度 | 评价 |
|---|---|
| 结构化实体适配性 | 中：模型简单、行级 CRUD 友好；但语言无关字段（`price`）**物理重复**，与「价格单一真源」冲突 |
| 查询与索引成本 | 好：普通列索引；但**所有查询都必须带 lang**，漏一处就串语言 |
| 缺语言回退 | 需要「找 group 内默认语言那一行」→ 额外查询；缺失可被 `NOT EXISTS` 查询直接发现（优点） |
| 迁移成本 | 中：现有行需回填 group/lang；`revision` 语义要重新定义（每行独立 revision？） |
| 对 ContentTemplate / PresentationInstance 的影响 | **大**：`presentation_instances.UNIQUE(entity_type, entity_id)` 的 `entity_id` 指哪一行？需改为 `(entity_id, lang)`；依赖 key `product:100` 需变 `product:100@en-US` 或 `product:{group}`，**fan-out 全链路改动** |
| 其他 | `price` 多份 → 库存/价格一致性问题，商品场景直接踩雷 |

### 6.4 模型 C：主表 + 翻译表（推荐）

```sql
-- 主表：语言无关身份与字段（价格、SKU、媒体引用、revision）
-- contents 保持不变（entity_type, slug, revision, data jsonb）
--   data 只保留语言无关字段：product.price / product.images / article.featuredImage / category.image

-- 翻译表：语言相关字段（字段名与白名单一一对应）
CREATE TABLE IF NOT EXISTS content_translations (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id    uuid NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    entity_type  text NOT NULL,              -- 冗余：便于按类型建索引与校验
    lang         text NOT NULL,
    data         jsonb NOT NULL DEFAULT '{}'::jsonb,  -- 仅语言相关字段子集
    slug         text NULL,                  -- 可选：本地化 slug（见 D4）
    revision     bigint NOT NULL DEFAULT 1,  -- 翻译自身 revision
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_id, lang)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_content_tr_type_lang_slug
    ON content_translations(entity_type, lang, slug) WHERE slug IS NOT NULL;

-- 实例：每语言一个发布实例
ALTER TABLE presentation_instances ADD COLUMN lang text NOT NULL DEFAULT 'zh-CN';
-- 原 UNIQUE(entity_type, entity_id) → UNIQUE(entity_type, entity_id, lang)
```

| 维度 | 评价 |
|---|---|
| 结构化实体适配性 | **最好**：语言无关字段（价格/SKU/媒体）在主表**单一真源**；语言相关字段按白名单子集入翻译表；新实体只需在字段切分表登记，**零结构变更**（翻译表共用） |
| 查询与索引成本 | 好：`(entity_id, lang)` 唯一索引 + `(entity_type, lang, slug)` 唯一索引；构建期一次 JOIN/两次查询即可，无表达式索引 |
| 缺语言回退 | 好：`LEFT JOIN content_translations ON lang = ?`，缺失即 `NULL`，**可直接统计缺失**并产出构建期告警 |
| 迁移成本 | 中：需把现有 `data` 中语言相关字段搬到翻译表（一次性脚本 + 回滚脚本）；`revision` 主表保留（内容变更聚合），翻译表独立 revision |
| 对 ContentTemplate / PresentationInstance 的影响 | 中：模板不变（模板引用字段名，不引用语言）；`presentation_instances` 加 `lang` 并改唯一键；依赖 key 建议为 `product:100:en-US`（或保留 `product:100` + 依赖表加 lang 列，见 D9） |
| 其他 | 翻译表可承载「未来实体」，无需为每个新实体建表 |

### 6.5 三模型横向对比

| 维度 | A 列内 JSON | B 每语言一行 | C 主表 + 翻译表 |
|---|---|---|---|
| 实体无关（新实体成本） | 零结构变更，但约定隐式 | 零结构变更 | 零结构变更（共用翻译表） |
| 语言无关字段单一真源 | ❌ 复制 N 份 | ❌ 复制 N 份 | ✅ |
| 索引/查询 | 差 | 好（但必须处处带 lang） | 好 |
| 缺语言可检测性 | 弱 | 强 | 强 |
| 迁移成本 | 最低 | 中 | 中 |
| 对 `presentation_instances` 影响 | 仍需加 lang | 需改 entity_id 语义 | 加 lang + 改唯一键 |
| 对依赖 fan-out 影响 | 小 | **大**（key 语义变化） | 中 |
| 后台编辑体验 | 单表单切换语言（简单） | 每语言一条记录（易漂移） | 主表单 + 翻译页签（清晰） |

**结论**：推荐 **模型 C（主表 + 翻译表）**。理由：(1) 价格/SKU/媒体保持单一真源，商品场景不踩雷；(2) 缺语言可被 SQL 直接检出，回退策略可实现为构建期告警；(3) 翻译表可服务未来实体，无需为新实体建表；(4) 对 `presentation_instances` 与依赖追踪的改动可控。模型 A 可作**过渡或原型**，模型 B 因 revision 语义与依赖 key 的全链路改动而**不推荐**。

> **后续收敛（最终决策见 §7）**：模型 C 的「主表 + 翻译表」思路被进一步收敛为**内容寻址单表** `sys_translation`——
> 不再为 CMS 内容单独建 `content_translations`，构建器内联文本与 CMS 内容**共用同一张表**；
> 寻址方式由 `(entity_id, lang)` 改为 `(source_hash, context, lang)`，因此**老数据零迁移、原文不动**。
> 模型 A / B 被否决，理由见本节对比表。

## 7. 最终决策：`sys_translation` 内容寻址翻译层 + 翻译工作台

> **本节为最终决策（已拍板），不再讨论备选方案。** §6 的三模型对比与 §8 的字段级切分保留为历史论证与被否决方案简述，
> 最终落地形态**以本节为准**。原「模型 C：主表 + 翻译表」被收敛为**内容寻址单表**：不再为 CMS 内容单独建
> `content_translations`，构建器内联文本与 CMS 内容**共用同一张 `sys_translation`**。

### 7.1 决策速览（F1–F17）

| # | 决策 | 内容 |
|---|---|---|
| F1 | 原文不动 | Page Document 保持默认语言原文（字符串形态），翻译是**附加层**，不修改 AST、**老数据零迁移** |
| F2 | 存储 | 翻译单独存表 `sys_translation`，**内容寻址** |
| F3 | 主键 | `(source_hash, context, lang)` |
| F4 | 字段 | `source_text`（冗余原文，供工作台对照）、`target_text`、`engine`（manual/ai/po）、`updated_at` |
| F5 | `context` | `组件类型.字段名`，如 `core.button.text`；同文本不同语境可分别翻译 |
| F6 | 可翻译范围 | 组件定义里声明白名单 `Translatable: []string{"text","title","alt"}`；**未声明字段永不翻译** |
| F7 | 跳过规则 | 字段值整体是纯数字或纯符号（`2024`、`→`、`--`）不进翻译表；含数字的句子（`共 42 件`）照常翻译 |
| F8 | 取值 | 有译文用译文，无译文**回退原文**；**不做 draft/confirmed 状态机** |
| F9 | AI 译文 | **直接生效**；用户不满意可覆盖；`engine` 记录来源，工作台给 AI 行打徽章，可筛「只看 AI 翻译的」 |
| F10 | 入口 | 页面列表操作栏加「多语言」按钮（**不做独立菜单**），点进去看该页可翻译内容 |
| F11 | 跨页面复用 | 默认全站同步，界面**必须明确提示**（例：「『了解更多』还用在另外 11 个页面」） |
| F12 | 完成度统计 | 页面列表顶部全站翻译完成度（**可选、后续做**） |
| F13 | 一键 AI | **暂不实现**：按钮灰置 + tooltip「待接入」，接口与 `engine` 字段预留 |
| F14 | 埋点 | 三层：组件声明（白名单）→ 编辑器字段旁标记（已翻译/缺失）→ 构建期扫描 + 缺失统计告警 |
| F15 | 质量防线 | 五道：hash 指纹 / engine 标记 / 构建期缺失告警 / 翻译表快照 / 工作台对照审阅 |
| F16 | CMS 共用 | 商品/文章共用同一张表，只是 `context` 不同（`product.name` / `article.title`）；价格/SKU 等语言无关字段不进表 |
| F17 | 与 `sys_i18n` 分工 | `sys_i18n` 跟代码发布走（i18ngen 生成）；`sys_translation` 跟内容编辑走（工作台管理） |

**结论**：翻译是**原文之外的附加层**，以「内容寻址 + 语境限定」为唯一寻址方式；**不做状态机、不迁移老数据、不建独立菜单**。

### 7.2 与 `sys_i18n` 的分工（决策 F17）

| 维度 | `sys_i18n`（界面文案） | `sys_translation`（内容翻译） |
|---|---|---|
| 管理对象 | 开发者 key（`admin.*` / `site.component.*` / `Err*`） | 编辑器里写下的**用户文本**（构建器内联文本 + CMS 字段） |
| 谁写 | 开发者，随代码发布走 | 内容编辑，随工作台走 |
| 生成方式 | `i18ngen` 从代码/模板抽取，生成 seed/迁移 | 无抽取；构建期扫描发现 + 工作台录入 |
| 寻址 | `item_key` + `lang`（`UNIQUE(item_key, lang)`） | `(source_hash, context, lang)` |
| 改原文后果 | 改 key 即改代码，走发布流程 | 改原文 → hash 变 → 旧译文**自动失效**，进入待重译 |
| 读取时机 | 请求期（后台）+ 构建期冻结快照 | 构建期冻结快照 |
| 缺失行为 | 回退 key 原文（后台可见） | 回退原文（**绝不输出空串**） |

**结论**：两张表、两套生命周期，**不合并**。`sys_i18n` 面向「程序文案」，`sys_translation` 面向「内容文本」；前者跟代码发布，后者跟内容编辑。
**注意区分**：§10 的组件模板固定文案（如 `aria-label="上一张"`、空态提示）属**开发者文案**，走 `sys_i18n`；本节处理的是**编辑器里用户填写的文本**（按钮文字、标题、alt 等），走 `sys_translation`。两者同一个页面共存、互不覆盖。

### 7.3 `sys_translation` PostgreSQL DDL（决策 F2/F3/F4）

```sql
-- 内容寻址翻译表：构建器内联文本与 CMS 内容共用
CREATE TABLE IF NOT EXISTS sys_translation (
    source_hash  text        NOT NULL,                  -- sha256(source_text) 小写十六进制，64 字符
    context      text        NOT NULL,                  -- 语境：组件类型.字段名 / 实体.字段名
    lang         text        NOT NULL,                  -- 目标语言，如 en-US
    source_text  text        NOT NULL,                  -- 冗余原文：供工作台对照、校验 hash
    target_text  text        NOT NULL,                  -- 译文
    engine       text        NOT NULL DEFAULT 'manual', -- manual | ai | po
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_hash, context, lang),
    CONSTRAINT ck_sys_translation_hash   CHECK (source_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT ck_sys_translation_engine CHECK (engine IN ('manual', 'ai', 'po')),
    CONSTRAINT ck_sys_translation_target CHECK (target_text <> '')
);

-- 构建期批量取数走 (source_hash, lang)
CREATE INDEX IF NOT EXISTS idx_sys_translation_hash_lang
    ON sys_translation (source_hash, lang);

-- 工作台按页面/组件浏览
CREATE INDEX IF NOT EXISTS idx_sys_translation_context_lang
    ON sys_translation (context, lang);

-- 工作台「只看 AI 翻译的」筛选
CREATE INDEX IF NOT EXISTS idx_sys_translation_engine_lang
    ON sys_translation (engine, lang);
```

设计要点：

| 点 | 说明 |
|---|---|
| 主键即寻址 | `(source_hash, context, lang)` 三元组唯一；同一文本在**同一语境**下每语言一行 |
| 无代理键 | 不需要自增 id；行本身就是「文本 + 语境 + 语言」的答案 |
| `source_text` 冗余 | 工作台对照与 hash 校验用；写入时校验 `sha256(source_text) = source_hash` |
| 无 `status` 列 | 刻意不做 draft/confirmed（决策 F8） |
| 无 `project_id` | 跨页面复用是**全局**的（决策 F11）；未来要按站点隔离需加列改主键（**待拍板**，见 §14 D12） |
| `engine` 是元数据 | 只用于筛选与审阅，**不参与取值逻辑** |
| 孤儿行 | 改原文后旧 hash 行不再被命中，保留可追溯（清理策略见 §14 D14） |

### 7.4 内容寻址：`source_hash` 的语义（决策 F2）

```text
source_hash = sha256hex(source_text)      # 只对原文做 hash，不含 context、不含 lang
context     = "core.button.text"          # 语境独立成列
```

| 场景 | 行为 |
|---|---|
| 原文改了一个字 | hash 变 → 新 hash 无译文 → **自动进入待重译**（旧行成为孤儿，不再被命中） |
| 同一文本出现在两个组件 | 两个 `context` → **两行**，可分别翻译（页脚「了解更多」与商品 CTA 语气不同） |
| 同一文本、同一 `context`、出现在 12 个页面 | **一行**，12 个页面共用 → 这就是跨页面复用（§7.8） |
| 原文末尾多个空格 | hash 变 → 视为不同文本；写入前统一 `TrimSpace`，避免噪声 |

**结论**：hash 是「原文是否变过」的指纹，`context` 是「语境」的限定；两者**正交，缺一不可**。

### 7.5 `context` 语义与组件 `Translatable` 声明（决策 F5/F6）

`context` 统一为 `{类型}.{字段名}`：

| 来源 | `context` 示例 |
|---|---|
| 构建器组件 | `core.button.text`、`core.image.alt`、`core.heading.text`、`core.card.title` |
| CMS 商品 | `product.name`、`product.description`、`product.seoTitle` |
| CMS 文章 | `article.title`、`article.body`、`article.excerpt`、`article.seoTitle` |
| CMS 分类 | `category.name`、`category.description` |

组件定义新增白名单字段（示意，未实现）：

```go
// 示意（未实现）：组件定义新增 Translatable 白名单
type Definition struct {
    Type         string   // "core.button"
    // …
    Translatable []string // 只有列出的字段参与翻译
}

func (Button) Definition() core.Definition {
    return core.Definition{
        Type:         "core.button",
        Translatable: []string{"text", "title", "alt"},
    }
}

func (Image) Definition() core.Definition {
    return core.Definition{
        Type:         "core.image",
        Translatable: []string{"alt", "caption"},
    }
}
```

规则：

1. **未声明字段永不翻译** —— 防误翻的核心：`link`（`/shop`）、`color`（`#FF0000`）、`width`、`variant`、`iconName` 一律不在白名单内。
2. 白名单在**组件注册期校验**：字段名必须存在于组件 Schema，拼错即注册失败，不静默放过。
3. 白名单是**唯一**的可翻译性来源；构建期不猜、不按「看起来像中文」判断。
4. `context` 由 `组件Type + "." + 字段名` 拼出，**不拼页面 ID** —— 页面维度是「跨页面复用」的表现，不进入寻址。
5. 嵌套字段（同一组件内数组元素的同名字段）的 `context` 命名见 §14 D11（**待拍板**）。

### 7.6 跳过规则：不进翻译表的取值（决策 F7）

| 类别 | 判定 | 例子 | 处理 |
|---|---|---|---|
| 纯数字 | 整体只由数字、正负号、小数点、千分位组成 | `2024`、`42`、`3.14`、`-5`、`1,024` | 跳过 |
| 纯符号 | 整体只由标点、符号、空白组成 | `→`、`--`、`·`、`|`、`…` | 跳过 |
| 空串 / 纯空白 | 去空白后为空 | `""`、`  ` | 跳过 |
| 含数字的句子 | 含数字但也含文字 | `共 42 件`、`2010 年成立` | **照常翻译** |
| 非字符串 | 数字 / 布尔 / 数组 / 对象 | `width: 1200`、`items: [...]` | 跳过（结构不是文本） |

判定用一条正则（示意）：

```go
// 整体只由数字、标点、符号、空白组成 → 跳过
var skipValue = regexp.MustCompile(`^[\p{N}\p{P}\p{S}\s]*$`)

func shouldTranslate(v string) bool {
    s := strings.TrimSpace(v)
    return s != "" && !skipValue.MatchString(s)
}
```

**结论**：跳过是**取值层**的规则；工作台把这类字段显示为「跳过」，且**不计入完成度统计的分母**。

### 7.7 构建期取值逻辑与批量取数（决策 F8）

```text
构建一次（一个页面 + 一个语言）：
1. 遍历 Page Document（或 CMS 实体），按组件 Translatable 白名单收集候选 (context, source_text)
2. 过滤：非字符串 / 空串 / 跳过规则命中 → 丢弃
3. 计算 source_hash = sha256hex(source_text)，得到 hash 集合
4. 一次批量查询取回译文（下方 SQL），在内存建 (source_hash, context) → target_text 索引
5. 逐字段替换：命中 → 用译文；未命中 → 保留原文，缺失计数 +1
6. 缺失计数 > 0 → 产出构建期诊断告警（决策 F14 第三层）
```

批量取数 SQL（构建期唯一查询形态）：

```sql
-- $1 = text[]：本页/本实体所有候选 source_hash
-- $2 = text：目标语言
SELECT source_hash, context, target_text, engine
FROM sys_translation
WHERE source_hash = ANY($1) AND lang = $2;
```

要点：

| 点 | 说明 |
|---|---|
| 为什么按 `source_hash` 过滤而非 `context` | 一个页面的候选 context ≈ 字段数，而 hash 集合一次覆盖所有组件；索引 `(source_hash, lang)` 直接命中 |
| `lang = $2` 的位置 | 主键是 `(source_hash, context, lang)`，查询走 `idx_sys_translation_hash_lang`，回表按 `context` 精确匹配 |
| 多语境命中 | 同一 hash 可能返回多行（不同 `context`）；程序按 `(hash, context)` 精确取 |
| **不跨语境回退** | 目标 `context` 无行、但同一 hash 的其他 `context` 有行 → **仍回退原文**，避免把页脚按钮译法用到商品 CTA |
| 零查库 | 一个页面一次查询，结果在构建内存中复用；组件渲染期不再查库 |
| 快照 | 查询结果在构建开始时**冻结**，构建中途改表不影响本次产物（防线 4） |

```go
// 示意（未实现）：构建期装配
type translationIndex map[string]string // key = source_hash + "\x00" + context

func (r *Resolver) Load(ctx context.Context, lang string, hashes []string) (translationIndex, error) {
    rows, err := r.db.Query(ctx,
        `SELECT source_hash, context, target_text
           FROM sys_translation
          WHERE source_hash = ANY($1) AND lang = $2`, hashes, lang)
    // …
}
```

**结论**：一次 `ANY($1)` 批量查询 + 内存索引 + 未命中回退原文，即可实现「原文不动」的全部翻译行为；Page Document 与 `contents.data` **不发生任何写入**。

### 7.8 翻译工作台（决策 F10/F11/F12）

**入口（决策 F10）**：页面列表的操作栏加「多语言」按钮，**不做独立菜单**。

```text
页面列表
┌────────────────────────────────────────────────────────────────────────────┐
│ 全站翻译完成度  en-US 78%（1,204 / 1,543 条）  ← 可选、后续做（决策 F12）    │
├──────────────┬───────────┬────────┬───────────────────────────────────────┤
│ 标题          │ 路径       │ 状态    │ 操作                                  │
│ 首页          │ /          │ 已发布  │ 编辑  预览  [多语言]  发布  回滚        │
│ 关于我们      │ /about     │ 草稿    │ 编辑  预览  [多语言]  发布  回滚        │
└──────────────┴───────────┴────────┴───────────────────────────────────────┘
                                    ↓ 点击
/admin/page/translations?page_id=PAGE_ID&lang=en-US
```

**工作台界面（示意）**：

```text
┌──────────────────────────────────────────────────────────────────────────────────────┐
│ 多语言 · 关于我们        语言 [ en-US ▾ ]   本页完成度 8 / 11                        │
│                                              [ 一键 AI 翻译 ] ← 灰置，tooltip「待接入」│
├──────────────────────────────────────────────────────────────────────────────────────┤
│ 筛选  [ 全部 ] [ 只看 AI 翻译 ] [ 只看缺失 ] [ 只看人工 ]         [ 保存全部 ] [ 撤销 ]│
├────────────────┬──────────┬──────────────────────┬────────────────────┬─────────────┤
│ 组件 / 字段     │ 类型      │ 原文（默认语言）      │ 译文               │ 来源         │
├────────────────┼──────────┼──────────────────────┼────────────────────┼─────────────┤
│ core.heading    │ text     │ 关于我们             │ About Us           │ [人工]      │
│ core.richText   │ html     │ 我们成立于 2010 年…  │ （待翻译）          │ [缺失]      │
│ core.button     │ text     │ 了解更多             │ Learn more         │ [AI]        │
│                 │          │  ↳ 还用在另外 11 个页面（修改后全站同步生效）        │
│ core.image      │ alt      │ 公司前台             │ —                  │ [跳过]      │
│ core.cta        │ link     │ /shop                │ —                  │ [不翻译]    │
└────────────────┴──────────┴──────────────────────┴────────────────────┴─────────────┘
```

| 界面元素 | 行为 |
|---|---|
| 语言下拉 | 切换 `lang`，刷新本页可翻译清单与完成度 |
| 来源徽章 | `[人工]` / `[AI]` / `[PO]` / `[缺失]` / `[跳过]` / `[不翻译]`（未声明字段） |
| 「只看 AI 翻译」 | 按 `engine = 'ai'` 筛选，集中审阅 AI 产出（决策 F9） |
| 「只看缺失」 | 列出本页未命中的字段，一键定位到编辑器 |
| 保存 | 写入 `sys_translation`，`engine` 记为 `manual`（覆盖 AI 后即变人工） |
| 撤销 | 丢弃本地编辑，不动库 |

**跨页面复用提示（决策 F11，界面必须明确）**：

```text
行内：  ↳ 还用在另外 11 个页面（修改后全站同步生效）
展开：  该文本在 12 个页面出现：首页、关于我们、联系我们、产品中心、…
        共 1 个译文（context = core.button.text，lang = en-US）
```

- 提示**不可省略**：一行译文改一次，12 个页面同时变，必须让用户看见。
- 文案固定为「修改后全站同步生效」，不使用「可能影响其他页面」这类模糊表述。
- 若某文本只在本页出现，则不显示该提示。

**完成度统计（决策 F12，可选、后续做）**：

```text
全站翻译完成度  en-US 78%（1,204 / 1,543 条）
分母 = 全站可翻译字段去重后的 (source_hash, context) 数
分子 = 其中在目标语言已有译文的条数
跳过项（纯数字/符号）不计入分母
```

### 7.9 一键 AI 的预留形态（决策 F13，暂不实现）

按钮（灰置 + tooltip，点击无请求）：

```html
<button type="button" class="btn btn-secondary"
        disabled aria-disabled="true"
        title="待接入">一键 AI 翻译</button>
```

接口预留（只留签名，不实现）：

```go
// 预留（未实现）：POST /api/page/translation/ai
type AITranslateRequest struct {
    PageID   string   `json:"page_id"`
    Lang     string   `json:"lang"`
    Contexts []string `json:"contexts"` // 空 = 当前页全部缺失项
}

type AITranslateResponse struct {
    Items []struct {
        Context    string `json:"context"`
        SourceHash string `json:"source_hash"`
        TargetText string `json:"target_text"`
    } `json:"items"`
}
// 落库时 engine = 'ai'；用户手改后 engine 改回 'manual'
```

| 预留点 | 状态 | 说明 |
|---|---|---|
| `engine` 列 | ✅ 已就位 | 已有 `ai` 取值与 CHECK 约束，接入时无需迁移 |
| 工作台徽章与筛选 | ✅ 已就位 | AI 行天然可标可筛 |
| 接口路径与 DTO | ✅ 签名占位 | 路由不注册、按钮禁用，不产生半成品入口 |
| 批量写入 | ✅ 复用 | 走「保存全部」的同一写入路径 |

**结论**：不做「按钮灰置但接口能调」的中间态；**按钮禁用 + 路由不注册**，只保留数据结构与 DTO。

### 7.10 埋点三层（决策 F14）

| 层 | 位置 | 做什么 | 产出 | 可见性 |
|---|---|---|---|---|
| L1 组件声明 | 组件定义 `Translatable` | 声明白名单，声明即纳入 | 注册期校验（字段必须存在） | 开发者 |
| L2 编辑器标记 | 构建器字段面板 / CMS 编辑表单 | 字段旁显示「已翻译 / 缺失」 | 实时标记（按当前 `lang` 查一次） | 内容编辑 |
| L3 构建期扫描 | Publish Compiler | 扫描全部可翻译字段，统计缺失 | 构建诊断告警（「en-US 缺 3 个字段」） | 后台可见 |

三层**递进**：L1 决定「什么该翻译」，L2 决定「编辑时能不能看见」，L3 决定「上线前有没有兜住」。

### 7.11 数据质量五道防线（决策 F15）

| # | 防线 | 机制 | 挡住什么 | 生效时机 |
|---|---|---|---|---|
| 1 | hash 指纹 | `source_hash = sha256(source_text)` | 改原文后**旧译文自动失效**，进入待重译 | 每次构建 |
| 2 | `engine` 标记 | `manual / ai / po` | 区分来源，AI 产出可筛可审 | 写入时 |
| 3 | 构建期缺失告警 | L3 扫描 + 统计 | 漏译、新增文本未翻译 | 每次构建 |
| 4 | 翻译表快照 | 构建开始时冻结查询结果 | 构建中途改表导致产物抖动、破坏确定性 | 每次构建 |
| 5 | 工作台对照审阅 | 原文/译文并排 + 跨页面提示 | 语义偏差、术语不一致、语气不符 | 人工 |

```text
防线 1 保证「原文变了译文不会继续生效」
防线 2 保证「AI 产出看得见」
防线 3 保证「漏了能发现」
防线 4 保证「构建可复现」
防线 5 保证「翻译质量有人看」
```

### 7.12 风险与缓解：AI 译文直接生效（决策 F9）

**风险（明确承认）**：`engine = 'ai'` 的译文**未经人工确认即参与构建并上线**。

| 风险 | 具体表现 |
|---|---|
| 语义偏差 | 术语译错、多义词选错语境（`Learn more` 在商品页与页脚含义不同） |
| 语气不一致 | 同一站点出现正式/口语混排 |
| 品牌词误译 | 产品名、专有名词被翻译 |
| 回滚粒度粗 | 没有 draft 状态，无法「只回滚某条 AI 译文」（但可按语言回滚整个 Artifact） |

**缓解（已设计，不额外引入状态机）**：

| # | 缓解 | 说明 |
|---|---|---|
| 1 | `engine` 徽章 | 工作台给 AI 行打 `[AI]`，来源永远可见 |
| 2 | 「只看 AI 翻译」筛选 | 集中审阅 AI 产出，可批量覆盖 |
| 3 | 用户可覆盖 | 手改即写回，`engine` 变为 `manual`，覆盖即生效 |
| 4 | 构建期缺失告警 | 兜住**完整性**（**不等于质量**——这点必须在文档与界面上讲清楚，避免误以为告警绿了译文就对了） |
| 5 | 翻译表快照 + Artifact 回滚 | 上线出问题可按语言回滚到上一版产物 |
| 6 | 可选加严开关（**待拍板**） | Project 级「AI 译文需人工确认后才参与构建」；本期不做，留开关位（§14 D13） |

**结论**：AI 直接生效以「**可见、可筛、可覆盖、可回滚**」四件事承担风险，不引入 draft/confirmed 状态机（决策 F8）。

### 7.13 CMS 内容共用同一张表（决策 F16）

| 实体 | `context` | 是否进 `sys_translation` |
|---|---|---|
| 商品 | `product.name` / `product.description` / `product.seoTitle` / `product.seoDescription` | ✅ |
| 商品 | `product.price` / `product.sku` / `product.images` | ❌ 语言无关 |
| 文章 | `article.title` / `article.body` / `article.excerpt` / `article.seoTitle` / `article.seoDescription` | ✅ |
| 文章 | `article.featuredImage` | ❌ |
| 分类 | `category.name` / `category.description` | ✅ |
| 分类 | `category.image` | ❌ |
| 通用 | `id` / `entity_type` / `slug`（默认语言）/ `revision` | ❌ |

- CMS 的**原文同样不动**：默认语言值仍在 `contents.data`，翻译只是附加层。
- 语言无关字段不进表，**杜绝价格/SKU 出现 N 份真源**。
- 与组件共用一张表：`core.button.text` 与 `product.name` 同表共存，靠 `context` 前缀区分，互不干扰。
- 本节**取代 §6.4 的 `content_translations` 表**；§6 的三模型对比保留为历史论证。

### 7.14 明确不做（范围边界）

| 不做 | 理由 |
|---|---|
| draft / confirmed 状态机 | 决策 F8；用 hash + engine + 回退原文替代 |
| 独立「多语言」菜单 | 决策 F10；入口跟随页面操作栏 |
| 一键 AI 实际调用 | 决策 F13；按钮禁用、路由不注册 |
| 修改 Page Document / AST | 决策 F1；原文不动、老数据零迁移 |
| 翻译未声明字段 | 决策 F6；防止 `/shop`、`#FF0000` 被翻译 |
| 请求期翻译 | 沿用 §2 不变量：翻译只在构建期发生（Fragment 例外见 §11） |
| 机器翻译质量评估 | 超出本期范围；由工作台人工审阅承担 |
| 语言无关字段入表 | 决策 F16 |

**结论**：本方案的全部复杂度集中在「内容寻址 + 语境限定 + 构建期批量替换」；除此之外**不新增表、不改 AST、不做状态机、不建独立菜单**。

## 8. 字段级 vs 实体级

| 实体 | 字段 | 是否翻译 | 归属 | 理由 |
|---|---|---|---|---|
| product | `name` | ✅ | 翻译表 | 名称本地化是核心诉求 |
| product | `description` | ✅ | 翻译表 | 同上 |
| product | `seoTitle` / `seoDescription` | ✅ | 翻译表 | 各语言 SERP 独立 |
| product | `price` | ❌ | 主表 | 数值事实；显示格式由构建期 locale 决定（D8） |
| product | `images` | ❌ | 主表 | 媒体二进制；alt 另议（D6） |
| article | `title` | ✅ | 翻译表 | |
| article | `body` | ✅ | 翻译表 | 富文本按语言独立（不是翻译同一份 HTML） |
| article | `excerpt` | ✅ | 翻译表 | |
| article | `seoTitle` / `seoDescription` | ✅ | 翻译表 | |
| article | `featuredImage` | ❌ | 主表 | |
| category | `name` / `description` | ✅ | 翻译表 | |
| category | `image` | ❌ | 主表 | |
| 通用 | `id` / `entity_type` / `revision` / 时间戳 | ❌ | 主表 | 构建输入标识 |
| 通用 | `slug` | ⚠️ 可选 | 主表 + 翻译表 | 默认语言 slug 在主表；本地化 slug 可选（D4） |

**结论**：**字段级**，不是实体级。实体级（整条记录按语言复制）会让价格/SKU/媒体出现 N 份真源，并让 `revision` 语义分裂；字段级切分与现有白名单一一对应，迁移可枚举、可测试。

## 9. 缺语言回退策略

| 策略 | 行为 | 适用场景 | 风险 |
|---|---|---|---|
| S1 回退默认语言 | 取不到目标语言字段时用默认语言值 | 通用内容（营销页、分类描述） | 访客看到混合语言；需在页面标注语言（`<html lang>` 仍为目标语言） |
| S2 隐藏该条目 | 集合源过滤掉缺目标语言的内容 | 列表/商品墙（宁可少展示也不串语言） | 列表数量随语言波动；分页与 SEO 需一致 |
| S3 构建期告警 + 回退 | 构建成功，但产出 diagnostic（「en-US 缺 3 个字段」），后台可见 | **默认策略** | 告警被忽略 → 长期混合语言 |
| S4 阻断发布 | 缺必需语言字段时构建失败 | 合规/资金相关（商品价格说明、法律条款） | 阻断上线；需明确「必需字段清单」 |

组合建议：

```text
默认：S3（告警 + 回退）
列表类集合源：S2（隐藏）
必需字段清单（Project 级配置）：S4（阻断）
```

**关键约束（易漏）**：回退必须在**构建期**完成并**记录回退来源**（例如 manifest 的 `dependencies` 中记录「该字段实际取自 zh-CN」）。否则补齐 en-US 翻译后，依赖 revision 未变化 → **不会触发重建**，站点长期停留在回退内容。

**结论**：默认 S3；回退来源必须进依赖记录，否则「补翻译不重建」是必然 bug。

## 10. 组件 UI 文案的构建期翻译

### 10.1 现状与成本

面向访客的硬编码中文**共 6 处**：

| 文件 | 文案 | 用途 |
|---|---|---|
| `internal/templates/components/gallery.jet` | 上一张 / 下一张 | 轮播箭头 `aria-label` |
| `internal/templates/components/slider.jet` | 上一张 / 下一张 | 轮播箭头 `aria-label` |
| `internal/templates/components/nav.jet` | 站点导航 | `aria-label` |
| `internal/templates/components/video.jet` | 视频 | iframe `title` |

**结论**：现在做成本极低（6 处），且越早做越避免后续组件继续硬编码中文。

### 10.2 两条实现路径

| 方案 | 做法 | 优点 | 缺点 |
|---|---|---|---|
| 10.1 Jet 自定义函数 | `{{ t("site.component.gallery.prev") }}` | 模板内联、直观 | 需实测 Jet v6 `SetCustomFunction` 与转义/unsafe 的交互；模板里出现函数调用，破坏「模板只输出 view 字段」的现状 |
| 10.2 view 预翻译（推荐） | Go 侧 `BuildView` 时 `V.PrevLabel = i18n.GetText(key, lang)`，模板只输出 `{{ .V.PrevLabel }}` | 与现有 Jet 化路径一致（gallery.jet 已用 `.V.Items`）；转义边界不变；零模板语法风险 | 每个组件需加 1~2 个 view 字段 |

```go
// 示意（未实现）：组件 view 预翻译
type GalleryView struct {
    // …
    PrevLabel string // 构建期由 i18n.GetText("site.component.gallery.prev", lang) 填入
    NextLabel string
}
```

### 10.3 key 规范与数据源

| 项 | 约定 |
|---|---|
| key 前缀 | `site.*`（访客面）/ `admin.*`（后台）/ `Err*` `Msg*`（响应消息） |
| 组件文案 key | `site.component.{type}.{prop}`，如 `site.component.gallery.prev` |
| 数据源 | `sys_i18n`（与后台**同一张表**） |
| 读取时机 | 构建期从 `pkg/i18n` 内存缓存读（**零查库**，缓存已由 20s 刷新维护） |
| 缺失行为 | 回退默认语言 + 构建期告警；**绝不把 key 写进产物** |

### 10.4 依赖追踪

组件文案改动后必须重建 → 需要新的依赖类型：

```text
DependencyKind += 'i18n'
dependency_key = "i18n:site"        // 或 "i18n:site.component.gallery"
revision       = sys_i18n 的资源版本号（max(update_time) 或独立计数器）
```

**需实测**：`sys_i18n` 目前无版本字段，取 revision 的方式（`max(update_time)` 是否足够稳定）需验证。

### 10.5 实施记录（P4，2025-09）

实际扫描结果比 §10.1 的 6 处**多 4 处**（原表只统计了模板内字面量，漏了 Go 侧生成的访客可见文案与客户端增强脚本）：

| 文件 | 文案 | 用途 | 类型 |
|---|---|---|---|
| `internal/templates/components/gallery.jet` | 上一张 / 下一张 | 轮播箭头 aria-label | 模板字面量 |
| `internal/templates/components/slider.jet` | 上一张 / 下一张 | 箭头 aria-label | 模板字面量 |
| `internal/templates/components/nav.jet` | 站点导航 | 容器 aria-label | 模板字面量 |
| `internal/templates/components/video.jet` | 视频 | iframe title | 模板字面量 |
| `internal/builder/components/countdown/jet.go` | 天 / 时 / 分 / 秒 | 倒计时单元标签 | Go 生成 |
| `internal/builder/components/form/jet.go` | 提交 | 提交按钮缺省文案 | Go 生成 |
| `internal/builder/components/rating/jet.go` + `rating.go` | 评分 %s / %s | role=img aria-label | Go 生成 |
| `internal/builder/enhance.js` | 第 N 张 | 轮播圆点 aria-label（访客浏览器里创建） | 客户端增强 |

实现要点（与 §10.2 的「view 预翻译」一致，未引入 Jet 自定义函数）：

- 取词函数与兜底链**单点实现**在 `pkg/i18n.Translate` / `TranslateFunc`；后台模板层（`internal/templates.TranslateFunc`）与构建期（`builder`）都委托它，不存在第二套逻辑。
- 构建期注入点：`builder.WithLanguage(lang)` + `builder.WithTranslator(fn)` → `core.RenderContext{Lang, Translate}`；语言未指定时取 `i18n.GetDefaultLang()`（P2 的 `/{lang}/` 产物路径仍属后续阶段，本轮只预留维度）。
- 组件侧契约：`core.I18nAware`（与 `ImageLoadingAware` 同形），渲染层在 `BuildView` 之后统一回填，组件包不依赖 `pkg/i18n`。
- 兜底链：当前语言 → 默认语言 → 组件包内中文原文 → key；绝不报错、绝不 panic、绝不输出空串（`aria-label=""` 属事故，已被测试断言拦住）。
- 客户端增强：圆点标签改成「构建期下发模板 + 客户端替换序号」——`slider.jet` 输出 `data-slide-label="第 %s 张"`（已按语言翻译），`enhance.js` 只做 `%s` 替换，脚本内不再有中文文案。
- 数据：迁移 `060_i18n_seed_site_components.sql`（13 个 key × zh-CN/en-US = 26 行，人工翻译，幂等 ON CONFLICT，`register.go` 按 `site.component.%` 的 zh-CN 行数 13 判定）。
- 产物字节：默认语言下产物与改造前**逐字节一致**（取词兜底 = 原中文），golden 仅在 `slider.html`（新增 `data-slide-label`）与 `document.html`（内嵌 enhance.js 变更）两处有意更新（`-update-jet-golden` / `-update-document-golden`）。
- 未做（后续）：`DependencyKind=i18n` 依赖追踪（§10.4）、构建期冻结快照（§12）、`/{lang}/` 产物与 `page_routes` 多语言行（P2/P3）。

## 11. 动态能力：Runtime Fragment 按请求语言返回

| 环节 | 现状 | 方案 |
|---|---|---|
| 请求结构 | `Request{Type, Context, Params, UserID}` | 新增 `Lang string` |
| 语言解析 | 无 | 端点内解析：`?lang` → `Accept-Language` → 默认语言；**复用** `pkg/response` 的 `normalizeLang`（建议上移到 `pkg/i18n` 供两处共用，避免两套白名单） |
| 参数白名单 | 长度 ≤200、数量 ≤10、context 枚举 | `lang` 需进白名单（校验语言在 Project 语言清单内） |
| 缓存 | 无 `Vary` | 响应必须带 `Vary: Accept-Language`（或用 `?lang` 进缓存键），否则 CDN/浏览器会串语言 |
| 构建期 | Fragment Host 节点不含语言 | 构建期把 `lang` 注入 `hx-get` URL（`/_fragments/product-stock?lang=en-US`），由 `BuildContext.lang` 决定 |
| 处理器 | 返回 HTML 片段 | 处理器内部用 `i18n.GetText(key, lang)` + 语言相关内容查询 |

```html
<!-- 构建期产物（示意） -->
<div hx-get="/_fragments/product-stock?context=currentProduct&lang=en-US"
     hx-trigger="load" hx-swap="innerHTML"></div>
```

**结论**：Fragment 是唯一按请求语言分支的出口；语言必须**显式进 URL 参数**，不允许靠 Cookie/Header 隐式切换（否则 CDN 缓存必然串味）。

## 12. 与现有后台 i18n 的关系

| 问题 | 建议 | 理由 |
|---|---|---|
| 是否共用 `sys_i18n` | **共用** | 避免两套翻译源与两套管理界面；`pkg/i18n` 缓存与刷新机制可直接复用 |
| key 命名空间 | `admin.*` / `site.*` / `Err*` / `Msg*` | 后台与访客面互不干扰，同一表可按前缀筛选 |
| 语言清单来源 | Project 级配置（单一真源） | 后台界面语言、构建语言、Fragment 语言校验都必须来自同一处 |
| 缺失 key 行为 | 后台：回退 key 原文（可见）；构建期：回退默认语言 + 告警（**不可见 key**） | 访客看到 `site.component.gallery.prev` 是事故 |
| 刷新时机 | 后台：20s 自动刷新；构建期：**构建开始时冻结快照** | 构建期读实时缓存会破坏确定性 |
| 管理入口 | 需新增 `sys_i18n` 后台 CRUD（当前无） | 否则改文案要写 SQL（D7） |

**结论**：**一张表、两套读取时机**——请求期读实时缓存，构建期读冻结快照；语言清单与 key 规范必须单点定义。

## 13. 分阶段实施建议

| 阶段 | 范围 | 改动面 | 风险 | 可验证产出 |
|---|---|---|---|---|
| P0 约定与数据 | 语言清单进 Project 配置；`sys_i18n` seed（zh-CN/en-US）；key 规范 | 无代码（或仅配置结构） | 低 | 配置可读、seed 脚本幂等 |
| P1 后台 i18n 落地 | seed 数据 + 各模块 enums key 化 + 后台语言切换 | `sys_i18n` 数据、`enums`、`pkg/response` | 低（机制已就绪） | 后台切 `?lang=en-US` 提示全英文；未命中不出现 key |
| P2 组件文案构建期翻译 | 6 处硬编码 → view 预翻译；`WithLanguage`；`DependencyKind=i18n` | `builder`、4 个组件、`pipeline` 依赖表 | 低 | 同一文档 zh/en 两次构建**字节不同**；同语言两次构建**字节相同** |
| P3 `lang` 进 BuildContext + URL 前缀 | `CompileFn`/`Manifest`/`Publisher`/装配层；`/{lang}/path` 激活；hreflang | 内核 + page service | **中高**（状态机、hash 变化） | `/zh-CN/about` 与 `/en-US/about` 各自 200；hreflang 互指；回滚按语言独立 |
| P4 CMS 内容翻译（模型 C） | 翻译表 + 字段切分 + 解析器 + 回退告警 + `presentation_instances.lang` | content / presentation / contenttemplate | 中高（迁移） | 商品两种语言各自名称/描述；价格单一真源；缺语言构建期告警 |
| P4b 构建器内联文本 + 翻译工作台（§7） | 组件 `Translatable` 白名单 + 构建期批量替换（回退原文）+ 工作台（入口/徽章/筛选/跨页面提示）+ 埋点三层 + 质量五道防线 | 组件定义、`builder`、`page`、dashboard | 中 | 页面操作栏「多语言」可进；改原文后旧译文自动失效；缺失走构建期告警；跨页面复用提示可见 |
| P5 动态能力语言 | Fragment `lang` + `Vary` + 构建期注入 | runtimefragment | 低 | 同一 Fragment 两种语言响应不同；CDN 不串味 |

**每阶段的「不改什么」**：P2 不改 URL 与激活；P3 不改内容模型；P4 不改激活内核；P5 不改构建管线。

## 14. 待用户决策点清单

| # | 决策点 | 选项 | 我的建议 | 影响面 |
|---|---|---|---|---|
| D1 | 默认语言是否带前缀 | A 全带 `/{lang}/`；A' 默认语言无前缀 | **A**（避免双入口与重复内容；实现最简） | URL 结构、301 规则、sitemap |
| D1' | 语言根路径如何激活 | ① 语言首页激活为 `/{lang}`；② 激活为 `/{lang}/index`；③ 语言根不单独占页 | **③ 或 ②**（① 会与 `/{lang}/about` 触发父子 symlink 污染，见 §4.4） | 激活内核、路由占用、首页映射 |
| D2 | `lang` 是否进 Manifest | ① 进（hash 变化，需全量重建）；② 不进（仅靠 canonicalPath 区分） | **① 进**（回滚/校验必须能区分语言，否则不同语言产物可能同 hash） | 全站产物 hash、历史回滚 |
| D3 | CMS 内容模型 | **已拍板（见 §7）**：内容寻址单表 `sys_translation`，与构建器内联文本共用，**不再建** `content_translations` | — | content / presentation |
| D4 | slug 是否本地化 | ① 仅默认语言 slug（`/en-US/about`）；② 每语言 slug（`/en-US/about-us`） | **① 先做**（② 需 slug 映射与 301，可后续） | URL、路由唯一键、SEO |
| D5 | 缺语言严格度 | S1 回退 / S2 隐藏 / S3 告警+回退 / S4 阻断 | **S3 默认 + 必需字段 S4** | 构建期诊断、发布流程 |
| D6 | 媒体 `alt` 是否多语言 | ① 否（`media_asset` 单一 alt）；② 是（新增 `media_translations`） | **② 后续单独做**（可访问性依赖它，但不阻塞主链） | media 模块 |
| D7 | `sys_i18n` 管理入口 | ① 后台 CRUD 页；② 仅 seed/迁移 | **① 后台 CRUD**（否则运维要写 SQL） | dashboard/admin 模块 |
| D8 | 价格/数字格式化归属 | ① 构建期按语言 locale 格式化；② 保留原始数值由前端处理 | **① 构建期**（访客面零 JS） | 组件渲染、确定性 |
| D9 | 依赖 key 是否含语言 | ① `product:100:en-US`；② `product:100` + 依赖表加 `lang` 列 | **② 加列**（key 语义不变，改动更小） | 依赖表、fan-out |
| D10 | 语言清单存放位置 | ① Project settings jsonb；② 独立表 `project_locales` | **② 独立表**（需要顺序、默认标记、启用状态） | project 模块 |
| D11 | 嵌套字段的 `context` 命名（§7 遗留） | ① 不带索引 `core.buttonList.text`；② 带索引 `core.buttonList.items[0].text`；③ 带路径 `core.buttonList.items[].text` | **① 不带索引**（同组件同字段统一语境，复用率最高） | context 生成器、工作台展示 |
| D12 | `sys_translation` 是否加 `project_id`（§7 遗留） | ① 全局单表（本期）；② 按站点隔离 | **① 全局**（跨页面/跨站点复用是本期卖点；多站点需求出现再加列改主键） | 表主键、索引 |
| D13 | AI 译文是否需人工确认才参与构建（§7 遗留） | ① 直接生效（本期）；② Project 级开关：AI 译文需确认 | **① 直接生效**（用徽章+筛选+可覆盖承担风险，见 §7.12） | 构建期取值、工作台 |
| D14 | 改原文后的孤儿译文行如何清理（§7 遗留） | ① 定期清理；② 永久保留（审计） | **② 保留**（可追溯，表体量可控） | 运维、存储 |
| D15 | `engine='po'` 的导入入口（§7 遗留） | ① 后台 .po 导入；② 仅 CLI | **① 后台导入**（与工作台同一处） | dashboard 模块 |
| D16 | CMS 内容翻译的工作台入口（§7 遗留） | ① 沿用页面列表「多语言」按钮（决策 F10 只覆盖构建器）；② CMS 列表各自加「多语言」；③ 统一「内容翻译」页 | **② 各自加**（与 F10 同构，就近原则） | content 模块、页面列表 |

## 15. 实施记录（P2，构建内核加 lang 维度）

> 本节记录 P2 的实际落地结论，供 P3 接手。代码为唯一事实来源，结论均带验证方式。

### 15.1 已落地

| # | 改动 | 位置 | 验证 |
|---|---|---|---|
| 1 | `Manifest.Lang`（D2，`json:"lang,omitempty"`） | `internal/pipeline/artifact.go` | `TestPublisherLangDimension` |
| 2 | `pipeline.BuildInput{PageID,Lang,Path,DocJSON}` + `CompileFn(ctx, BuildInput)` | `internal/pipeline/publisher.go` | 编译期 + 既有 publisher 测试 |
| 3 | `pipeline.Draft{Path,Lang,DocJSON}` + `SaveDraftInput`（`SaveDraft` 保留为无语言等价签名） | `internal/pipeline/publisher.go` | 既有 16 处调用未改 |
| 4 | `PageRecord.Lang`（构建语言随记录冻结） | `internal/pipeline/publisher.go` | `TestPublisherLangDimension` |
| 5 | `LangPath` / `StripLangPath` / `NormalizeLang`（路径映射单点） | `internal/pipeline/lang.go`（新） | `TestLangPathMapping` / `TestLangPathRejects` / `TestStripLangPath` |
| 6 | 激活层祖先符号链接防线 `ensureAncestorsAreDirs` | `internal/pipeline/publication.go` | `TestLocalPublicationRejectsSymlinkAncestor` |
| 7 | 构建期冻结词条快照 `i18n.Snapshot(lang)` | `pkg/i18n/snapshot.go`（新） | `TestSnapshotFreezesCache` / `TestSnapshotFallbackChain` |
| 8 | 文案资源版本号 `i18n.Revision()`（`sys_i18n_revision` → `max(update_time)`） | `pkg/i18n/revision.go`（新） | 编译期 + 人工核对 |
| 9 | `DependencyKind=i18n` + `WithDependencies` 提供者 | `internal/pipeline/artifact.go` / `publisher.go` | `TestPublisherI18nDependency` |
| 10 | 改文案触发重建 `MarkStaleForI18n` | `internal/module/page/{model,service,contract}` | 编译期断言（无调用方，见 15.5） |
| 11 | 装配层全链路传语言（构建/预览/发布/改 URL/导航链接） | `internal/module/page/service/page_{assemble,preview,publish,draft,lang,navigation}.go` | `TestPageLangPrefixFullChain` / `TestPageLangExplicitRequest` |

### 15.2 D1 与 D1 的落地口径

- **D1 全语言带前缀**：`pipeline.LangPath(lang, path)` 是唯一映射点，默认语言同样带前缀。
- **D1 语言根**：采用方案 **② `/{lang}/index`**（§14 D1 的 ②/③ 建议）。
  实测确认 §4.4 硬坑真实存在：父路径 `/zh-CN` 已激活为「指向产物目录的符号链接」后，
  再激活 `/zh-CN/about` 时 `MkdirAll` 会跟随该链接，把符号链接写进 `artifacts/{hash}` 内部
  （不可变产物被污染，且上溯层数错算成悬空链接）。改为 `/{lang}/index` 后语言根与同语言子路径
  是兄弟节点，冲突面消失；激活层再加 `ensureAncestorsAreDirs` 防线，即使误用 `/zh-CN` 也**显式失败**
  而不是污染产物。

### 15.3 落地开关（重要）

配置 `i18n.site_lang_prefix`（默认 **false**，见 `config.yaml` / `pkg/i18n`）：

```text
false（默认）：页面产物路径保持逻辑路径（/about），行为与 P2 前一致，
              Manifest.lang 仍记录默认语言；既有测试与线上 URL 不受影响。
true：全语言带前缀（/zh-CN/about、语言根 /zh-CN/index），决策 D1 的完整形态。
```

开关是灰度闸门，不是「不做 D1」：内核、装配层、路由与激活全链路都已按带前缀实现并有测试覆盖
（`TestPageLangPrefixFullChain` 打开开关跑完整链路）。**建议与 P3 的「语言清单 + 每语言路由行」
一起打开**——单独打开会暴露 15.5 的两处结构性缺口。

### 15.4 对既有产物的影响

- 产物 hash = `SHA256(manifestJSON + "\n" + indexHTML)`（`artifactPayloadHash`）**含 Manifest**，
  因此新增 `lang` 字段会改变所有「带语言构建」的产物 hash；`omitempty` 保证未接入语言的来源
  （presentation 自动发布）Manifest 字节不变。历史产物仍可按旧 hash 回滚。
- 构建期取词从「实时缓存」改为「构建开始时刻的冻结副本」，且**去掉了 `cache.Get` 的
  「遍历所有可用语言」随机兜底**（Go map 迭代顺序随机，会破坏确定性）；未命中一律回退
  组件内中文原文。默认语言（zh-CN）下产物字节与 P4 后一致。

### 15.5 遗留项（P3 前置）

| # | 缺口 | 影响 | 建议 |
|---|---|---|---|
| 1 | ~~`page_artifacts UNIQUE(page_id, version)`~~ | 同一页面同一草稿版本只允许一行产物 → 第二个语言的产物会**替换**第一个语言的行，`page_routes.artifact_id` 指向错内容 | **已修复**（迁移 061 + model/service 加 lang 维度，见 §15.7） |
| 2 | `pages.active_path` 单值 | 一页只能记住一个语言的激活路径，多语言并行激活时旧路径清理失效 | 新增 `page_publications(page_id, lang, active_path, artifact_id)` 或等价结构 |
| 3 | 语言清单（§14 D10） | 没有「站点有哪几种语言」的真源，无法为每语言登记 `page_routes` 行（当前 reserved 行只登记默认语言） | Project 级 `project_locales` 表（顺序 + 默认标记 + 启用状态） |
| 4 | hreflang / sitemap 语言分组 | `BuildSEOHead` 与 `internal/seo/sitemap.go` 未输出 `hreflang` / `xhtml:link` | §5 的构建期输出方案，激活后刷新阶段统一生成 |
| 5 | 站内链接本地化只覆盖导航 | 按钮/图片/文本里的站内链接仍是逻辑路径 | 组件链接属性统一过 `LangPath`（与导航同一函数） |
| 6 | `MarkStaleForI18n` 无调用方 | 后台 i18n CRUD（§14 D7）尚未实现，改文案不会自动触发重建（依赖条目已就位） | 后台 CRUD 保存成功后调用；或加 CLI |
| 7 | Runtime Fragment 语言（P5） | `/_fragments` 请求仍无 `lang`、无 `Vary` | §11 方案，独立阶段 |

### 15.6 验证命令

```bash
go build ./... && go vet ./... && go test ./... -count=1
go test ./public/test/pipeline/unit/ -run "Lang|Publication|Determinism|Dependency" -count=1 -v
go test ./public/test/page/unit/ -run TestPageLang -count=1 -v
go test ./pkg/i18n/ -count=1 -v
```

### 15.7 P3 前置项 1 已修复（page_artifacts 加 lang 维度）

§15.5 第 1 条（同页多语言互相覆盖）已落地，实施范围：

| # | 改动 | 位置 |
|---|---|---|
| 1 | 迁移 061：加 `lang` 列（存量行回填站点默认语言）→ 删旧 `UNIQUE(page_id, version)` → 建 `UNIQUE(page_id, version, lang)`（索引名 `uk_page_artifacts_page_version_lang`，与 gorm 标签同名同形） | `public/migrations/061_page_artifacts_lang.sql` + `register.go`（ConditionSQL 按 lang 列存在判定） |
| 2 | `PageArtifactEntity.Lang` + `GetByPageVersion(pageID, version, lang)`；`GetByHash` 保持无语言（产物 hash 覆盖 Manifest.lang，同 hash 必同语言） | `internal/module/artifact/model/artifact_model.go` |
| 3 | `RecordReq.Lang` / `ArtifactResp.Lang`；service 归一化空语言 → 站点默认语言，替换语义限定在同一语言内 | `internal/module/artifact/{dto,service,contract}` |
| 4 | 装配层调用点补语言：`Build` / `UpdateURL` → `ensureArtifactRow(..., lang)` → `EnsureRecord{Lang}`（跨模块仍只经 `artifact/contract`） | `internal/module/page/service/page_publish.go` |

存量兼容：回填口径为「项目设置 `settings.defaultLang`/`default_lang` → 应用内置 `zh-CN`」（迁移器读不到 `config.yaml`）；
`site_lang_prefix=false` 的无前缀构建路径行为不变（路径仍为逻辑路径，产物行只是多带一个默认语言值）。

验证：`TestPageArtifactsLangMigration{BackfillsExistingRows,Idempotent,UsesProjectDefaultLang}`、
`TestMigrationsRunTwiceIsIdempotent`（全量迁移重复执行不报错）、
`TestArtifactEnsureRecordSamePageTwoLangsCoexist`、`TestArtifactEnsureRecordSameLangSameVersionReplaces`、
`TestArtifactEnsureRecordEmptyLangFallsBackToDefault`、`TestArtifactGetByPageVersionLangScoped`、
`TestPageArtifactLangCoexistAndRouteBinding`（端到端：两语言各一行 + 路由 artifact_id 指向各自语言产物）。

仍属 P3：§15.5 第 2 条（`pages.active_path` 单值，实测 `Publish(en-US)` 会取消 `/zh-CN/about` 的激活路由）、
第 3 条（`project_locales` 语言清单）——两条均已随 P3 落地，见 §15.8。

### 15.8 P3 已落地（站点多语言上线，2025-09）

P3 目标：解掉 `pages.active_path` 单值，让「一页多语言同时在线」成立，并补上语言清单、灰度开关双路径与 SEO 语言标注。
代码为唯一事实来源；本节记录改动位置与验证方式。

| # | 改动 | 位置 | 验证 |
|---|---|---|---|
| 1 | 迁移 062 `page_publications(page_id, lang, active_path, artifact_id, artifact_hash, published_at, updated_at)`：每语言激活状态真源；存量 `pages.active_path` 回填默认语言一行 | `public/migrations/062_page_publications.sql` + `register.go` | `TestPagePublicationsLanguageScopedLifecycle`、`TestMigrationsRunTwiceIsIdempotent` |
| 2 | 迁移 063 `page_stagings(page_id, lang, artifact_id, artifact_hash, draft_version, updated_at)`：每语言暂存指针（解「先构建两语言再逐个发布」失败） | `public/migrations/063_page_stagings.sql` + `register.go` | `TestPageStagingsPerLanguageIndependent` |
| 3 | 迁移 064 `project_locales(project_id, lang, sort_order, is_default, enabled)` + 部分唯一索引 `uq_project_locales_default` | `public/migrations/064_project_locales.sql` + `register.go` | `TestProjectLocalesSaveAndList` / `TestProjectLocalesValidation` |
| 4 | page model：`PublicationEntity`/`StagingEntity`、`MarkPublishedLang`（upsert + pages 单值镜像同事务）、`MovePublicationPath`、`MarkStagedLang`、软删同清两表 | `internal/module/page/model/page_publication_model.go`、`page_model.go` | `TestPagePublications*` 全组 |
| 5 | Publish / Rollback / UpdateURL 按语言作用域：旧路径取 `page_publications` 本语言行（`publishedPathOf`），暂存产物取 `page_stagings` 本语言行（`stagedArtifactOf`，跨语言暂存不互认） | `internal/module/page/service/page_publish.go` | `TestPagePublicationsLanguageScopedLifecycle`、`TestPageStagingsPerLanguageIndependent` |
| 6 | `RenameReservedReq.OnlyReserved`：改某语言 URL 时其他语言**只迁 reserved 行**，绝不动他人 active 行 | `publication/{dto,service}`、`page_lang.go` | `TestPagePublicationsLanguageScopedLifecycle`（第 5 段） |
| 7 | 路由登记按 `project_locales` 逐语言：建页 `reservePath`、改草稿/改 URL `renameReservedAllLangs`（失败回迁） | `page_draft.go`、`page_lang.go` | `TestPageRoutesRegisteredPerLocale` |
| 8 | project 模块语言清单：`ListLocales` / `EnabledLangs`（无清单回退默认语言一种）/ `DefaultLocale` / `SaveLocales`（至少一种、至多一个默认且默认必须启用、语言码白名单） | `internal/module/project/{model,dto,service,contract}` | `TestProjectLocales*` |
| 9 | 产物 head 输出 hreflang 互指（`builder.Alternate` + `WithAlternates` + `BuildSEOHead` 第五参）：语言码升序、`x-default` 固定最后，<2 语言不输出（单语言字节不变） | `internal/builder/{seo_head.go,builder.go}`、`page_assemble.go` | `TestBuildSEOHeadAlternates`、`TestPageArtifactHreflangPerLanguage` |
| 10 | sitemap 按语言分组：`SitemapEntry.Alternates` + `xhtml:link` + 按需声明 `xmlns:xhtml`；`RefreshSiteFiles` 增加 `langs/defaultLang` 参数（语言清单由装配层传入，publication 不跨模块查语言） | `internal/seo/sitemap.go`、`publication_control.go`、`publication/contract` | `TestBuildSitemapAlternates`、`TestPageSitemapGroupedByLanguage` |
| 11 | 导航「当前项」高亮修复：`pageContextOf(ctx, pageID, lang)` 返回**逻辑路径**（此前取已带前缀的 active_path 再加前缀 → `/zh-CN/zh-CN/about`，高亮永不命中） | `page_navigation.go`、`page_assemble.go` | 既有导航测试 + `TestPageBilingualSiteOnline` |
| 12 | `PageResp.Publications`（每语言激活状态投影）与 `RollbackReq.Lang` | `internal/module/page/dto` | `TestPagePublicationsLanguageScopedLifecycle` |

**语义口径（重要）**

- `page_publications` 是每语言激活状态的**真源**；`pages.active_path` / `active_artifact_id` / `published_at` 退化为「最近发布语言的单值镜像」，仅供既有单值读取方（导航来源候选、列表投影）使用。
- `page_stagings` 是每语言暂存指针的真源；`pages.staged_artifact_id` 同理为镜像。跨语言暂存**不互认**：本语言无暂存行且镜像产物语言不符时返回「无暂存产物」，绝不跨语言发布。
- 灰度开关 `i18n.site_lang_prefix` 默认 **false**：关闭时两种语言映射到同一逻辑路径（路由只有一行、后发布者覆盖线上内容），「一页多语言同时在线」必须显式开启（`TestPagePublicationsGateOffKeepsSinglePath` 记录该口径）。
- 语言清单不可读（表缺失/查询失败）时 `EnabledLangs` 回退「站点默认语言一种」，单语言站点行为与 P3 之前逐字节一致。

**端到端证据**（`TestPageBilingualSiteOnline`，真实 PG + 真实 FS + gin 静态面）：同一页 `/about` 两语言同时在线：

```text
路由行: path=/en-US/about kind=active artifact_id=defbb8d0-...
路由行: path=/zh-CN/about kind=active artifact_id=49c035ac-...
激活状态: lang=en-US active_path=/en-US/about artifact_hash=79d3a74e...
激活状态: lang=zh-CN active_path=/zh-CN/about artifact_hash=8965d536...
激活链接: .../public/active/zh-CN/about -> ../../../artifacts/8965d536...
激活链接: .../public/active/en-US/about -> ../../../artifacts/79d3a74e...
HTTP GET /site/zh-CN/about/ -> 200, 8732 bytes
HTTP GET /site/en-US/about/ -> 200, 8732 bytes
确定性: zh 产物 8732 字节，两次构建字节一致（hash=8965d536...）
```

**仍属后续**

- ~~`project_locales` 的后台 CRUD 入口与语言切换器 UI~~ **已落地，见 §15.9**。
- 站内链接本地化仍只覆盖导航（§15.5 第 5 条）：导航来源候选（`navigation/outbound/source`）读 `pages.active_path` 镜像，多语言下取「最近发布语言」的路径。
- 禁用某语言后其已激活路由不会自动清理（无 UI 触发，本阶段不处理）。
- sitemap 的 `<loc>` 与 hreflang 在未配置 `WP_SITE_BASE_URL` 时输出站点内路径（既有行为）。

### 15.9 语言切换器 UI + 后台语言清单管理已落地（2025-09）

§15.8「仍属后续」第 1 条（前台切换器 + 后台清单 CRUD）已落地。代码为唯一事实来源，结论均带验证方式。

| # | 改动 | 位置 | 验证 |
|---|---|---|---|
| 1 | `core.languages` 组件（独立组件形态，判断依据见下） | `internal/builder/components/languages/{languages,jet}.go` + `internal/templates/components/languages.jet` | `TestLanguagesSwitcherRendersLinks` 等 7 个用例 |
| 2 | `core.LocaleLink` + `RenderContext.Locales` + `builder.WithLocaleLinks` | `internal/builder/core/render.go`、`internal/builder/builder.go` | 编译期 + 组件测试 |
| 3 | 装配层一次计算、两处消费：hreflang 互指 + 切换器链接（同一份 `siteRouteEntries`） | `internal/module/page/service/page_assemble.go`（`localeViewOf` 取代 `alternatesOf`） | `TestPageArtifactLanguageSwitcher`、既有 `TestPageArtifactHreflangPerLanguage` |
| 4 | `<html lang>` 跟随构建语言（此前硬编码 `zh-CN`，en-US 产物自称中文） | `internal/builder/{builder.go,document.jet}`（`CompiledPage.Lang` → `documentView.Lang`，空回退默认语言） | `TestRenderDocumentLangAttribute`、`TestRenderDocumentGolden`（默认语言字节不变） |
| 5 | 切换器容器无障碍标签词条 seed | `public/migrations/065_i18n_seed_language_switcher.sql` + `register.go`（ConditionSQL 按 `site.component.languages.%` 的 zh-CN 行数判定） | `TestMigrationsRunTwiceIsIdempotent` |
| 6 | 组件库入口（工作台可拖入） | `internal/templates/static/js/workbench/palette.js` | `TestPaletteGroupsCoverItems` / `TestPaletteInsertNodesValidate` |
| 7 | 后台语言清单管理（站点设置「语言」分组） | `internal/module/dashboard/inbound/http/{site_settings_handle,site_locales_handle,dashboard_router}.go`、`internal/templates/admin/{settings.html,partials/locale_rows.html}` | `TestSiteSettingsRendersLocaleGroup` / `TestLocaleRowsFragmentAddAndRemove` / `TestSaveSiteLocalesPersists` / `TestSaveSiteLocalesValidation` |

**前台切换器为什么是独立组件**（而不是 document.jet 全局注入 / nav 的一个选项）：

1. 访问面所有可见 UI 都由组件产生，`document.jet` 只是骨架（head + body 包裹）；把可见结构塞进骨架会打破「body 内容 = 文档编译产物」的语义，且骨架没有组件 CSS 通道（无法表达位置、可见性开关与样式）；
2. 导航（`core.nav`）的职责是站点菜单，语言切换不是菜单项；混入会让 navigation 模块承担非菜单职责，且只在「放了导航的页面」生效；
3. 独立组件可放进页眉全局块（block 模块 global 引用）→ 全站一次放置即生效，这是站点级复用的既有机制；
4. 组件天然受益于「同一文档、多语言各自一份产物」：文档只维护一份，构建期按当前语言渲染当前项标记与各语言链接。

**产物形态**（真实构建输出，`TestPageArtifactLanguageSwitcher` 断言原文）：

```html
<nav class="wp-c-lang1 wp-lang" aria-label="语言">
  <ul class="wp-lang-list">
    <li class="wp-lang-item is-current"><span class="wp-lang-current" lang="zh-CN" aria-current="true">简体中文</span></li>
    <li class="wp-lang-item"><a class="wp-lang-link" href="/en-US/about" hreflang="en-US" lang="en-US">English</a></li>
  </ul>
</nav>
```

- 当前语言：`<span aria-current="true">`，不可点（不输出指向自身的 `<a>`）；其他语言：`<a hreflang lang>`。
- **零 JS**：产物里没有任何跳转脚本（测试断言无 `onclick` / `location.href=`）。
- 展示名用「语言自称」（简体中文 / English / 日本語 …，`languages.Endonym`，未收录回退语言码）。

**缺语言回退策略（§9）选 S2「隐藏」**，理由：

1. 静态访问面没有运行时回退（`/site` 是 `http.FileServer`，未激活路径直接 404），给出一个必然 404 的链接是访问面最差结果；
2. 「指向默认语言回退页」会与产物 head 的 hreflang/canonical 互相矛盾（head 里 `en-US → /en-US/about`，页面上 en-US 链接却指向 `/zh-CN/about`），SEO 与访客认知双输；
3. 判据必须是构建输入的一部分才能守住确定性不变量（同一文档两次构建字节一致）：本实现只用「启用语言清单 + 本页逻辑路径」两项构建输入，`siteRouteEntries` 已按路径去重，因此「目标语言在本页没有独立可寻址路径」的语言不会进入清单（含未开启语言前缀时多语言映射同一路径 → 整个切换器不渲染）。发布/激活状态属运行时事实，一旦进产物会让同输入产出不同字节，故**不参与判据**。

**后台管理入口与交互**：

- 入口：`/admin/settings` 的「语言」分组（同一页面内，不新开独立页与侧栏菜单）：语言清单是工程级配置（`project_locales` 按 `project_id`），与站点名/简介同属一个设置面，且页面已有工程切换器（`?project=`），语言清单天然随工程切换；`settings.html` 的「待实现能力」清单里原本就列了「站点语言与地区」，此处正是其落地位置。
- 契约复用：读写一律经 `project` 的 `ListLocales` / `SaveLocales`，**不写第二套校验**（「至少一种语言、至多一个默认且默认必须启用、语言码白名单」单点在 `project/service/locale_service.go`）。
- 交互：行编辑器（语言码 + 默认 radio + 启用 checkbox + 删除）+「添加语言」（HTMX `hx-post` 到 `/admin/settings/locales/rows`，服务端重渲染行片段，未落库）+「保存语言清单」（普通表单 POST → 303 回跳，PRG）。
- 行身份用「提交顺序下标」（`langs` + `defaultIndex` + `enabledIndex`）而非语言码：用户可在表单里直接改语言码，用语言码做 value 会让默认/启用勾选静默丢失。
- 禁用语言提示（页面固定文案）：「禁用某语言后，该语言已激活的站点路由不会自动清理（需手动取消激活或删除路由占用）」。
- 校验失败不落库：回渲染设置页并给出提示（`MsgSiteLocalesInvalid`），保留用户输入便于就地修正。

**验证命令**（真实输出见提交说明）：

```bash
go build ./... && go vet ./... && go test ./... -count=1
go test ./internal/builder/ -run "TestLanguages|TestRenderDocumentLang" -count=1 -v
go test ./public/test/page/feature/ -run TestPageArtifactLanguageSwitcher -count=1 -v
go test ./public/test/dashboard/feature/ -run "TestSiteSettingsRendersLocaleGroup|TestLocaleRowsFragment|TestSaveSiteLocales" -count=1 -v
```

**仍属后续（本节新增遗留）**

- 改语言清单后不会自动重建已发布页面：切换器链接与 hreflang 进产物字节，清单变化后需重新构建/发布才生效（建议在 `SaveLocales` 成功后调用 page 模块的「全站标记待重建」能力，与 `MarkStaleForI18n` 同形；当前未接）。
- 禁用某语言后其已激活路由仍不会自动清理（§15.8 遗留，本节只提供提示文案）。
- 「目标语言已登记路由但尚未发布」的语言仍会给出链接（会 404）：这是确定性判据的必然取舍，如需严格隐藏需把 `page_routes` 存在性纳入构建输入并接受产物随发布状态变化。
- 语言前缀开关 `i18n.site_lang_prefix` 默认 false：关闭时多语言映射同一路径，切换器不渲染（同一页多语言也不能同时在线），页面已用文案提示。

## 变更记录

- v7（2025-09）：新增 §15.9——语言切换器 UI 与后台语言清单管理已落地：`core.languages` 独立组件（纯链接零 JS、当前语言 `aria-current` 不可点、展示名用语言自称）+ 构建期 `WithLocaleLinks`（与 hreflang 同源）+ `<html lang>` 跟随构建语言 + §9 缺语言策略选 S2「隐藏」及其理由 + 站点设置「语言」分组（复用 project `ListLocales`/`SaveLocales`、HTMX 行片段增删、禁用语言提示、校验失败不落库）+ 迁移 065 词条 seed；§15.8「仍属后续」第 1 条标记为已落地，并新增 4 条遗留（清单变更不自动重建、禁用语言路由不清理、未发布语言仍出链接、前缀开关默认关闭）。
- v6（2025-09）：新增 §15.8——P3 站点多语言上线已落地：迁移 062 `page_publications`（每语言激活状态真源）+ 063 `page_stagings`（每语言暂存指针）+ 064 `project_locales`（语言清单）；Publish/Rollback/UpdateURL 按语言作用域、`RenameReserved.OnlyReserved` 防跨语言误改、路由登记逐语言、产物 head hreflang 与 sitemap 语言分组、导航高亮双重前缀修复；含端到端双语言在线证据与灰度开关双路径测试。
- v5（2025-09）：新增 §15.7——§15.5 第 1 条（`page_artifacts` 同页多语言互相覆盖）已修复：迁移 061 加 `lang` 列并改唯一键为 `(page_id, version, lang)`、model/service 补语言维度、装配层调用点补 `lang`，含存量回填与幂等验证；§15.5 第 2/3 条仍属 P3。
- v1（2025-09）：首版设计提案。确立「`lang ∈ BuildContext`」「`/{lang}/path` 前缀」「CMS 内容字段级 + 主表/翻译表」三条主线，列出 10 个待决策点。
- v2（2025-09）：**收敛最终决策**——新增 §7「`sys_translation` 内容寻址翻译层 + 翻译工作台」（17 条决策 F1–F17、DDL、组件 `Translatable` 声明、构建期批量取数 SQL、工作台界面示意、跨页面复用提示、一键 AI 预留形态、埋点三层、质量五道防线、AI 直接生效风险与缓解、CMS 共用同一张表）；§6 标注为历史论证（模型 C 收敛为内容寻址单表，`content_translations` 被取代）；§7–§14 顺延重编号，同步更新交叉引用；新增章节索引；D3 标记为已拍板，新增 D11–D16 遗留待拍板点。
- v4（2025-09）：新增 §15 实施记录（P2 构建内核加 lang 维度）——Manifest.lang、BuildInput/Draft、PageRecord.Lang、LangPath 路径映射、激活层祖先符号链接防线、构建期冻结词条快照、DependencyKind=i18n 与 MarkStaleForI18n、装配层全链路传语言；记录 D1 语言根采用 `/{lang}/index`、落地开关 `i18n.site_lang_prefix`（默认 false）、产物 hash 变化与 P3 前置缺口。
- v3（2025-09）：新增 §10.5 实施记录——组件文案构建期翻译已落地（10 处访客面固定文案走 `site.component.*` key，含 enhance.js 圆点标签改为「构建期下发模板」；迁移 060 seed 13 key × zh-CN/en-US，取词兜底链单点在 `pkg/i18n.Translate`）；`DependencyKind=i18n`、构建期冻结快照、`/{lang}/` 产物仍为后续阶段。
