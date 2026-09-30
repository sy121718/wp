# 10 · 文档盘点与待办清单

> 盘点对象：\`docs/\` 下全部 Markdown（含 \`docs/agents/\`）。
> 盘点方式：**只读扫描**，未修改任何既有文档；本文为新增汇总。
> 盘点时间：2026-09（以当前工作区代码为事实来源核对）。
> 结论口径：文档说「未做」但代码已存在的，一律标注 **文档滞后-代码已有**，避免按旧结论重复施工。
>
> **2026-09 回填状态（第 0 步已执行）**：DOC-1 / DOC-2 / DOC-3 已修，另同步修正 DOC-8 / DOC-13 / INF-3 与 I18N-1 的现状口径。改动文档：\`01-overview.md\`（§1.1 术语、§1.4 非目标、§4 目录树与模块表、§5 冻结边界）、\`05-implementation-plan.md\`（当前状态表 + 阶段 4–7 任务与验收门禁）、\`02-domain.md\`（§4.2/§4.3 补实现归属）、\`02-B-media-center.md\`（§6 实现映射 + §7 表结构）、\`09-session-handoff.md\`（§1.3/§1.6/§4 + 新增 §5 回填记录）。本次只改 \`docs/\`，未动代码。
>
> **2026-09-26 现状列复核（本批次，只改本文档）**：对全部 104 条的「现状」列逐条回代码核验，**「未开始」判定的过期率很高** —— 本轮改判 **12 条为「已落地」**（SEO-1 / SEO-2 / SEO-3 / SEO-7 / SEO-9 / I18N-3 / I18N-6 / I18N-7 / I18N-12 / PIPE-2 / INF-8 / BIZ-9）、**4 条为「部分完成」**（CMP-7 / I18N-10 / PIPE-1 / PIPE-12）、**1 条为「文档滞后-代码已有」**（CMP-5）；另有 **11 条引用的 \`文件:行\` 已失效**（I18N-1 / I18N-2 / I18N-4 / SEO-6 / PIPE-11 / CMP-9 / CMP-10 / CMP-11 / CMP-13 / INF-4 / INF-7 —— 逐条见各行）与 **2 条引用了不存在的路径/函数名**（INF-1 的 \`nav_menu.go\`、I18N-2 的 \`compileBlockFragment\`）。
> 同时按发起人指令，**插件生态（PLG-1~\`PLG-10\`）整组改为「明确不做（冻结）」**，口径已在 §2 / §9 第 3 步 / §10.3 第 2 条 / 附一 四处一并贯彻（连带的 §10.3 第 3 条、§9 第 2 步、§9 第 1 步也按本轮实测同步修正）。
> **复核方法（下次盘点请沿用）**：**按能力追、不按名字追** —— 本仓已有两次教训（\`COV-4\` 的函数改名后按字面量 grep 会误判「测试丢失」；\`I18N-2\` 的 \`compileBlockFragment\` 已重构掉）。另外**不要用 \`rg -r\`**（那是 \`--replace\`，会把命中替换掉并伪造出无意义输出）；**中文关键词必须限定 \`--glob '*.go'\` / \`--glob '*.html'\`**，否则「评论」会命中 \`COMMENT ON TABLE\`、「积分」会命中 \`permission points\`。

---

## 0. 盘点方法与扫描范围

### 0.1 扫描范围

| 项 | 值 |
|---|---|
| 文档数 | 39 个 \`.md\`（\`docs/\` 根目录 36 + \`docs/agents/\` 3） |
| 总行数 | 7715 行 |
| 最大文档 | \`06-D-site-i18n.md\`（1386 行）、\`02-domain.md\`（866 行）、\`03-pipeline.md\`（683 行） |
| 逐份提取 | 全部 39 份均提取「主题 / 已实现 / 未实现」三要素 |

### 0.2 方法

1. **全文读取**重点文档：\`05-implementation-plan.md\`、\`06-A\`、\`06-B\`、\`06-plugin-system.md\`、\`03-pipeline.md\`（§4.4/§7/§8）、\`06-D-site-i18n.md\`（§13/§14/§15 全部小节）、\`09-session-handoff.md\`、\`02-E\`、\`04-B\`、\`04-A\`、\`0-A2\`、\`02-D\`、\`03-A-workbench\`、\`06-C\`、\`jet-and-go-libs-plan\`、\`component-jet-migration-plan\`、\`media-variants-recon\`。
2. **关键词定位**其余文档：\`规划|待实现|待做|TODO|未做|未实现|尚未|后续|暂不|预留|待决策|缺口\`，命中 206 处，逐条归并。
3. **代码交叉核对**：对每条待办用 \`grep\`/\`glob\`/\`ls\` 核对 \`internal/\`、\`public/migrations/\`、\`internal/templates/\` 是否已有对应文件/函数/表/迁移。

### 0.3 统计

> **口径重算（2026-09-26 复核）**：旧版本节写「待办总条数 **98**」、附一写「合计 **104**」，同一文档两个总数。
> 判据：正文九组编号连续、无缺号无重号，逐行点数实测 = 10（SEO）+ 10（PLG）+ 16（CMP）+ 4（COV）+
> 18（I18N）+ 13（PIPE）+ 9（BIZ）+ 11（INF）+ 13（DOC）= **104**，与附一合计一致。
> 旧读数 98 是盘点初版的口径：其分项（65+8+24+1）与附一旧分项（68+10+24+2）**自身就不自洽**，
> 说明两处是不同时点的两次统计。差额来自盘点后逐次追加的条目 —— 其中 \`COV-1\`~\`COV-4\`（第 11 节）
> 明确可考为 2026-09-19 补入，其余无逐条考证。**一律以正文实测 104 为准**，本表与附一同口径重算。

| 指标 | 数量 |
|---|---|
| 条目总数 | **104** |
| 未开始 | **26** |
| 部分完成 | **14** |
| 文档滞后（清单已核为落地，目标文档口径未跟上） | **21** |
| 待决策（非实现项） | **2** |
| 已落地 / 已核销（无须再动，核对即可） | **31** |
| **明确不做（冻结，不在当前路线内）** | **10** |

> 说明：\`文档滞后\` 条目同样计入总数，但**不应作为施工项**——先修文档口径即可。
> 真正需要动手的是「未开始 + 部分完成」共 **40** 条；**冻结的 10 条（PLG 全组）不计入施工范围**。
> \`文档滞后\` 与 \`已落地\` 两列**不可相加当作「都做完了」**：前者指清单已核为落地、但**目标文档的口径还没跟上**（仍需改文档）。

> **与审计台账的关系（根因，2026-09-26 补）**：仓库 \`scripts/audit-mark-resolved.py\` 是审计发现项的
> 闭环台账（实测 **209** 条，覆盖 \`SEC-*\` / \`TX-*\` / \`PERF-*\` / \`IDX-*\` / \`SEO-*\` / \`I18N-*\` 等），
> 代码注释普遍回引它的编号（「审计 SEO-009」「审计 I18N-017」）。**本清单与该台账未同步** ——
> 本清单的过期读数主要来自「台账已闭环、清单未同步」。**凡台账已记闭环的条目，先按台账核销，再决定是否施工。**
> **警告：两套编号体系不同，不可字面映射** —— 台账 \`SEO-009\`（GSC 验证 meta）对应本清单 \`SEO-1\`；
> 台账 \`I18N-017\`（禁用语言下线路由）对应本清单 \`I18N-12\`。按编号跨体系对照会直接对错项。

---

## 1. SEO

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| SEO-1 | Google Search Console 验证（验证文件/元标签，发布时落根路径） | \`06-A-plugin-ecosystem-roadmap.md\` §2.1（L77 起；原引 L40 已随该文档重写失效） | ✅ **已落地（2026-09-26 复核）**：构建期注入链 —— 设置字段 \`internal/module/project/dto/site_settings_dto.go:23-28\`（\`SearchConsoleVerification\`）→ 保存校验 \`internal/module/project/inbound/http/site_settings_admin_pages.go:293-294\`（\`builder.NormalizeSearchConsoleVerification\`）→ 工程装配 \`internal/pipeline/compile_analytics.go:38\`（\`SiteSearchConsoleVerification\`）/ \`:47\`（\`AnalyticsCompileOptions\`）→ 构建选项 \`WithSearchConsoleVerification\`（\`internal/builder/seo_verification.go:76\`）、\`internal/builder/builder.go:774\` 装配为 \`SearchConsoleHead\` → head 注入点 \`internal/builder/document.jet:10\`。断言见 \`internal/builder/seo_verification_test.go\`（空值零字节 / 设值进 head / 恰好一次 / token 大小写敏感 / 形状白名单 / 确定性）。**原记录「无 \`google-site-verification\` 相关代码」已过时** —— 实现名是 \`SearchConsoleVerification\`，按字面量 \`google-site-verification\` 检索必然零命中 | — | 无 |
| SEO-2 | IndexNow 主动推送（发布激活后 ping 搜索引擎） | \`02-E-seo-scoring-engine.md\` §6 步骤 6（L111）、§2.2（L33） | ✅ **已落地（2026-09-26 复核）**：\`internal/seo/indexnow.go:18\` \`NotifyIndexNowAsync\`（站点根地址或 key 为空则不动；异步 goroutine ping，失败只记日志）；两个发布侧调用点 —— \`internal/module/page/service/page_indexnow.go:11-21\`（发布成功后取 \`settings.IndexNowKey\`）与 \`internal/module/presentation/service/presentation_indexnow.go:16-27\`；设置字段 \`project/dto/site_settings_dto.go:29-30\`，后台配置项 \`internal/templates/admin/project/settings.html:98-104\`。**原记录「未开始」已过时** | — | 无 |
| SEO-3 | redirects 管理器（插件形态，与 publication URL 占用协同） | \`02-E\` §6 步骤 7（L112）、\`06-B-dual-track-adr.md\` 决策 6（L66） | ✅ **已落地（2026-09-26 复核）**：后台管理页与 API 齐全 —— \`internal/module/page/inbound/http/page_router.go:60\`（GET \`/api/page/redirect\` 列表页）/ \`:61\`（create）/ \`:62\`（delete）/ \`:63\`（merge）/ \`:92\`（\`/page-redirects/bulk-delete\`，复用单条删除权限点），权限点 \`PageRedirectView\` / \`PageRedirectCreate\` / \`PageRedirectDelete\` / \`PageRedirectMerge\`（\`internal/permission/codes.go:394-400\`），模板 \`internal/templates/admin/page/page_redirects.html\`，服务实现 \`internal/module/page/service/page_redirect.go:65\`（\`ListRedirects\`）/ \`:121\`（\`CreateRedirect\`）/ \`:205\`（\`DeleteRedirect\`）/ \`:241\`（\`MergeRedirectChain\`）。**原记录「仅缺管理界面」已过时** —— 管理器已落地，缺的只是「插件形态」这一点，而插件生态**已冻结**（见 §2） | — | PIPE-5 已完成 |
| SEO-4 | Yoast 式 SEO 分析器**插件化**（L0，进 workbench 检查器侧栏） | \`06-A\` §2.2（现 L89 起，原引 L45-49 已失效）、§3（现 L95 起，原引 L58 已失效） | **部分完成（2026-09-26 复核）**：本体侧 \`internal/seo/scoring\` + 页面设置评分面板已落地；**检查器侧栏也已落地** —— 路由注释 \`internal/module/workbench/inbound/http/router.go:194\`（\`/workbench/seo-score-panel\`）、handler \`internal/module/workbench/inbound/http/seo_score_handle.go\`、片段 \`internal/templates/fragments/seo_score.html\`、前端刷新 \`internal/templates/static/js/workbench/methods/panels.js:123-129\`（\`refreshSeoScoreHtmx\`）。**原记录「插件形态与检查器侧栏未做」中「侧栏未做」已过时**；「插件形态」不再作为待办 —— 插件生态已冻结（见 §2），本条**只余本体侧维护** | 低 | 无（原依赖 PLG-9，已随插件生态冻结解除） |
| SEO-5 | 英文 Flesch 阅读容易度规则 | \`02-E\` §6 步骤 2（L107）、§4.2 表（L75） | 部分完成：已按 CJK 分支做句长/段长（\`checks_impl.go\` \`isCJK\`），英文 Flesch 未实现 | 低 | 无 |
| SEO-6 | meta Profile 三级默认回落（页面 → 站点 → 省略） | \`02-E\` §5（L98）、§6 步骤 4（L109） | 文档滞后-代码已有：\`internal/builder/seo_head.go:16\` 注释与实现均含三级回落（页面 \`settings.seo\` → 站点默认 → 省略）。**2026-09-26 行号复核：原记录引的 \`:12\` 已失效** | — | 无 |
| SEO-7 | 标题长度改用**像素宽**判定（Yoast 做法） | \`02-E\` §8.3 差异登记（L186）、§7 印证结论 2（L137） | ✅ **已落地（2026-09-26 复核）**：\`internal/seo/scoring/checks_impl.go:23\` \`chkTitleLength\` 已按**展示宽度**判定而非字符数 —— \`internal/seo/display_width.go:7\` \`DisplayWidth\`（ASCII 1 单位、CJK/全角 2 单位）+ \`internal/seo/scoring/scoring.go:254\` 同口径实现（避免与 \`seo\` 循环依赖），阈值 \`internal/seo/scoring/benchmarks.go:41-45\`（理想 40–60、硬上限 65）。**原记录「当前仅字符数」已过时**。口径提示：它是**宽度单位估算**（注释自陈「不测量真实字体」，约等于 SERP ~580px），不是真实字体像素测量 | 低 | 无 |
| SEO-8 | 关键词密度建议区收紧到 1–1.5%（RankMath 印证） | \`02-E\` §8.3（L187）、§7 结论 2 | 未开始（当前 0.5–2.0% 主判） | 低 | 无 |
| SEO-9 | 商品结构化数据（Product JSON-LD 由商品数据驱动） | \`06-A\` §2.1（现 L77 起，原引 L39 已失效） | ✅ **已落地（2026-09-26 复核）**：\`schemaType=product\` 与 JSON-LD 输出在 \`internal/builder/seo_head.go:399\`（\`offer != nil && schemaType=="product"\`）+ \`internal/builder/settings.go:152-153\`（\`ProductOfferLD\`）；**商品实体数据源也已接通** —— 详情页构建期 \`internal/module/presentation/service/presentation_seo.go:173\` 调 \`:197\` \`productOfferLD\`，经 \`core.ContentResolver\` 读 \`price\` / \`sku\` / \`variants\`（决定 InStock/OutOfStock）/ \`ratingCount\` / \`rating\` 组装 Offer。**原记录「缺商品实体数据源」已过时**（商品域本体已落地，见 BIZ-1）。范围提示：仅构建期静态 Offer，不含实时库存（注释自陈，SEO-005） | — | BIZ-1（已完成） |
| SEO-10 | 文章编辑页评分入口（正文侧栏：密度/长度/可读性/内链） | \`02-E\` §9 表（L197）、\`09-session-handoff.md\` §1.4（L45） | ✅ **已落地（2026-09，2026-09-26 复核行号）**：\`internal/seo/article.go:48\` 的 \`ScoreArticle\`（文章字段 → \`scoring.Input\`：标题即 <title>、摘要即 meta description（2026-09-30 字段合并后不再有 seoTitle/seoDescription 回落）、正文去标签算字数、h1-h6 结构、正文图片 + 封面、内外链与锚文本）；编辑页侧栏 \`POST /admin/articles/score\`（\`internal/module/content/inbound/http/article_router.go:82\` 路由 → \`article_handle.go:424\` \`ArticleScorePanel\`；HTMX 局部刷新，无 JS 时打开 / 保存后整页渲染同一份分）。**本次补记（2026-09）**：canonical 与结构化数据原先恒判未达标（当时的真实缺口），现由构建期注入（`presentation/service/presentation_seo.go`：canonical 取实例线上路径、JSON-LD 按 `schemaType=article`），`ScoreArticle` 已同步判真 —— 侧栏只显示编辑者能改的东西，系统保证项不挂在上面 | — | INF-1（已完成） |

---

## 2. 插件体系（整组冻结 · 明确不做）

> **本节整组冻结（2026-09-26 发起人指令）**：插件生态**不在当前路线内，不是待施工项**。
> 冻结范围与重启条件见 [06-A-plugin-ecosystem-roadmap.md](./06-A-plugin-ecosystem-roadmap.md) 的
> 「当前状态（冻结 · 明确不做）：插件生态不在当前路线内」（L7）与「决策记录（2026-09-15）：插件生态延后」
> （L28；**重启条件三条**在同节 L40-41：`CQ-007` 与 `UIK-003` 收口、本体对外契约定稿、
> 出现真实第三方扩展需求 —— 三条同时满足才重启）。
> 下列 10 条的**优先级一律为 `—`**，排期时不得计入工作量；要重启先按上述决策记录重新判据，
> **不要**把本节当成现成待办直接开工。
> 冻结只改口径、不改事实：各行仍记录代码里**已存在**的部分（示例插件包、`presets` 机制等），那是既有产物，不是待办。

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| PLG-1 | L2 插件**自有集合源**注册（\`collections.json\` 声明式，\`plugin:{id}.{table}\` 通道） | \`06-plugin-system.md\` §9（L245-264）、§13 P3（L310） | **明确不做（冻结，2026-09-26）**：不在当前路线内，不是待施工项。原记录「部分完成」的事实面仍成立 —— 组件级 \`Collection\` 绑定与 \`content:{product\|article\|category}\` 本体通道已落地（\`internal/module/content/service/collection_resolver.go:26\`、\`internal/module/product/service/collection_resolver.go:46\`），\`plugin:{id}.{table}\` 仅白名单正则、无注册机制（\`internal/builder/source/collection.go:27\`）；该缺口**不排期** | — | — |
| PLG-2 | manifest 顶层 \`collections\` 段（§5.1 目录结构 / §5.3 骨架） | \`06-plugin-system.md\` §5.1（L106）、§5.3（L147） | **明确不做（冻结，2026-09-26）**：原记录「未开始（\`plugincomp.Manifest\` 无 collections 字段）」的事实仍成立（\`internal/builder/plugincomp/plugincomp.go\` 的 \`Manifest\` 无该字段），但**不排期** | — | — |
| PLG-3 | manifest \`admin.menu\`（插件后台菜单注册） | \`06-plugin-system.md\` §5.3（L148） | **明确不做（冻结，2026-09-26）**：原记录「未开始」事实成立，但**不排期** | — | — |
| PLG-4 | 插件 \`fragments/\` 目录（运行时片段随包分发） | \`06-plugin-system.md\` §5.1（L114） | **明确不做（冻结，2026-09-26）**：原记录「未开始」事实成立，但**不排期** | — | — |
| PLG-5 | zip 签名校验（\`signature\` 字段 + 公钥验签，未签名仅 dev 可装） | \`06-plugin-system.md\` §5.4（L152-155）、§11 表（L290）、\`06-B\` 决策 1/4（L18、L50） | **明确不做（冻结，2026-09-26）**：原记录「未开始」事实成立，但**不排期**（重启后它仍是任何第三方分发的前置） | — | — |
| PLG-6 | 插件**受限事务 API**（service 层 \`Transaction()\` 透传给受信插件） | \`06-A\` §4 前置硬骨头 2（原引 L87 已随该文档重写失效）、\`06-B\` 决策 4 前置 2（L49） | **明确不做（冻结，2026-09-26）**：原记录「未开始（\`plugin/model\` 的 \`Transaction\` 仅服务 L1 迁移执行器）」的事实仍成立，但**不排期** | — | — |
| PLG-7 | 第三轨（Yaegi 解释器 / Wasm 沙箱受限逻辑） | \`06-plugin-system.md\` §4.3（L91-96）、§13 演进（L311） | **明确不做（冻结，2026-09-26）**：原记录「未开始（文档明确『首发不做，留作 Fragment 落地后再评估』）」的事实成立，但**不排期** | — | — |
| PLG-8 | 插件市场与签名分发 | \`06-plugin-system.md\` §13 演进（L311） | **明确不做（冻结，2026-09-26）**：原记录「未开始」事实成立，但**不排期** | — | — |
| PLG-9 | 首批插件 12 个（forms/marketing/SEO 分析器/analytics/membership/points/i18n/comments/search/smtp/组件包） | \`06-A\` §3（现 L95 起，原引 L53-66 已失效） | **明确不做（冻结，2026-09-26）**：首批 12 个插件不排期。**原记录「工作区无 \`plugins/\` 目录，无示例插件包」已过时** —— \`examples/\` 下已有 4 个示例插件包（\`l0-demo\` / \`l0-style-only\` / \`l0-template-assets\` / \`l1-marketing\`，各含 \`manifest.json\` + \`components\`，前两者另有 \`assets\`，后两者另有 \`migrations\`）；这些是**既有产物**，不是待办 | — | — |
| PLG-10 | 组件包 packs（L0，轮播/动效/图标库/定价表/团队墙/时间线） | \`06-A\` §3（现 L95 起，原引 L66 已失效）、§6 备忘（原引 L104 已失效） | **明确不做（冻结，2026-09-26）**：组件包 packs 的分发不排期。**原记录「未开始（文档标注『样式引擎 + presets 已就绪，现在就能写』）」中「presets 已就绪」可核** —— \`presets\` 机制本身已实现（\`internal/builder/plugincomp/plugincomp.go:49-50\` \`Manifest.Presets\`、\`:149-155\` 数量上限与去重校验、\`:172\` \`validatePreset\`）；缺的是**组件包的分发**，该项**不排期** | — | — |

---

## 3. 组件与构建器

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| CMP-1 | \`component\` 模块（Global Component 版本 + \`immutable/auto-update/pinned\` 策略 + Registry manifest） | \`05-implementation-plan.md\` 阶段 5（L230）、\`01-overview.md\` 模块表（L286）、\`02-domain.md\` §4.2/§4.3（L406-472） | 未开始（\`internal/module/\` 无 component；当前由 \`block\` 模块 \`reuse_mode=global\` 承担复用语义） | 中 | 概念需先对齐（见 §9 DOC-13） |
| CMP-2 | Jet 三层 Set 隔离 + \`AddGlobalFunc\` 注入 | \`jet-and-go-libs-plan.md\` 待办表 P1（L216）、§3（L47-58） | 文档滞后-代码已有：\`templates/jet_render.go\`（admin/workbench）、\`component_set.go\`（构建期）、\`fragment_render.go\`（片段）、\`plugin_loader.go\`（插件）各自 Set + \`injectGlobals\` | — | 无 |
| CMP-3 | \`builder/core\` 抽库（拆 \`internal\`，开源复用） | \`jet-and-go-libs-plan.md\` 待办表 P3（L220）、\`component-jet-migration-plan.md\` §八（L196） | 未开始 | 低 | 无 |
| CMP-4 | Jet 模板 IDE 高亮/校验插件选型 | \`component-jet-migration-plan.md\` §八（L197） | 未开始 | 低 | 无 |
| CMP-5 | \`card\` 组件缺标题/正文排版字段（当前只能靠通用层） | \`09-session-handoff.md\` §4（L384） | **文档滞后-代码已有（字段面，2026-09-26 复核）**：\`internal/builder/components/card/card.go:19\` \`Title\`（\`ct:"text,maxlen=200"\`）、\`:22\` \`TitleTag\`（\`ct:"select"\`，h2~h5 白名单，专为避免标题跳级）、\`:24\` \`Text\`（\`ct:"richtext,maxlen=1000"\`）；渲染侧 \`internal/builder/components/card/jet.go:55-62\` 做白名单回落（非 h2~h5 一律 h3）。**原记录「card.go 仅 title/text/buttonText/buttonLink」已过时**（另有 \`imageSrc\` / \`loading\` / \`fetchPriority\` / \`advanced\`）。**仍成立的一半**：字号 / 对齐 / 间距这类**排版**项仍走通用层 \`Advanced core.AdvancedProps\`（\`:36\`） | 低 | 无 |
| CMP-6 | \`core.text\` \`ct:maxlen\` 与清洗硬上限不一致 | \`09-session-handoff.md\` §4（L385） | 文档滞后-代码已有：\`core.MaxRichLen\` 已统一为 30000，与 \`ct:"richtext,maxlen=30000"\` 一致 | — | 无 |
| CMP-7 | 富文本图片（Trix \`figure/img\`）未接媒体变体与 caption | \`09-session-handoff.md\` §4（L386） | **部分完成（2026-09-26 复核）**：**caption 侧已落地** —— 清洗白名单保留 \`figure\` / \`figcaption\`（\`internal/builder/core/richtext.go:57\`），并用 \`figcaption\` 纯文本回填同级 \`<img>\` 的 \`alt\`（\`:476\` \`backfillFigureAlt\`，调用点 \`:110\`；原注释 \`:86\` 说明动机）。**仍缺**：正文 \`img\` 不走 \`srcset\`（媒体变体目前只在 image 组件侧接入）。**原记录「未开始」已过时**（caption 已做） | 中 | 无 |
| CMP-8 | \`infobox.icon\` 手写面板与后端 tag 是否重复显示 | \`09-session-handoff.md\` §4（L389） | **仍待确认（2026-09-26 复核：两侧都在，是否重复显示本轮未判定）**：前端手写面板确实存在 —— \`internal/templates/static/js/workbench/methods/controls/repeater.js:49\` \`infoboxPanel\`（注释自陈「仅图标选择（其余字段 schema 驱动）」，调用点 \`internal/templates/static/js/workbench/methods/controls/misc.js:444\`）；后端字段 tag 为 \`ct:"select,..."\`（\`internal/builder/components/infobox/infobox.go:58\`）。本轮只核到「手写面板只覆盖 icon 一个字段」，**未做界面实拍判定**，因此结论保持原样 | 低 | 无 |
| CMP-9 | 容器 flex 布局面板（方向/主轴/交叉轴/换行/间距） | \`09-session-handoff.md\` §4（L388） | 文档滞后-代码已有：\`containerLayout()\` 已渲染完整面板，核查后无需改动（同文 L281 已记录）。**2026-09-26 行号复核**：定义现在 \`internal/templates/static/js/workbench/methods/controls/misc.js:251\`（09 文中的 \`:128\` 已随控件拆分失效），调用点 \`misc.js:440\` | — | 无 |
| CMP-10 | workbench 块「插入-复制」前端接线 | \`02-D-reusable-assets.md\` §12 步骤 5（L306） | 文档滞后-代码已有：前端已调 \`POST /api/block/clone\`。**2026-09-26 行号复核**：现在 \`internal/templates/static/js/workbench/methods/canvas.js:278\`（\`insertBlockClone\`，fetch 在 \`:283\`；另 \`:188\` 是模板一次性复制语义的注释）—— 原记录引的 \`:250-255\` 已失效。注：目标文档 \`02-D\` §12 步骤 5 至今仍写「workbench 前端接线待做」（见 §8 DOC-6） | — | 无 |
| CMP-11 | \`inspector.js\` 拆分（\`syncInspector()\` 内嵌 49 个闭包控件函数，约 1500 行） | \`09-session-handoff.md\` §3（L194、L355） | ✅ **文档滞后-代码已有（2026-09 复核，2026-09-26 行数重测）**：控件已提取到 \`internal/templates/static/js/workbench/methods/controls/*.js\`（9 个文件 —— base/color/corners/media/misc/repeater/selects/spacing/text，\`wc -l\` 合计 **2263** 行），\`internal/templates/static/js/workbench/methods/inspector.js\` 实测 **252** 行。**原记录的行数（2093 / 161）已过时** | — | 无 |
| CMP-12 | 服务端持 AST（编辑操作服务端化，彻底压缩 workbench.js） | \`09-session-handoff.md\` §3 收益天花板（L355-356） | 未开始（评估项） | 低 | CMP-11 |
| CMP-13 | htmx 官方扩展引入（head-support / response-targets / loading-states / class-tools 等） | \`06-C-htmx-extensions.md\` §二（L21-37） | 部分完成（**2026-09-26 路径与行号复核**）：idiomorph 已引入 —— vendor 实文件 \`internal/templates/static/vendor/idiomorph/idiomorph.min.js\`（\`VERSION\` 记 0.8.0）+ \`.../idiomorph-ext.min.js\`；引入点 \`internal/templates/workbench/layout.html:202\`（注释 \`:200\`，**原记录引的 \`:181\` 已失效**）；封装 \`internal/templates/static/js/workbench/core.js:56\` \`morphHTML\`（注释 \`:35\`，**原记录只写 \`core.js\` 未给目录，实际在 \`workbench/\` 下**）；其余 htmx 扩展仍待评估 | 低 | 无 |
| CMP-14 | 结构树拖拽 DOM 级自动化测试 | \`09-session-handoff.md\` §3 交互走查补充（L268） | 未开始（\`moveNode\` 已有 Go 侧行为覆盖） | 低 | 无 |
| CMP-15 | 富文本 ⇄ 可视化组件树的等价转换（\`06-B\` 决策 5） | \`06-B-dual-track-adr.md\` 决策 5（L52-59） | **转换能力已落地（2026-09）**：\`internal/builder/richdoc\` 的 \`HTMLToNodes\`（块级标签 → 组件：h1~h6→heading / p→text / ul,ol→list / blockquote→quote / pre→text / img,figure→image / hr→divider / table→table；行级格式留在 core.text 内）与 \`NodesToHTML\`（可逆子集反向导出：heading / text / list / quote / image / divider / table / **core.accordion → \`<details><summary>摘要</summary>正文…</details>\` 序列**，items ↔ children 一一对应时才导出；不可逆组件与结构对不上的手风琴输出占位并标 \`Lossless=false\`）+ fuzz 背书（\`FuzzRichTextRoundTrip\` 90 秒 72.9 万次执行通过，历史失败用例留在 \`testdata/fuzz/\`；畸形嵌套如 \`<a>\` 里嵌 \`<table>\` 的序列化不是自身解析的不动点，该类往返仍可能漂移，见 \`06-B\` 决策 5 落地状态）。**入口已落地（2026-09）**：文章编辑页「导入到画布」区块 —— 预览（纯计算，展示组件统计与损失清单）→ 创建页面草稿并跳工作台；端到端实测（正文 → 6 组件 → Page 草稿 → 预览编译出真实 HTML）。**未做**：① 真源改造（文章 body 仍存 HTML 字符串，「双视图单真源」还不成立）② 模板起稿入口（只做了文章 → 页面） | 中 | 无 |
| CMP-16 | 结构模板画布预览是否引入「宿主页面」参数（可选 `hostPageId`） | `03-A-workbench.md`、`13-module-inventory.md` 的 `contenttemplate` 行（L58）、本仓 `pkg/datarule` 之外的同类开放项对照 | **待决策（开放产品决策，不是缺陷）**：结构模板（header / footer 两个结构类型）在画布上编辑时，预览**是否需要宿主页面上下文** —— 即「用哪个页面的 `BuildContext` 来渲染这一版页眉 / 页脚」。当前实现**不使用**宿主上下文，默认行为保持不变，发布链不受影响（发布时宿主上下文来自真实引用方页面）。**要引入的话，代价在语义而不在数据模型**：它会让「预览所见 == 发布所得」这个既有保证变弱 —— 预览渲染的是**我选的那个宿主页面**的上下文，发布渲染的是**引用方页面**的上下文，两者不同时预览就会说谎。所以这是「预览要多准」的产品取舍，不是补一个参数那么简单。**决定前不要动手**；真要做，先明确：多宿主场景（一个页眉被 5 个页面引用）预览该怎么表达、未选宿主时的默认值是什么 | 低 | 无 |

---

## 4. 多语言（i18n）

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| I18N-1 | 灰度开关 \`i18n.site_lang_prefix\` 默认 \`false\`，多语言**不能同时在线** | \`06-D-site-i18n.md\` §15.3（L953-965）、§15.8 语义口径（L1044）、§15.9 遗留（L1133） | ✅ **已解决（2026-09 复核；2026-09-26 修键名与行号）**：**结论仍成立**（多语言已启用），但**证据已换形态** —— 键不再叫 \`site_lang_prefix\`：现为 \`config.yaml:88\` \`site_lang_url_mode: default_plain\`，旧键 \`site_lang_prefix\`（bool）仅作**兼容识别**（\`config.yaml:87\` 注释：「true → all_prefix、false → off」）；样例文件对应 \`config.yaml.example:122\`（注释 \`:121\`）。**原记录「\`config.yaml:66\` 与 \`config.yaml.example:65\` 均为 \`site_lang_prefix: true\`」的键名与行号均已失效** | 高 | 无 |
| I18N-2 | 块内文本不翻译（\`compileBlockFragment\` 未传 lang 与取词器） | \`06-D\` §15.11 已知缺口（L1269-1271）、§15.12（L1365-1366） | ✅ **已落地（2026-09 复核；2026-09-26 按能力重追）**：**函数名 \`compileBlockFragment\` 已被重构掉**，按能力追到取词器装配点 —— \`internal/module/page/service/page_assemble.go:215\` \`var contentTranslator *i18n.ContentTranslator\`，\`:219\` 把 \`s.newContentTranslator\` 传入 \`pipeline.AppendContentTranslationFor\`（页眉 / 页脚 / \`core.globalref\` 内联块一并收集候选）。**原记录「未传 lang 与取词器」已不成立**；注意**不要按 \`compileBlockFragment\` 字面量检索**（零命中会把本条误判为没做） | — | 无 |
| I18N-3 | \`core.nav\` 菜单标签多语言归属未定（标签来自 navigation 数据） | \`06-D\` §15.11（L1272-1273）、§15.12（L1370-1371） | ✅ **已落地（2026-09-26 复核）**：菜单标签翻译已有完整入口 —— \`internal/module/navigation/inbound/http/navigation_translation_handle.go:113\`（GET \`/admin/navigations/translations\`，注释 \`:112\`）/ \`:131\`（POST \`.../translations/save\` 整表提交，注释 \`:130\`），语境常量 \`:108\` \`navigationTranslationContext()\` 固定为 \`i18n.ContentContext("navigation","label")\`（注释自陈与构建期 resolver 逐字一致）；页面模板 \`internal/templates/admin/navigation/navigation_translations.html\`。**原记录「未开始（未定论）」已过时** | — | 无 |
| I18N-4 | 站内链接本地化只覆盖导航（按钮/图片/文本内链接仍是逻辑路径） | \`06-D\` §15.5 第 5 条（L992）、§15.8 仍属后续（L1064） | ✅ **已落地（2026-09，2026-09-26 行号复核）**：统一入口 `core.RenderContext.ResolveSiteLink`（`internal/builder/core/render.go:422`，注释 `:414`；**原记录引的 `:311` 已失效**），由 `builder.WithSiteLinkResolver`（`internal/builder/builder.go:228`）注入，与页面路径规则同源；空安全包装 `core.SiteLinkOrSame`（`internal/builder/core/render.go:444`）；补齐 infobox / quote / gallery / breadcrumb（作者手填层级）/ productcard / productlist / cardstack，导航另行经装配层 `LocalizeMenuURL`。判据是**数据来源不是字符串形状**：作者手填 props → 本地化；CMS 绑定值 → 原样。**仍未接线**：① 富文本正文 `<a href>`（需在 `core.RichTextHTML` 之后加「扫描不到站内候选就原样返回」的短路通道，否则重新序列化会改掉全站富文本页面字节）② `form.action`（`internal/builder/components/form/form.go:72-80`，刻意不本地化，判据在字段注释里）③ 面包屑「派生」层级的首页与中间段（`breadcrumb/jet.go:159-181`，带前缀方案下不可达，属既有缺口，修它要改「派生源是访问路径还是逻辑路径」的设计）。**不要照抄旧建议里的 `pipeline.LangPath`**（`internal/pipeline/lang.go:306` 会把默认语言也加前缀，与站点方案相反） | — | 无 |
| I18N-5 | Runtime Fragment 语言（\`/_fragments\` 无 \`lang\`、无 \`Vary\`） | \`06-D\` §15.5 第 7 条（L992）、§11（L855） | ✅ **已落地（2026-09；2026-09-26 补路径）**：端点（输出 \`Content-Language\` 在 \`internal/module/runtimefragment/endpoint.go:218\`；语言解析 \`internal/module/runtimefragment/lang_resolve.go:19\` \`fragmentLangParam = "lang"\` / \`:32\` \`resolveRequestLang\`）按 `?lang` 取词（经 `project_locales` 启用清单校验，非法值回落工程默认语言）并输出 `Content-Language`；**Vary 刻意不写 `Accept-Language`** —— 语言只由 URL 决定，声明一个不参与选择的头只会让 CDN 为同一份字节多建缓存桶（语言进 URL 即进缓存键，服务端片段缓存键也含语言）；构建期片段 lang 注入已覆盖 carticon / searchresults / orderlist / userforms / productlist / product / productselector / addtocart（GET 走 query、POST 走 hidden 表单域）；登录面板两句文案已接取词（迁移 293）。**剩余缺口**：① 片段响应**未声明缓存策略**，读身份的能力（`cartView` / `ordersList` / `accountProfileForm`）在共享缓存下有串个人数据风险 —— 需按能力区分公开可缓存 / 读身份 `Cache-Control: private, no-store`，**属产品口径，待拍板** ② `endpoint.go` 的协议错误文案（400/404/500）未国际化，属独立项 | — | 无 |
| I18N-6 | 后台 i18n 词条 CRUD（D7）+ \`MarkStaleForI18n\` 调用方不完整 | \`06-D\` §15.5 第 6 条（L985）、§14 D7（L912） | ✅ **已落地（2026-09-26 复核）**：路由 \`internal/module/admin/inbound/http/admin_pages_router.go:98\`（GET \`/admin/i18n\`，门 \`/api/i18n/list\` ⇒ 权限点 \`i18n:view\`，迁移 294）/ \`:99\`（\`/admin/i18n/edit\`）/ \`:100-102\`（update / save / delete，门 \`/api/i18n/save\` ⇒ \`i18n:manage\`）/ \`:106\`（\`/i18n/bulk-delete\`）；页面模板 \`internal/templates/admin/system/i18n.html\` 与 \`i18n_edit_form.html\`；读写端口 \`pkg/i18n/admin.go\`。**「\`MarkStaleForI18n\` 调用方不完整」也已不成立** —— 词条保存后调用的断言在 \`internal/module/admin/inbound/http/admin_i18n_stale_test.go\`。**原记录「仅 \`/admin/lang\` 语言切换路由，无词条管理页」已过时** | — | 无 |
| I18N-7 | CMS 内容字段翻译（P5d，\`sys_translation\` 接 content 模块） | \`06-D\` §13 P4（L895）、§15.11 范围（L1186） | ✅ **已落地（2026-09-26 复核）**：内容侧 \`internal/module/content/service/content_translate.go\`（注释自陈「审计 I18N-006」）+ 断言 \`content_translate_test.go\`；商品侧对偶实现 \`internal/module/product/service/entity_source_translate.go\`。**原记录「\`content/service\` 无翻译接入」已过时** | — | 无 |
| I18N-8 | 一键 AI 翻译（\`engine='ai'\`，当前按钮灰置预留） | \`06-D\` §7.9（L611-649）、§15.12（L1283） | 未开始（明确本期不做） | 低 | I18N-7 |
| I18N-9 | 译文删除 / 改原文后孤儿行清理（D14） | \`06-D\` §14 D14（L919）、§15.12（L1367） | 未开始 | 低 | 无 |
| I18N-10 | 媒体 \`alt\` 多语言（D6，新增 \`media_translations\`） | \`06-D\` §14 D6（L911） | **部分完成（2026-09-26 复核）**：**已有部分** —— 商品图集 \`alt\` 译文已落地：\`internal/module/product/service/entity_source_translate.go:205-216\`（\`imageAltTranslations\`，语境固定 \`product.imageAlts\`，URL 永不翻译），消费点 \`:309\`。**仍缺**：媒体库级 \`media_translations\` 表（\`rg media_translations public/migrations\` 零命中）—— 即「同一张图在多处引用时按语言给不同 alt」仍无处存 | 低 | 无 |
| I18N-11 | 每语言 slug（D4 方案②） | \`06-D\` §14 D4（L909） | 未开始（① 仅默认语言 slug 已实现） | 低 | 无 |
| I18N-12 | 禁用某语言后其已激活路由不自动清理 | \`06-D\` §15.8 仍属后续（L1065）、§15.9（L1131） | ✅ **已落地（2026-09-26 复核）**：语言下线链路齐全 —— 页面侧实现 \`internal/module/page/service/page_locale_retire.go:3\`；端口 \`internal/module/project/contract/project_service.go:109\`（\`LocaleRetirePort\`，\`:117\` \`LocaleRetireImpact\` 先给运营看代价）；显式确认字段 \`internal/module/project/dto/locale_dto.go:17-20\`（\`ConfirmRetire\`，注释自陈审计 I18N-017）；服务侧 \`locale_service.go\` 记 \`errLocaleRetireNeeded\` 并要求确认后才执行。**原记录「未开始（页面仅有提示文案）」已过时** | — | 无 |
| I18N-13 | 「目标语言已登记路由但未发布」仍给出 404 链接（S2 判据取舍） | \`06-D\` §15.9 仍属后续（L1132） | 未开始（确定性判据的必然取舍） | 低 | 无 |
| I18N-14 | 翻译完成度统计是近似（进程内 30s 缓存 + 1000 页上限） | \`06-D\` §15.12 已知缺口（L1368-1369） | 未开始 | 低 | 无 |
| I18N-15 | 待决策点 D11–D16（嵌套字段 context 命名 / project_id / AI 译文确认 / 孤儿清理 / .po 导入 / CMS 工作台入口） | \`06-D\` §14（L916-921） | 待决策（D11/D12/D13 已在 §15.11 按建议落地，D14/D15/D16 未拍板） | 低 | 无 |
| I18N-16 | \`i18n:content\` revision 粒度（全局 \`max(update_time)\` → 全站重建） | \`06-D\` §15.11 不确定项（L1274-1275） | 未开始 | 低 | 无 |
| I18N-17 | sitemap \`<loc>\` 未配 \`WP_SITE_BASE_URL\` 时输出站内路径 | \`06-D\` §15.8 仍属后续（L1066） | 未开始（既有行为） | 低 | 无 |
| I18N-18 | \`.po\` 导入入口（D15） | \`06-D\` §14 D15（L920） | 未开始 | 低 | I18N-6 |

---

## 5. 发布管线

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| PIPE-1 | 删除、取消发布与 GC（tombstone / 保留期 / \`gc_pending\` claim / 内容对象闭包回收） | \`03-pipeline.md\` §7（L373-400）、\`internal/pipeline/url.go:10\` 注释「§7/§8 属后续阶段」 | **部分完成（2026-09-26 复核，原读数「未开始」已过时）**：**已有部分** —— ① 保留期已落地：\`internal/module/page/service/page_retention.go:20\`（每页保留 20 条快照）/ \`:22\`（90 天窗口）/ \`:24\`（产物 30 天）/ \`:26\`（24h 间隔）/ \`:28\`（500 行一批，避免长事务），任务编排在 \`internal/retention/\`；② **产物 GC 已定时化**：\`internal/module/page/inbound/http/page_router.go:116\` \`StartPageRetentionScheduler(svc)\`（此前只能人工调 \`POST /api/page/artifact/gc\`）；③ **\`gc_pending\` 已有实现**（不是「零命中」）：\`internal/module/artifact/contract/artifact_service.go:27\`、\`internal/module/artifact/model/artifact_model.go:288\`、\`internal/module/page/service/page_artifact_rebuild.go:126\`（同 hash 仍被引用时只标 \`gc_pending\` 不删文件）；④ 内容对象闭包回收：\`internal/module/artifact/service/artifact_content_gc.go\`（审计 IDX-016）。**仍缺**：\`tombstone\`（全树 \`rg tombstone\` 零命中）—— 即删除域那边仍无墓碑语义 | 中 | 上线前必须补（剩余部分） |
| PIPE-2 | 构建队列 / Build Job Engine（幂等键、superseded、重试与退避） | \`03-pipeline.md\` §8.3（L492-500）、\`0-A1-pipeline.md\` §3.2（L46） | ✅ **已落地（2026-09-26 复核）**：Go 侧引擎齐全 —— \`internal/module/build/service/build_worker.go:25\` \`RunOnce\` / \`:109\` \`StartWorkers\`（n 个消费协程 + 1 个租约回收）/ \`:141\` \`reclaimLoop\`（回收租约到期未结束的任务，\`:105\` 注明认领走 \`FOR UPDATE SKIP LOCKED\`，多实例安全）、\`internal/module/build/service/build_service.go\`、\`internal/module/build/model/build_model.go\`、\`internal/module/build/contract/build_service.go\`、\`internal/module/build/inbound/http/build_router.go:30-32\`（GET \`/api/build/queue\` / GET \`/api/build/jobs\` / POST \`/api/build/retry\`，权限点 \`build:queue\` / \`build:jobs\` / \`build:retry\` 见 \`internal/permission/codes.go:72-76\`）；幂等由 DB 唯一索引保证 —— 迁移 \`295_build_jobs_lease.sql\`（同一来源同时只有一条 running）、\`307_build_jobs_intent.sql\`（pending 幂等键）。**原记录「无 Go 侧引擎」已过时** | — | PIPE-3（已完成） |
| PIPE-3 | 依赖 fan-out 与 stale 状态机（\`direct_content\` / \`content_collection\` / \`content_template\` / \`site_setting\` 等） | \`03-pipeline.md\` §8.1（L404-426）、§8.2（L428-490）、\`05-implementation-plan.md\` 阶段 4 验收（L216） | **主体已落地（2026-09，2026-09-26 复核遗留清单）**：迁移 071（反查索引 + kind 扩展）、依赖记录落库（page + presentation 两侧）、\`direct_content\`/\`content_collection\` 精确反查（不再全站标记）、受影响产物自动重建 + 已发布页面自动回写。**遗留（本轮逐条核过）**：① \`media\` 与 \`site_setting\` **未登记** —— \`pipeline.DepKindMedia\`（\`internal/pipeline/dependency.go:44\`）与 \`DepKindSiteSetting\`（\`:53\`）全仓只有常量定义、**无任何发射点**（\`rg DepKindMedia\` / \`rg DepKindSiteSetting\` 两条命令均只命中定义处）；② stale 仍是 \`bool\`（\`internal/module/page/model/page_model.go:66\`）。**原记录两条遗留已过时**：「menu 未登记」→ 现已接通（\`internal/routers/assembly_publish.go:527-540\`：\`SetMenuStaleDispatcher\` + \`pipeline.NewMenuStaleAdapter(fanout)\`，未提供注入点时启动 panic fail-fast；实现见 \`internal/module/navigation/service/navigation_stale.go\`）；「page 侧未登记 \`content_template\`、仅 presentation 侧」→ page 侧同样登记（\`internal/module/page/service/page_dependency.go:265\` 经 \`pipeline.StructureSlotDependencies\` 登记 \`content_template:{id}\`，presentation 侧见 \`presentation/service/presentation_render.go:392\`） | 高 | 无 |
| PIPE-4 | 插件生效流水线（上传 → 校验 → 入库 → **asynq 入队重建受影响页面** → 原子切换） | \`06-plugin-system.md\` §10（L266-279） | 部分完成：安装/编译/原子切换已落地；\`asynq\` 目前仅用于媒体变体（\`media_variant_task.go\`），未接构建 | 中 | PIPE-3 |
| PIPE-5 | Redirect Artifact（\`redirect.json\` + \`route_kind=redirect\` + Static Server 301） | \`03-pipeline.md\` §4.4（L222-247） | 文档滞后-代码已有：\`pipeline/artifact.go\` \`RedirectDirective\`、\`store.go\` \`PutRedirect\`、\`publication.go\` 读取、\`page_publish.go:595\` \`ensureRedirectRoute\`（改 URL 时生成 301） | — | 无 |
| PIPE-6 | AccessGuard（密码保护 / 登录用户可见，Manifest 标记 + 静态守卫页） | \`0-A2-page-routing-meta.md\` §2.2（L37-38）、§5（L84） | 未开始（无 \`AccessGuard\` 相关代码） | 中 | 访客账号域（BIZ-3）做「登录可见」 |
| PIPE-7 | 定时上下线（Scheduled Publishing） | \`0-A2\` §2.2（L39-41）、§5（L85） | 未开始（无 \`internal/task\` 目录、无 scheduled 字段） | 中 | 无 |
| PIPE-8 | Head / Body 代码注入（页面级统计与营销脚本） | \`0-A2\` §2.3（L45-48）、§5（L86） | 未开始（无 \`HeadScripts\`/\`BodyScripts\`） | 中 | 需安全白名单评审 |
| PIPE-9 | Robots meta（\`index/noindex\` + \`follow/nofollow\`） | \`0-A2\` §2.1（L28-30）、§5（L83） | ✅ **已落地（2026-09 复核；2026-09-26 补行号）**：\`PageSettings.SEO\` 有 \`robotsIndex\`（\`internal/builder/settings.go:181\`）与 \`robotsFollow\`（\`:183\`）两个字段；构建期由 \`internal/builder/seo_head.go:38\` 的 \`robotsContent\` 组装、\`:98\` 输出 \`<meta name=\"robots\">\`（两项都取默认 index/follow 时**返回空串**，默认页面的产物字节与没这个功能时逐字节一致）；工作台页面设置面板有「搜索引擎收录」与「链接跟踪」两组按钮。**本次回填修正**：上一版说「两个字段都没有」是盘点时的滞后 | — | 无 |
| PIPE-10 | Meta Keywords 字段 | \`0-A2\` §2.1（L27） | 未开始（现代搜索引擎已不看重，可低优先） | 低 | 无 |
| PIPE-11 | OGTitle / OGDescription 独立字段 | \`0-A2\` §2.1（L32）、§5（L83） | 文档滞后-代码已有：\`internal/builder/seo_head.go:102\`（\`og:title\`）/ \`:106\`（\`og:description\`）已输出（取自页面 SEO 标题/描述）。**2026-09-26 行号复核：原记录引的 \`:46/50\` 已失效** | — | 无 |
| PIPE-12 | 一键设为全站首页（根路径 \`/\`） | \`0-A2\` §1（L13）、§5（L80） | **部分完成（2026-09-26 复核）**：**能力可达但无专用动作** —— 根路径本身已放行（\`pkg/pathkit/pathkit.go:79\` 显式 \`if raw == "/" { return "/", nil }\`），因此「新建页面 / 改 URL 填 \`/\`」即可达成首页效果；**仍缺**的是专用「一键设为首页」动作与页面设置面板项（\`internal/templates/fragments/settings_panel.html\` 全字段无此项）。**原记录「设置面板与 service 无该动作」的判断对「动作」成立、对「能力」不成立** | 中 | 无 |
| PIPE-13 | CDN 缓存清理（激活后清理旧路径边缘缓存） | \`0-A2\` §3（L67）、§5（L87） | 未开始 | 低 | 部署层 |

---

## 6. 商品与业务模块

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| BIZ-1 | \`commerce\` 本体模块（商品/分类/SKU/库存/订单状态机/支付回调验签/优惠码幂等核销） | \`06-A\` §4（L70-83）、\`06-B\` 变更记录 v2（L70-81） | **商品 / 分类 / SKU / 库存已落地**（\`internal/module/product\` 与 \`product/inventory\`，#5–#32 —— v2 决策里的 \`commerce\` 实际以 \`product\` 命名）；**订单已落地**（\`internal/module/order\`，建单 / 状态机 / 取消 / 退款 / 查询 + 归因冗余，迁移 135/136）；**购物车与访客结算已落地**（\`internal/module/cart\`：HMAC 签名 cookie 购物车 + 六个 anonymous 片段能力 + 结算四步；支付通道当前为**模拟 PayPal**，零迁移）；**归因采集链路已落地**（构建期内联 \`track.js\` + 下单定格）；**支付回调验签已落地**（`cart/inbound/http` 的 `POST /payment/callback`：先验签后解析 + 常量时间比较 + 金额核对 + 幂等落账）；**优惠码已落地**（迁移 141/142/143：口径 / 门槛 / 次数 / 每人限次 / 时间窗 + **与建单同事务的幂等核销** + 后台页 `/admin/coupons`）；**后台订单管理页已落地**（`/admin/orders`）；**访客账号表单已片段化**（login / register / forgot / reset / account 五个片段 + `core.userForms` 组件：表单带 CSRF token 必须现渲染，槽位从此可指向作者自建的登录页 / 个人中心页）；**后台备注可编辑已落地**（`POST /api/order/note`，只开放备注一列、不写流转链，迁移 146）；**退货入库已落地**（迁移 144/145：按订单项申请 → 审核 → **先入库、后退款**；收货门闩保证重复点击不会二次入库；全额才转订单已退款；后台页 /admin/returns，访客侧片段 returnRequest）；**访客自助订单查询已落地**（`ordersList` / `orderDetail` 片段 + `core.orderList` 组件，归属由 SQL 条件收口）。**2026-09-26 复核补充「仍缺」**（原读数未列，避免读成整条已闭合）：① 真支付网关 —— \`internal/module/cart/outbound/\` 下只有 \`mockpaypal/mockpaypal.go\`；② webhook 后台界面 —— \`rg webhook --glob '*.html' internal/templates\` 零命中 | 高 | 已拍板方向，可先行 |
| BIZ-2 | 首批商品 capability（\`productList\` / \`searchResults\` / \`productAvailability\` / \`productLivePrice\` / \`cartSummary\` 接真实数据 / \`cartAdd\`） | \`04-A-dynamic-capabilities.md\` §7（L128-142）、\`04-B\` §6 索引（L152） | ✅ **全部已落地（2026-09 复核）**：\`runtimefragment/\` 下已注册 \`cartSummary\` / \`cartView\` / \`cartAdd\` / \`cartSetQty\` / \`cartClear\` / \`checkout\`（cart.go）、\`productList\`（product_list.go）、\`productVariantAvailability\`（variant_availability.go）、\`productLivePrice\`（live_price.go）、\`searchResults\`（search_results.go）、\`bundleConfigurator\`（bundle_configurator.go）。**本次回填修正**：上一版把 \`searchResults\` / \`productLivePrice\` 记为未开始，是盘点时的滞后 | — | BIZ-1 |
| BIZ-3 | 会员 membership（访客账号领域，admin 之外另建） | \`06-A\` §3 表 #6（L60）、\`AGENTS.md\` 命名约束 | **账号底座已落地（issue #36）**：\`internal/module/user\` 提供注册 / 邮箱验证 / 登录 / 密码重置 / 账号中心（资料 / 偏好 / 登录设备），与 \`admin\` 完全隔离；**会员等级与权益未开始** | 中 | 无 |
| BIZ-4 | 积分 points（流水对账/幂等） | \`06-A\` §3 表 #7（L61） | 未开始 | 低 | BIZ-3 |
| BIZ-5 | 评论 comments（独立模块，多实体挂载） | \`06-A\` §3 表 #9（L63） | **转本体（2026-09-26 决定；原为 L1 轻轨插件、曾随插件生态冻结）**：因**文章与商品（博客 = 文章，不是第三种实体）多方都要**，定为独立模块 \`comment\` —— 多态挂载（实体类型白名单由**拥有该实体的模块**声明，照 \`pkg/datarule\`「白名单由拥有者声明」的既有原则）；审核 / 限流 / 防刷 / 审核后台 / i18n **一处实现**，差异化规则（如商品评论需已购买）由消费方经**收窄端口**提供（照 \`masterdata\`「调用方递快照」的形态）；**列表走 Runtime Fragment、不进构建期产物**（静态产物对所有人是同一份字节，而评论是实时数据） | 中 | BIZ-3（访客身份） |
| BIZ-6 | 站内搜索 capability（\`searchResults\`，白名单排序） | \`06-A\` §3 表 #10（L64） | ✅ **已落地（2026-09 复核）**：\`runtimefragment/search_results.go\` 注册 anonymous GET 片段 \`searchResults\`（参数 q / projectId / limit），经收窄只读端口 \`SetContentSearchProvider\` / \`SetProductSearchProvider\` 取数（未注入时降级为空片段而不是 500）；内容检索按原文匹配，**有译文的文章用中文关键词搜不到**（多语言检索属后续票） | — | 无 |
| BIZ-7 | 邮件 smtp（配置 + 队列，表单/会员通知依赖） | \`06-A\` §3 表 #11（L65） | **已落地（issue #37 / #38）**：\`internal/module/mail\`（发信账号 / 模板 / 联系人 / 群发 / 自动化 / 事务发送 / 追踪退订），队列未启用时 \`SendTemplate\` 降级同步发送；\`user\` 模块的验证信与重置信已实际接入 | 低 | 无 |
| BIZ-8 | 统计 analytics（GA4/gtag 注入 + 访问计数） | \`06-A\` §3 表 #5（L59） | ✅ **全部已落地（2026-09 复核）**：① 归因采集 —— 构建期内联 \`track.js\`（UTM 家族 / 广告点击 id / referrer / 会话 / 浏览轨迹 → 签名 cookie → 下单定格进 \`orders.attribution\`）；② GA4/gtag 注入 —— 真源 \`projects.settings.ga4MeasurementId\`，构建期经 \`builder.WithGA4MeasurementID\` 进 head（空值 / 非法值零字节注入）；③ 访问计数 —— \`internal/module/analytics\`（公开面 \`POST /analytics/collect\` 只写一条记录 + 后台只读 \`GET /api/analytics/summary\`，落库只存匿名派生值），后台页 \`/admin/analytics\`，迁移 147/148/149。**本次回填修正**：上一版把后两项记为未开始，是盘点时的滞后 | — | PIPE-8 |
| BIZ-9 | 商品数据进可视化（\`content:product\` 集合源本体化注册） | \`06-A\` §4 前置硬骨头 1（原引 L86 已随该文档重写失效）、\`06-B\` 变更记录 v2 理由 4（L79-80） | ✅ **已落地（2026-09-26 复核）**：\`content:product\` 已由商品域**本体化注册**、不是只通通用通道 —— \`internal/module/product/service/collection_resolver.go:46\` / \`:146\` \`ResolveCollection\`（解析 \`content:product\`，含字段白名单、\`status\` 等值过滤、\`CollectionPager\`），契约 \`internal/module/product/contract/data_source.go:22\`（\`source.CollectionResolver\`），取数 \`internal/module/product/model/product_model.go:524\` \`ListForCollection\`（issue #9；RLS 经 \`rls.InProjectScope\`）；内容侧对照 \`internal/module/content/service/collection_resolver.go:26\`。**原记录「缺商品实体本身」已过时** —— 原记录的「文档滞后-部分已有」口径已升级为「已落地」 | — | BIZ-1（已完成） |

---

## 7. 基础设施

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| INF-1 | CMS 内容**后台管理页** + 文章编辑页 | \`05-implementation-plan.md\` 阶段 4 验收（L215）、\`09-session-handoff.md\` §1.4（L45） | ✅ **已落地（2026-09，2026-09-26 复核路径）**：\`/admin/articles\`（列表 + 新建 + 删除 + 发布状态；路由 \`internal/module/content/inbound/http/article_router.go:50\` 起）、\`/admin/articles/edit\`（Trix 富文本 + 摘要 + 封面 + SEO 字段（2026-09-30 起只剩主关键词，标题 / 描述已与标题 / 摘要合并）+ 评测侧栏 + 发布区块）、侧栏入口。**原记录引用的 \`nav_menu.go\` 不存在**（\`rg --files\` 过滤 \`nav_menu\` 仅命中 \`public/migrations/224_admin_nav_menu_rebuild.sql\`）—— 侧栏菜单是 **DB 驱动**、不是代码配置：树由 \`sys_menus\` seed 提供（文章管理见迁移 \`public/migrations/150_article_menu.sql\`，全量重建见 \`224_admin_nav_menu_rebuild.sql\`），渲染入口 \`internal/web/shell/nav.go:56\` \`BuildNav\`（\`buildNavNodes\` \`:89\`）。写操作复用既有 \`content:*\` 权限点（**未新增权限点**），正文落库前过 \`core.SanitizeRichHTML\`（\`internal/builder/core/richtext.go:94\`），保存即触发依赖扇出（引用它的页面与已发布实例标记待重建） | — | 无 |
| INF-2 | CMS 实体变更 → 自动派生 DocumentSnapshot → 自动发布 | \`05\` 阶段 4 任务（L208）、\`03-pipeline.md\` §8.2 典型 fan-out（L462-472） | ✅ **两侧均已落地（2026-09）**：page 侧（PIPE-3）内容变更 → 精确反查 → 自动重建 + 已发布页面自动回写；presentation 侧在 DDL 对齐修复后接入同一 fan-out（内容变更 → 精确反查 → 自动重建并重新发布，\`presentation_dependencies\` 落库） | — | 无 |
| INF-3 | 02-B 媒体中心三表与实现不一致 | \`02-B-media-center.md\` §7（L85-88：\`media_asset\`/\`media_asset_variant\`/\`media_reference\`） | ✅ **已修正（2026-09）**：\`02-B\` §7 改为实际三表，并说明 \`media_asset\`/\`media_asset_variant\`/\`media_reference\` 仅为 \`init_schema.sql\` 建表、无任何 Go 引用的遗留；§6 实现映射同步改为真实代码位置（原引用的 \`internal/builder/media/\` 不存在） | — | 无 |
| INF-4 | 媒体下载接口（单图 + 批量 zip） | \`media-variants-recon.md\` §C（L200-204） | 文档滞后-代码已有：\`internal/module/media/inbound/http/media_router.go:48\`（GET \`/download\`）/ \`:49\`（GET \`/download/batch\`）。**2026-09-26 行号复核：原记录引的 \`:37-38\` 已失效** | — | 无 |
| INF-5 | qiniu 存储不支持批量打包（一期限定 \`storage_type=local\`） | \`media-variants-recon.md\`（L204） | 未开始（已知限制） | 低 | 无 |
| INF-6 | 变体异步生成（goroutine / asynq + \`pending\` 状态） | \`media-variants-recon.md\` §A（L196） | 部分完成：\`media_variant_task.go\` 已接 asynq 任务；上传同步生成仍在 | 低 | 无 |
| INF-7 | \`GenerateVariants\` 导出为契约方法（存量回填 / 重新生成按钮） | \`media-variants-recon.md\` §B（L198） | 文档滞后-代码已有：\`internal/module/media/contract/media_service.go:61\` 已导出 \`GenerateVariants(ctx, attachmentID uint64) ([]mediadto.VariantResp, error)\`。**2026-09-26 行号复核：原记录引的 \`:33\` 已失效** | — | 无 |
| INF-8 | 后台「菜单管理」页未收录 \`/admin/navigations\` | \`09-session-handoff.md\` §4（L390） | ✅ **已落地（2026-09-26 复核）**：\`/admin/navigations\` **已在菜单树里** —— seed 迁移 \`public/migrations/224_admin_nav_menu_rebuild.sql:149\`（\`('导航菜单', '内容', '/admin/navigations', 'navigation:list', 7)\`，同文件 \`:99\` 是目录归属），路径别名 \`internal/web/shell/nav.go:162\`（\`/admin/navigations/translations\` → \`/admin/navigations\`）；页面本身在 \`internal/module/navigation/inbound/http/navigation_page_handle.go:3\`（注释即「前台导航菜单管理页（/admin/navigations）」），模板 \`internal/templates/admin/navigation/navigations.html\`。**原记录「未开始（侧边栏走代码配置）」两处都过时** —— 侧栏菜单是 **DB seed 驱动**（渲染 \`internal/web/shell/nav.go:56\` \`BuildNav\`），不是代码配置；且该页已收录 | — | 无 |
| INF-9 | 复刻页外链图片本地化（走媒体库，顺带验证变体管线） | \`09-session-handoff.md\` §4（L383） | 文档滞后-代码已有：同文 §3（L270-274）已记录完成（10 张图本地化，仅剩 \`<a>\` 链接保留外链） | — | 无 |
| INF-10 | 访客面零 JS 校验 / 依赖 fan-out E2E 验收清单保持规划态 | \`04-runtime-and-delivery.md\`（L148） | 文档滞后：依赖 fan-out 见 PIPE-3；Runtime Fragment / Client Enhancement / PresentationInstance / ContentTemplate 均已落地 | — | 见 PIPE-3 |
| INF-11 | 页面设置面板缺 Robots / Keywords 字段（与 PIPE-9/PIPE-10 同源，此处记录 UI 侧） | \`0-A2\` §2.1、\`templates/fragments/settings_panel.html\` | **部分完成（2026-09 复核；2026-09-26 补行号）**：Robots 侧已落地 —— 面板有「搜索引擎收录」（index/noindex）与「链接跟踪」（follow/nofollow）两组按钮（\`internal/templates/fragments/settings_panel.html:36\` / \`:43\`；见其 \`data-wb-setting=\"settings.seo.robotsIndex\" / robotsFollow\`），后端 \`settingsViewOf\` 也解析了这两个字段；**Keywords 仍未做**（同 PIPE-10，\`builder.SEO\` 结构里没有 keywords，只有 \`SecondaryKeywords\`（\`internal/builder/settings.go:168-169\`））。**本次回填修正**：上一版写「面板仅 title/description/…」漏了 robots 两组 | 低 | PIPE-10 |

---

## 8. 文档一致性（文档滞后 / 互相矛盾）

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| DOC-1 | \`01-overview.md\` 模块表把 \`content\`/\`contenttemplate\`/\`presentation\`/\`blueprint\`/\`component\`/\`navigation\`/\`runtimefragment\` 全部标为「规划 0-X」 | \`01-overview.md\` §模块目录树（L252-258）、模块表（L281-288） | ✅ **已修正（2026-09）**：\`01-overview\` 目录树与模块表已按 \`internal/module/\` 回填为「已实现」（含补 \`plugin\` 行）；\`component\` 标注「未落地，语义由 \`block.reuse_mode\` 承担」 | 高 | 无 |
| DOC-2 | \`01-overview.md\` 把「多语言静态发布（i18n）」列为**非目标/不做** | \`01-overview.md\` §1.3（L112） | ✅ **已修正（2026-09）**：\`01-overview\` §1.4 已把「多语言静态发布（i18n）」移出非目标并附代码依据；事实来源 \`config.yaml:66 site_lang_prefix: true\`、迁移 055/056/061–066、\`06-D\` §15 | 高 | 无 |
| DOC-3 | \`05-implementation-plan.md\` 阶段 4–7 全部标「待开始」 | \`05\` §当前状态表（L54-63） | ✅ **已修正（2026-09）**：\`05\` 状态表改为「阶段 4 部分完成 / 阶段 5 部分完成（component 未落地）/ 阶段 6 已完成 / 阶段 7 部分完成」，并逐条勾选/标注任务与验收门禁 | 高 | 无 |
| DOC-4 | \`04-runtime-and-delivery.md\` 称「依赖 fan-out、Runtime Fragment、Client Enhancement、PresentationInstance、ContentTemplate 仍属规划」 | \`04\`（L148） | 文档滞后：仅依赖 fan-out（PIPE-3）属实，其余四项已落地 | 中 | 无 |
| DOC-5 | \`02-D-reusable-assets.md\` 把 ContentTemplate 标为「规划/部分实现（0-A2）」 | \`02-D\` §表（L34） | 文档滞后：\`contenttemplate\` 模块已落地 | 中 | 无 |
| DOC-6 | \`02-D\` 称 workbench 块克隆「前端接线待做」 | \`02-D\` §12（L306） | 文档滞后：见 CMP-10 | 中 | 无 |
| DOC-7 | \`jet-and-go-libs-plan.md\` 待办表 P1「三层 Set」标待办 | \`jet-and-go-libs-plan\`（L211、L216） | 文档滞后：见 CMP-2 | 中 | 无 |
| DOC-8 | \`09-session-handoff.md\` §4 多条遗留已修复但未更新 | \`09\` §4（L383-390） | ✅ **已修正（2026-09）**：\`09\` §1.3/§1.6/§4 已按代码统一四处（\`MaxRichLen\`、idiomorph、容器 flex 面板、外链图片本地化），并新增 §5 回填记录表 | 中 | 无 |
| DOC-9 | \`06-A\` 把「L2 CollectionSource 落地」列为**硬依赖 0-A2 content** 的前置硬骨头 | \`06-A\` §4（原引 L86 **已随该文档重写失效**）、§5 依赖图（原引 L96 同） | **文档滞后（2026-09-26 复核）**：\`content\` 模块的集合源（\`content:{type}\`）已落地，且**商品侧同样已本体化注册**（见 BIZ-9，已核销）；仅插件自有 schema 通道（\`plugin:{id}.{table}\`）未做 —— 该项随**插件生态整组冻结**（见 §2），**不再是待办**。原记录引用的 \`06-A:86\` 行号已失效（该文档已重写为冻结版，§4 现位于 L119） | — | 见 §2（PLG 整组冻结） |
| DOC-10 | \`02-E\` §6 执行计划未标注各步骤落地状态 | \`02-E\` §6（L104-112） | 文档滞后：步骤 1/3/4/5 已落地（scoring / workbench 评分面板 / meta Profile / sitemap），仅步骤 2（部分）、6、7 未完成 | 中 | 无 |
| DOC-11 | \`06-D\` §13 阶段表与 §15 实施记录需对齐（P4b/P5b/P5c 已落地） | \`06-D\` §13（L887-897） vs §15.11/§15.12 | 文档滞后：§13 未回填「已落地」标记（§15 已详载） | 中 | 无 |
| DOC-12 | \`03-A-workbench.md\` §2「明确不做（03-B 前端工作台）」实际已实现 | \`03-A-workbench.md\` §2（L47-51） | 文档滞后：workbench 前端（四区布局/拖拽/快捷键/右键菜单/撤销）已落地并经浏览器审计（\`09\` §3） | 中 | 无 |
| DOC-13 | \`component\` 模块概念漂移未记录（文档写 Global Component 模块，实现用 \`block\` 的 \`reuse_mode\`） | \`01-overview\` 模块表（L286）、\`05\` 阶段 5（L230）、\`02-domain\` §4.2/§4.3（L406-472） vs \`AGENTS.md\` 模块表（\`block\` 承担全局块） | ✅ **已修正（2026-09）**：\`02-domain\` §4.2 补「实现由 \`block.reuse_mode\` 承担」及差异说明，§4.3 标注三策略未落地；\`01-overview\` §1.1/§4/§5 同步标注 | 中 | CMP-1 |

---

## 9. 建议推进顺序

按依赖关系而非并列罗列。**前置未被解掉的，后面做了也白做**。

### 第 0 步：先修文档口径（半天，零代码风险）

✅ **已完成（2026-09）**：\`DOC-1 / DOC-2 / DOC-3\` 已修（\`01-overview\` 模块表/目录树/非目标、\`05\` 状态表与阶段 4–7 任务验收），并同步修正 \`DOC-8\` / \`DOC-13\` / \`INF-3\`。
仍待修：\`DOC-4\`~\`DOC-7\`、\`DOC-9\`~\`DOC-12\`（以及其余「文档滞后」条目中尚未回填者）。

### 第 1 步：解锁已投入但被开关关掉的能力（1 天）

✅ **已解决（2026-09 复核；2026-09-26 修键名与行号）**：**结论不变（多语言已启用）**，但键名与行号已变 —— 现为 \`config.yaml:88\` \`site_lang_url_mode: default_plain\`（\`:87\` 注释保留旧键 \`site_lang_prefix\` 的兼容识别），样例文件 \`config.yaml.example:122\`；多语言内核、\`page_publications\`/\`page_stagings\`/\`project_locales\`、hreflang、语言切换器、翻译工作台全部已落地并有端到端测试。
（原记录「\`config.yaml:66\` 与 \`config.yaml.example:65\` 均为 \`i18n.site_lang_prefix: true\`」的**键名与行号均已过时**；「默认 false」的旧结论更早已过时。）
这是全表性价比最高的一条：一行配置 + 一轮回归，就能让 P2–P5c 的投入变成可交付能力。
先决条件：\`I18N-12\`（禁用语言路由清理）—— **已于 2026-09-26 核销落地**（见该行），不再阻塞。

### 第 2 步：阶段 4 收尾——自动发布主链（原 3–5 天，**2026-09-26 复核：主体已不需要**）

**本步已完成，不再是待办（2026-09-26 复核）**：\`PIPE-3\`（依赖 fan-out + stale 状态机）**主体已落地**（迁移 071 + page/presentation 两侧依赖落库 + 精确反查 + 自动重建与回写；遗留只剩 \`media\`/\`site_setting\` 未登记与 stale 仍为 bool，见 PIPE-3 行）；其下游同样已落地：\`INF-2\`（presentation 自动重建）✅、\`PIPE-2\`（构建队列引擎）✅、\`I18N-2\`（块内取词）✅。**原记录「\`PIPE-3\` 未做 ⇒ 只能手动重建 ⇒ 阶段 4 验收门禁无法通过」三句全部过时**（对应 §10.3 第 3 条，已同步修正）。
剩余真正待做的只有 \`PIPE-1\` 的**残留部分**（\`tombstone\`；保留期 / \`gc_pending\` / 内容对象回收均已落地）。
\`INF-1\`（CMS 后台内容管理页 + 文章编辑页）✅ **已完成（2026-09）** —— 它只依赖现有 \`content\` API，落地后同时解掉 \`SEO-10\`（文章评分入口）。\`I18N-7\`（CMS 翻译）现在有了入口（文章编辑页），可以接着做。

### 第 3 步：插件生态 —— **明确不做（冻结，2026-09-26）**

**本条已撤销，不再是排期项**：插件生态**整组冻结**，不在当前路线内 —— 依据 [06-A-plugin-ecosystem-roadmap.md](./06-A-plugin-ecosystem-roadmap.md) 的「当前状态（冻结 · 明确不做）：插件生态不在当前路线内」（L7）与「决策记录（2026-09-15）：插件生态延后」（L28，**重启条件三条**在 L40-41）。
原计划（\`PLG-1\` + \`PLG-2\` 的 \`collections.json\` 通道 → 解锁 \`PLG-9\` / \`PLG-10\`；\`PLG-5\` 签名先于任何第三方分发；\`PLG-6\` 受限事务 API 可后置）**全部作废**，保留在这里仅作历史记录，**排期时不要计入**。

### 第 4 步：商品本体模块（按需启动）

\`BIZ-1\`（\`commerce\`）已由 \`06-B\` v2 拍板为**本体模块**，且 \`BIZ-9\`（\`content:product\` 进可视化）**已落地**（2026-09-26 核销），是当前阻力最小的新领域。
它同时解锁 \`SEO-9\`（Product JSON-LD）与 \`BIZ-2\`（商品 capability）—— **这两项均已于 2026-09-26 核销落地**（见各自行）。
注意：库存/订单/支付必须留在 Go 模块内，不要因为「插件化更快」而退回到 \`06-B\` 决策 1 已否决的路径。

### 第 5 步：上线前的发布管线补全

\`PIPE-6\`（AccessGuard）、\`PIPE-7\`（定时上下线）、\`PIPE-8\`（Head/Body 注入）、\`PIPE-10\`（Meta Keywords）、\`PIPE-13\`（CDN 缓存清理）——这些是「静态站点对外营业」的必备项，但不是其他工作的前置，可排在功能主线之后。
**2026-09-26 复核后的本步真实范围**：\`PIPE-9\`（Robots meta）**已落地、已从本步移出**；\`PIPE-1\` 只剩残留（\`tombstone\`；保留期 / \`gc_pending\` / 内容对象回收均已落地，见该行）；\`PIPE-12\`（一键设为首页）只剩「专用动作 + 设置面板项」（根路径能力已可达）。

### 可长期挂账（低优先）

\`CMP-3\`（抽库）、\`CMP-12\`（服务端持 AST）、\`I18N-8\`（AI 翻译）、\`I18N-13\`~\`I18N-18\`、\`INF-5\` 等。**2026-09-26 移出两项**：\`PLG-7\`（第三轨）与 \`PLG-8\`（插件市场）**不属「挂账」而属「冻结」**（见 §2，不要按挂账项排期）；\`INF-8\`（菜单管理页收录 \`/admin/navigations\`）**已落地核销**（见该行）。

---

## 10. 盘点中发现的其他问题

### 10.1 文档互相矛盾（需人工裁决）

> 第 1–4 条均已于 2026-09 按代码更正（见第 8 节 DOC-1/DOC-2/DOC-13/INF-3 的「已修正」状态）。

1. **\`01-overview\` §1.3 把 i18n 列为非目标，\`06-D\` 已实现整套多语言** —— 两文档都未标注对方状态（\`DOC-2\`）。
2. **模块口径两套**：\`AGENTS.md\` 与 \`internal/module/CLAUDE.md\` 写「已实现」，\`01-overview\` 写「规划 0-X」（\`DOC-1\`）。
3. **\`component\` 模块概念漂移**：\`02-domain\` §4.2/§4.3 详细描述 Global Component 的版本与 \`immutable/auto-update/pinned\` 策略，\`AGENTS.md\` 用 \`block\` 的 \`reuse_mode\` 替代；两者关系（替代 / 并存 / 待建）无任何文档记录（\`DOC-13\`）。
4. **\`02-B\` 表结构描述与实现不一致**：文档写 \`media_asset\`/\`media_asset_variant\`/\`media_reference\` 三表，实现是 \`sys_attachment\`/\`sys_file_category\`/\`sys_media_variant\`（\`INF-3\`）。

### 10.2 过时描述（同文档内自相矛盾）

1. ✅ **已修正（2026-09）**：\`09-session-handoff.md\` 同一文件内 §1.4 说「未做」、§3 说「已完成」的 4 处（\`MaxRichLen\`、idiomorph、容器 flex 面板、updvape 图片本地化）已按代码统一，并在该文档新增 §5 回填记录。
2. \`06-D\` §13 阶段表与 §15 实施记录并存但未互标（\`DOC-11\`），读者容易按 §13 判断进度。
3. \`jet-and-go-libs-plan.md\` 的「待办优先级建议」表已部分过时（\`DOC-7\`），但该表是判断 Jet 改造进度的主要入口。

### 10.3 结构性观察（非缺陷，供决策参考）

1. ~~**「已落地但未启用」是当前最大浪费**：i18n 语言前缀开关（\`I18N-1\`）是典型——功能完整、测试齐备、默认关闭。~~ ✅ **已解决（2026-09）**：开关已启用（\`config.yaml:88\` \`site_lang_url_mode: default_plain\`；原引 \`:66\` 的旧键已换形态，见 I18N-1），本条不再成立。
2. **插件体系已整组冻结（2026-09-26）**：原记录「半成品最集中的区域（manifest 的 \`collections\`/\`admin\`/\`fragments\`/\`signature\` 四段全部缺失，插件只能做纯展示组件 L0，L1 数据层与 L2 内容源停在设计文档）」的**事实面仍成立**，但**不再作为缺口或待办** —— 插件生态不在当前路线内，重启条件见 \`06-A-plugin-ecosystem-roadmap.md\` 决策记录（L28，条件在 L40-41）。**盘点与排期都不得把它算作待补项**（见 §2）。
3. ~~**阶段 4 是唯一真正卡住主链的阶段**：\`PIPE-3\`（依赖 fan-out）未做，导致 content/presentation 只能手动重建，\`05\` 阶段 4 的验收门禁（「内容变更触发自动重建并发布」）无法通过。~~ ✅ **已解决（2026-09-26 复核）**：\`PIPE-3\` 主体已落地（精确反查 + 自动重建 + 已发布页面回写），\`INF-2\` 两侧均已接通，「内容变更触发自动重建并发布」这条门禁已可满足；本条不再成立（§9 第 2 步已同步修正）。
4. **商品域已两次改向**（插件 → 重轨 → 本体），建议在动工前把 \`06-B\` 变更记录与 \`02-domain\`/\`01-overview\` 的模块描述统一，避免第三次返工。

---

## 11. 重构遗留的测试覆盖缺口

来源：2026-09-19 修复 `scripts/check-workbench.sh`（它引用的 `internal/module/dashboard` 已不存在）时，
用 `git show -M --summary e7405ca9`（*refactor(web): 解体 dashboard 巨型模块，页面按归属回到各业务模块*）
逐项比对该次重构的 delete / rename 明细，发现**三个能力的测试被 delete 且无 rename 替代**。
它们与门禁修复本身无关，但都是「代码还在、断言没了」——**重构不会让它们报错，只会在某天回归时才发现**。

| # | 能力 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| COV-1 | workbench 编辑目标注册表无断言 | `internal/module/workbench/inbound/http/workbench_target.go:126`（`EditTargetFor`）/ `:132`（`WorkbenchTargets`），注释自称「测试与前端契约共用」；原测试 `dashboard/inbound/http/workbench_target_test.go`（`TestWorkbenchTargetRegistry`）随 `e7405ca9` 删除且无 rename | 未开始（全树 grep `EditTarget` / `WorkbenchTargets` 在 `*_test.go` 中零命中） | 中 | 无 |
| COV-2 | `internal/web/shell/nav.go` 整包无断言 | `nav.go:56` `BuildNav` / `:89` `buildNavNodes` / `:113` `firstNavPath` / `:126` `containsActive` / `:160` `NavPathAlias` / `:173` `NavPathFor`；旧 `nav_path_alias_test.go`（`TestNavPathFor`）与 `nav_normalize_contract_test.go`（`TestDashboardNormalizePathMatrix`）随 `e7405ca9` 删除且无 rename | 未开始（`internal/web/shell` 现只有 `bulk_test.go` / `errors_test.go` / `notice_test.go`）。这些是**纯函数**（路径归一、别名映射、父级展开态计算），按 CQ-020 二层策略正属「模块内就近单测」 | 中 | 无 |
| COV-3 | 页面路由渲染覆盖台账无替代 | 旧 `public/test/dashboard/feature/dashboard_route_coverage_ledger_test.go`（`TestDashboardRoutesHaveRenderTests`）随 `e7405ca9` 删除 | 未开始（全树 grep `RoutesHaveRenderTests` / `routeCoverage` 零命中）。台账的语义是「每个后台路由都有对应的渲染测试」——它消失意味着**新增页面漏测渲染不再有系统级提醒** | 中 | 无 |
| COV-4 | ~~语言切换的开放重定向防护无覆盖~~ **已核实为误判** | 旧 `dashboard/inbound/http/lang_handle_test.go`（`TestLangSwitch` / `TestLangSwitchOpenRedirectGuard`）确随 `e7405ca9` 删除 | ✅ **覆盖已转移，不是缺口（2026-09 核实）**：判据在重构中**单源化**到 `internal/web/shell/notice.go:151` 的 `LangRedirectPath`，由 `notice_test.go:112` 的 `TestLangRedirectPathIsTheOnlyCriterion` + `TestLangRedirectConverges` 断言，消费侧另有 `admin/inbound/http/admin_notice_test.go:190-242` 的对照用例（含 `//evil.example.com` 这类反例）。运行时入口 `AdminLangSwitch`（`admin/inbound/http/admin_pages_handle.go:1158`）经 `adminSafeLangRedirect` 调同一份判据。**不要另派工补这一条** | — | 无 |

> **COV-4 的判定过程本身是个教训**：初次盘点按**函数名字面量**（`LangSwitch`）grep，得出「无替代测试」；
> 而重构把那项能力**改名并拆到另一层**（`AdminLangSwitch` + `shell.LangRedirectPath`）。
> 判断「测试是否丢失」要按**能力**追，不能按**名字**追 —— 同类误判之所以没在 COV-1/COV-2 上发生，
> 只是因为那两处的函数名恰好没改。**盘点结论在下发补工之前必须先按能力复核一遍。**

---

## 12. 管理面「菜单 / 按钮授权」的两处同类缺口（2026-09-19 盘点）

后台配置「谁能用哪些目录 / 菜单 / 按钮」原本有两组接口，界面状态在这次盘点前是一样的 ——
都是「服务端齐、前端零」：

| 能力 | 接口 | 界面 | 状态 |
|---|---|---|---|
| 角色分权：这个**角色**能用哪些目录 / 菜单 / 按钮 | `GET /api/role/menu/list`、`POST /api/role/menu/save` | 角色列表行的「权限分配」抽屉（片段 `GET /admin/roles/permissions/drawer?role_id=N`，保存 `POST /admin/roles/permissions/save`） | ✅ **已落地**（本批：页面 `97502a4e`，页面路由的权限门 `4c9f774d`；后由独立页面改为抽屉，`GET /admin/roles/permissions` 已不存在） |
| 管理员直接额外授权：这个**管理员**在角色之外额外能用哪些菜单 | `GET /api/admin/menu/list`、`POST /api/admin/menu/save` | 无 | ❌ **仍无界面**（待决策，见下） |

判定方式是按**能力**检索调用方（不是按函数名）：`role/menu` 全仓除路由 / handler / service
实现外零引用，`admin/menu` 同样零引用 —— 所以两组能力此前都只能靠直接改库完成。

**但「管理员直接额外授权」没有界面不一定是缺口，很可能是刻意的**，动手前先回答一个问题：
这个口子由谁、在什么场景下开？它的语义是「在角色之外给单个人开口子」，一旦界面存在，
权限排查就必须同时看角色与个人两处（而这正是很多 RBAC 实现刻意不提供它的原因）。
`/api/admin/menu/list` 的响应形状也印证了这个定位 —— 它同时返回 `direct_menu_ids` 与
`effective_menu_ids`（直接 + 角色继承），是一个「排查某个人为什么能看到这个菜单」的读接口。

如果答案是「只有运维在事故处理时临时用」，那**保持无界面**（走 API + 审计）比做一个随时可点的
界面更安全；真要做界面，应当先补一条「谁在什么时候开了这个口子」的审计，否则它会变成一条
没人看得见的授权捷径。

> 本节存在的意义：让下一次盘点的人不用从零查一遍，也避免把「刻意留白」当成「漏做」重复施工。

---

## 附一：条目索引速查

| 分组 | 编号区间 | 条数 | 未开始 | 部分完成 | 文档滞后 | 待决策 | 已落地/已核销 | 冻结 |
|---|---|---|---|---|---|---|---|---|
| SEO | SEO-1 ~ SEO-10 | 10 | 1 | 2 | 1 | 0 | 6 | 0 |
| 插件体系 | PLG-1 ~ PLG-10 | 10 | 0 | 0 | 0 | 0 | 0 | **10** |
| 组件与构建器 | CMP-1 ~ CMP-16 | 16 | 6 | 3 | 6 | 1 | 0 | 0 |
| 重构遗留覆盖缺口 | COV-1 ~ COV-4 | 4 | 3 | 0 | 0 | 0 | 1 | 0 |
| 多语言 | I18N-1 ~ I18N-18 | 18 | 8 | 1 | 0 | 1 | 8 | 0 |
| 发布管线 | PIPE-1 ~ PIPE-13 | 13 | 5 | 4 | 2 | 0 | 2 | 0 |
| 商品与业务 | BIZ-1 ~ BIZ-9 | 9 | 2 | 2 | 0 | 0 | 5 | 0 |
| 基础设施 | INF-1 ~ INF-11 | 11 | 1 | 2 | 4 | 0 | 4 | 0 |
| 文档一致性 | DOC-1 ~ DOC-13 | 13 | 0 | 0 | 8 | 0 | 5 | 0 |
| **合计** | — | **104** | **26** | **14** | **21** | **2** | **31** | **10** |

> **说明（2026-09-26 重算）**：本表的旧口径（合计 104 / 未开始 68 / 部分完成 10 / 文档滞后 24 / 待决策 2）
> 与正文逐行归类不符，**已按正文实测重算** —— 旧口径把大量「已核销」「已落地」的行仍计在「未开始」里。
> 重算后九组条数之和仍为 104（10+10+16+4+18+13+9+11+13），与 §0.3 同口径。
> **本表是索引，不是工单量**：\`PLG\` 整组（10 条）已冻结，不得计入工作量；其余「已落地/已核销」列
> 与「文档滞后」列的区别见 §0.3 的说明（前者核对即可，后者仍需改目标文档）。
> 历史上被点名「已核销、不应重复施工」的条目（\`CMP-6\` / \`CMP-9\` / \`CMP-10\` 等）保留在正文表中以便追溯。

---

## 附二：非插件后台审计剩余项（2026-09-26 复核结转）

> 来源：本轮审计的收尾台账。审计批次的逐条任务书与批次记录已随文档清理删除，
> 仍待动手的项集中记在这里，避免散落在已删文档里。

| 项 | 现状 | 落点 |
|---|---|---|
| 采购收货后台表单 | **已闭环**（2026-09-26 复核：页面表单 `inventory_purchases.html:208`、页面路由 `inventory_router.go:156`、权限点 `inventory:purchase_receipt` 齐全，引入时间早于本次结转） | `internal/module/inventory/inbound/http` + `admin/inventory/` 模板 |
| 生产入库后台表单 | **已闭环**（同上：表单 `:242`、路由 `:157`、权限点 `inventory:purchase_production`） | 同上 |
| JS 运行时中文未收口 | `media-admin.js` / `drawer.js` 动态文案在英文界面仍是中文；i18n 门禁只扫模板 | `internal/templates/static/js/ui/drawer.js` + `internal/templates/static/js/media-admin.js`（后者属 AGENTS.md 列的待收敛存量，不在 `ui/` 下） |
| 捆绑表单非法数值文本 | 失败回填时非法数字文本会被归一为 `0`，不是原样回显 | `admin/product/` 捆绑配置片段 |
| 导航创建 check→create 竞态 | 并发下可能留下孤儿块（校验通过后目标被改） | `navigation` 服务层 |
| 商品编辑其它业务错误出口 | 非校验类错误仍走 `302 + ?err=`（主表单已按 htmx / 原生分档，变体与评分等子资源写路径仍是纯 302），长表单输入会丢 | `product_page_*` 写路径 |
| workbench 失败出口批 1 / 批 4 | **仍未做**：`workbench_handle.go` 还有 30 处硬编码中文 `c.String`（如 `:48`「缺少页面 id」）；`workbench_preview_handle.go:100` 直出 `workbench_enums.go:8` 的 `MsgInternalError`（**值即英文裸 key**）⇒ 用户看到英文裸串。批 2 / 批 3 已闭环 | `internal/module/workbench/inbound/http`（批 2 走 `shell.PageInternalText`、批 3 走 `c.JSON`） |
| 失败出口门禁扩围（三件） | **仍未做**：① `scripts/check-no-internal-error-leak.sh` 的 TARGETS 仍是 `find internal/module -type d -path '*/inbound/http'` ⇒ 扁平单包的 `runtimefragment` 整批漏过；② 候选集仍以「行内含 `.Error()`」为入口 ⇒ 硬编码文案与裸归口 key 从不进候选；③ `public/test/enums/unit/enums_key_seed_test.go` 的 `enumsFiles` 只有 cart/order/user/mail/workbench，**无 analytics** | 门禁脚本 + 该测试 |
| mediafield.js 弹窗未收口 | 选图弹窗仍自建 `.media-pick-mask`（`mediafield.js:80`），全文件无 `WBUI.modal` 调用 | `internal/templates/static/js/ui/mediafield.js` |

> 判定口径：前两项**已闭环**（证据见上表）；其余七项**不阻塞**已完成的批次（分类树、按需抽屉、
> 写失败回填、库存独立模块），不要因为本节而重复施工。末尾三项是 2026-09-26 复核时按「未做」状态补入的。
