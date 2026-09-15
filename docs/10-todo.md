# 10 · 文档盘点与待办清单

> 盘点对象：\`docs/\` 下全部 Markdown（含 \`docs/agents/\`）。
> 盘点方式：**只读扫描**，未修改任何既有文档；本文为新增汇总。
> 盘点时间：2026-09（以当前工作区代码为事实来源核对）。
> 结论口径：文档说「未做」但代码已存在的，一律标注 **文档滞后-代码已有**，避免按旧结论重复施工。
>
> **2026-09 回填状态（第 0 步已执行）**：DOC-1 / DOC-2 / DOC-3 已修，另同步修正 DOC-8 / DOC-13 / INF-3 与 I18N-1 的现状口径。改动文档：\`01-overview.md\`（§1.1 术语、§1.4 非目标、§4 目录树与模块表、§5 冻结边界）、\`05-implementation-plan.md\`（当前状态表 + 阶段 4–7 任务与验收门禁）、\`02-domain.md\`（§4.2/§4.3 补实现归属）、\`02-B-media-center.md\`（§6 实现映射 + §7 表结构）、\`09-session-handoff.md\`（§1.3/§1.6/§4 + 新增 §5 回填记录）。本次只改 \`docs/\`，未动代码。

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

| 指标 | 数量 |
|---|---|
| 待办总条数 | **98** |
| 未开始 | **65** |
| 部分完成 | **8** |
| 文档滞后-代码已有 | **24** |
| 待决策（非实现项） | **1** |

> 说明：\`文档滞后\` 条目同样计入总数，但**不应作为施工项**——先修文档口径即可。真正需要动手的是「未开始 + 部分完成」共 73 条。

---

## 1. SEO

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| SEO-1 | Google Search Console 验证（验证文件/元标签，发布时落根路径） | \`06-A-plugin-ecosystem-roadmap.md\` §2.1 表行 4（L40） | 未开始（无 \`google-site-verification\` 相关代码） | 中 | 无 |
| SEO-2 | IndexNow 主动推送（发布激活后 ping 搜索引擎） | \`02-E-seo-scoring-engine.md\` §6 步骤 6（L111）、§2.2（L33） | 未开始 | 低 | SEO-5（sitemap 已就绪） |
| SEO-3 | redirects 管理器（插件形态，与 publication URL 占用协同） | \`02-E\` §6 步骤 7（L112）、\`06-B-dual-track-adr.md\` 决策 6（L66） | 未开始（Redirect Artifact 本体已实现，仅缺管理界面） | 低 | PIPE-5 已完成 |
| SEO-4 | Yoast 式 SEO 分析器**插件化**（L0，进 workbench 检查器侧栏） | \`06-A\` §2.2（L45-49）、§3 表 #4（L58） | 部分完成：\`internal/seo/scoring\` + 页面设置评分面板已落地；插件形态与检查器侧栏未做 | 中 | PLG-9 |
| SEO-5 | 英文 Flesch 阅读容易度规则 | \`02-E\` §6 步骤 2（L107）、§4.2 表（L75） | 部分完成：已按 CJK 分支做句长/段长（\`checks_impl.go\` \`isCJK\`），英文 Flesch 未实现 | 低 | 无 |
| SEO-6 | meta Profile 三级默认回落（页面 → 站点 → 省略） | \`02-E\` §5（L98）、§6 步骤 4（L109） | 文档滞后-代码已有：\`internal/builder/seo_head.go:12\` 注释与实现均含三级回落 | — | 无 |
| SEO-7 | 标题长度改用**像素宽**判定（Yoast 做法） | \`02-E\` §8.3 差异登记（L186）、§7 印证结论 2（L137） | 未开始（当前仅字符数） | 低 | 无 |
| SEO-8 | 关键词密度建议区收紧到 1–1.5%（RankMath 印证） | \`02-E\` §8.3（L187）、§7 结论 2 | 未开始（当前 0.5–2.0% 主判） | 低 | 无 |
| SEO-9 | 商品结构化数据（Product JSON-LD 由商品数据驱动） | \`06-A\` §2.1 表行 3（L39） | 部分完成：\`schemaType=product\` 与 JSON-LD 输出已就绪；缺商品实体数据源 | 中 | BIZ-1 |
| SEO-10 | 文章编辑页评分入口（正文侧栏：密度/长度/可读性/内链） | \`02-E\` §9 表（L197）、\`09-session-handoff.md\` §1.4（L45） | ✅ **已落地（2026-09）**：\`internal/seo/article.go\` 的 \`ScoreArticle\`（文章字段 → \`scoring.Input\`：seoTitle/seoDescription 回落、正文去标签算字数、h1-h6 结构、正文图片 + 封面、内外链与锚文本）；编辑页侧栏 \`POST /admin/articles/score\`（HTMX 局部刷新，无 JS 时打开 / 保存后整页渲染同一份分）。**本次补记（2026-09）**：canonical 与结构化数据原先恒判未达标（当时的真实缺口），现由构建期注入（`presentation/service/presentation_seo.go`：canonical 取实例线上路径、JSON-LD 按 `schemaType=article`），`ScoreArticle` 已同步判真 —— 侧栏只显示编辑者能改的东西，系统保证项不挂在上面 | — | INF-1（已完成） |

---

## 2. 插件体系

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| PLG-1 | L2 插件**自有集合源**注册（\`collections.json\` 声明式，\`plugin:{id}.{table}\` 通道） | \`06-plugin-system.md\` §9（L245-264）、§13 P3（L310） | 部分完成：组件级 \`Collection\` 绑定 + \`content:{product\|article\|category}\` 本体通道已落地（\`content/service/collection_resolver.go\`）；\`plugin:{id}.{table}\` 仅白名单正则，无注册机制 | 高 | PLG-2 |
| PLG-2 | manifest 顶层 \`collections\` 段（§5.1 目录结构 / §5.3 骨架） | \`06-plugin-system.md\` §5.1（L106）、§5.3（L147） | 未开始（\`plugincomp.Manifest\` 无 collections 字段） | 中 | 无 |
| PLG-3 | manifest \`admin.menu\`（插件后台菜单注册） | \`06-plugin-system.md\` §5.3（L148） | 未开始 | 中 | 无 |
| PLG-4 | 插件 \`fragments/\` 目录（运行时片段随包分发） | \`06-plugin-system.md\` §5.1（L114） | 未开始 | 中 | PLG-3 |
| PLG-5 | zip 签名校验（\`signature\` 字段 + 公钥验签，未签名仅 dev 可装） | \`06-plugin-system.md\` §5.4（L152-155）、§11 表（L290）、\`06-B\` 决策 1/4（L18、L50） | 未开始 | 中 | 无 |
| PLG-6 | 插件**受限事务 API**（service 层 \`Transaction()\` 透传给受信插件） | \`06-A\` §4 前置硬骨头 2（L87）、\`06-B\` 决策 4 前置 2（L49） | 未开始（\`plugin/model\` 的 \`Transaction\` 仅服务 L1 迁移执行器） | 中 | PLG-5 |
| PLG-7 | 第三轨（Yaegi 解释器 / Wasm 沙箱受限逻辑） | \`06-plugin-system.md\` §4.3（L91-96）、§13 演进（L311） | 未开始（文档明确「首发不做，留作 Fragment 落地后再评估」） | 低 | 无 |
| PLG-8 | 插件市场与签名分发 | \`06-plugin-system.md\` §13 演进（L311） | 未开始 | 低 | PLG-5 |
| PLG-9 | 首批插件 12 个（forms/marketing/SEO 分析器/analytics/membership/points/i18n/comments/search/smtp/组件包） | \`06-A\` §3 表（L53-66） | 未开始（工作区无 \`plugins/\` 目录，无示例插件包） | 中 | PLG-1、PLG-2 |
| PLG-10 | 组件包 packs（L0，轮播/动效/图标库/定价表/团队墙/时间线） | \`06-A\` §3 表 #12（L66）、§6 备忘（L104） | 未开始（文档标注「样式引擎 + presets 已就绪，现在就能写」） | 中 | 无 |

---

## 3. 组件与构建器

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| CMP-1 | \`component\` 模块（Global Component 版本 + \`immutable/auto-update/pinned\` 策略 + Registry manifest） | \`05-implementation-plan.md\` 阶段 5（L230）、\`01-overview.md\` 模块表（L286）、\`02-domain.md\` §4.2/§4.3（L406-472） | 未开始（\`internal/module/\` 无 component；当前由 \`block\` 模块 \`reuse_mode=global\` 承担复用语义） | 中 | 概念需先对齐（见 §9 DOC-13） |
| CMP-2 | Jet 三层 Set 隔离 + \`AddGlobalFunc\` 注入 | \`jet-and-go-libs-plan.md\` 待办表 P1（L216）、§3（L47-58） | 文档滞后-代码已有：\`templates/jet_render.go\`（admin/workbench）、\`component_set.go\`（构建期）、\`fragment_render.go\`（片段）、\`plugin_loader.go\`（插件）各自 Set + \`injectGlobals\` | — | 无 |
| CMP-3 | \`builder/core\` 抽库（拆 \`internal\`，开源复用） | \`jet-and-go-libs-plan.md\` 待办表 P3（L220）、\`component-jet-migration-plan.md\` §八（L196） | 未开始 | 低 | 无 |
| CMP-4 | Jet 模板 IDE 高亮/校验插件选型 | \`component-jet-migration-plan.md\` §八（L197） | 未开始 | 低 | 无 |
| CMP-5 | \`card\` 组件缺标题/正文排版字段（当前只能靠通用层） | \`09-session-handoff.md\` §4（L384） | 未开始 | 低 | 无 |
| CMP-6 | \`core.text\` \`ct:maxlen\` 与清洗硬上限不一致 | \`09-session-handoff.md\` §4（L385） | 文档滞后-代码已有：\`core.MaxRichLen\` 已统一为 30000，与 \`ct:"richtext,maxlen=30000"\` 一致 | — | 无 |
| CMP-7 | 富文本图片（Trix \`figure/img\`）未接媒体变体与 caption | \`09-session-handoff.md\` §4（L386） | 未开始（白名单直出 \`src\`，不走 \`srcset\`） | 中 | 无 |
| CMP-8 | \`infobox.icon\` 手写面板与后端 tag 是否重复显示 | \`09-session-handoff.md\` §4（L389） | 未开始（待确认是否真缺陷） | 低 | 无 |
| CMP-9 | 容器 flex 布局面板（方向/主轴/交叉轴/换行/间距） | \`09-session-handoff.md\` §4（L388） | 文档滞后-代码已有：\`containerLayout()\` 已渲染完整面板，核查后无需改动（同文 L281 已记录） | — | 无 |
| CMP-10 | workbench 块「插入-复制」前端接线 | \`02-D-reusable-assets.md\` §12 步骤 5（L306） | 文档滞后-代码已有：\`workbench/methods/canvas.js:250-255\` 已调 \`POST /api/block/clone\` | — | 无 |
| CMP-11 | \`inspector.js\` 拆分（\`syncInspector()\` 内嵌 49 个闭包控件函数，约 1500 行） | \`09-session-handoff.md\` §3（L194、L355） | ✅ **文档滞后-代码已有（2026-09 复核）**：控件已提取到 \`internal/templates/static/js/workbench/methods/controls/*.js\`（9 个文件 2093 行），\`methods/inspector.js\` 实测 161 行 | — | 无 |
| CMP-12 | 服务端持 AST（编辑操作服务端化，彻底压缩 workbench.js） | \`09-session-handoff.md\` §3 收益天花板（L355-356） | 未开始（评估项） | 低 | CMP-11 |
| CMP-13 | htmx 官方扩展引入（head-support / response-targets / loading-states / class-tools 等） | \`06-C-htmx-extensions.md\` §二（L21-37） | 部分完成：idiomorph 已引入（\`layout.html:181\` + \`core.js\` \`morphHTML\`）；其余待评估 | 低 | 无 |
| CMP-14 | 结构树拖拽 DOM 级自动化测试 | \`09-session-handoff.md\` §3 交互走查补充（L268） | 未开始（\`moveNode\` 已有 Go 侧行为覆盖） | 低 | 无 |
| CMP-15 | 富文本 ⇄ 可视化组件树的等价转换（\`06-B\` 决策 5） | \`06-B-dual-track-adr.md\` 决策 5（L52-59） | **转换能力已落地（2026-09）**：\`internal/builder/richdoc\` 的 \`HTMLToNodes\`（块级标签 → 组件：h1~h6→heading / p→text / ul,ol→list / blockquote→quote / pre→text / img,figure→image / hr→divider / table→table；行级格式留在 core.text 内）与 \`NodesToHTML\`（可逆子集反向导出；不可逆组件输出占位并标 \`Lossless=false\`）+ fuzz 背书（\`FuzzRichTextRoundTrip\` 90 秒 72.9 万次执行通过，两个历史失败用例留在 \`testdata/fuzz/\`）。**入口已落地（2026-09）**：文章编辑页「导入到画布」区块 —— 预览（纯计算，展示组件统计与损失清单）→ 创建页面草稿并跳工作台；端到端实测（正文 → 6 组件 → Page 草稿 → 预览编译出真实 HTML）。**未做**：① 真源改造（文章 body 仍存 HTML 字符串，「双视图单真源」还不成立）② 模板起稿入口（只做了文章 → 页面） | 中 | 无 |

---

## 4. 多语言（i18n）

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| I18N-1 | 灰度开关 \`i18n.site_lang_prefix\` 默认 \`false\`，多语言**不能同时在线** | \`06-D-site-i18n.md\` §15.3（L953-965）、§15.8 语义口径（L1044）、§15.9 遗留（L1133） | ✅ **已解决（2026-09 复核）**：\`config.yaml:66\` 与 \`config.yaml.example:65\` 均为 \`site_lang_prefix: true\`，开关已启用（原记录「默认关闭」已过时） | 高 | 无 |
| I18N-2 | 块内文本不翻译（\`compileBlockFragment\` 未传 lang 与取词器） | \`06-D\` §15.11 已知缺口（L1269-1271）、§15.12（L1365-1366） | ✅ **已落地（2026-09 复核）**：\`compileBlockFragment(ctx, blockID, lang string, translator *i18n.ContentTranslator)\` 已带 lang 与取词器，调用点（\`page_assemble.go\` 的页眉 / 页脚内联）两个参数都传了。**本次回填修正**：「未传 lang 与取词器」这句已不成立 | — | 无 |
| I18N-3 | \`core.nav\` 菜单标签多语言归属未定（标签来自 navigation 数据） | \`06-D\` §15.11（L1272-1273）、§15.12（L1370-1371） | 未开始（未定论） | 中 | 无 |
| I18N-4 | 站内链接本地化只覆盖导航（按钮/图片/文本内链接仍是逻辑路径） | \`06-D\` §15.5 第 5 条（L984）、§15.8 仍属后续（L1064） | 未开始 | 中 | 无 |
| I18N-5 | Runtime Fragment 语言（\`/_fragments\` 无 \`lang\`、无 \`Vary\`） | \`06-D\` §15.5 第 7 条（L986）、§11（L855） | 未开始 | 中 | 无 |
| I18N-6 | 后台 i18n 词条 CRUD（D7）+ \`MarkStaleForI18n\` 调用方不完整 | \`06-D\` §15.5 第 6 条（L985）、§14 D7（L912） | 未开始（仅 \`/admin/lang\` 语言切换路由，无词条管理页） | 中 | 无 |
| I18N-7 | CMS 内容字段翻译（P5d，\`sys_translation\` 接 content 模块） | \`06-D\` §13 P4（L895）、§15.11 范围（L1186） | 未开始（\`content/service\` 无翻译接入） | 中 | BIZ-1 或内容页先行 |
| I18N-8 | 一键 AI 翻译（\`engine='ai'\`，当前按钮灰置预留） | \`06-D\` §7.9（L611-649）、§15.12（L1283） | 未开始（明确本期不做） | 低 | I18N-7 |
| I18N-9 | 译文删除 / 改原文后孤儿行清理（D14） | \`06-D\` §14 D14（L919）、§15.12（L1367） | 未开始 | 低 | 无 |
| I18N-10 | 媒体 \`alt\` 多语言（D6，新增 \`media_translations\`） | \`06-D\` §14 D6（L911） | 未开始（建议后续单独做） | 低 | 无 |
| I18N-11 | 每语言 slug（D4 方案②） | \`06-D\` §14 D4（L909） | 未开始（① 仅默认语言 slug 已实现） | 低 | 无 |
| I18N-12 | 禁用某语言后其已激活路由不自动清理 | \`06-D\` §15.8 仍属后续（L1065）、§15.9（L1131） | 未开始（页面仅有提示文案） | 中 | 无 |
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
| PIPE-1 | 删除、取消发布与 GC（tombstone / 保留期 / \`gc_pending\` claim / 内容对象闭包回收） | \`03-pipeline.md\` §7（L373-400）、\`internal/pipeline/url.go:10\` 注释「§7/§8 属后续阶段」 | 未开始（无 \`gc_pending\` / claim 相关实现） | 中 | 上线前必须补 |
| PIPE-2 | 构建队列 / Build Job Engine（幂等键、superseded、重试与退避） | \`03-pipeline.md\` §8.3（L492-500）、\`0-A1-pipeline.md\` §3.2（L46） | 未开始（\`build_jobs\` 表存在于 \`init_builder_schema.sql\`，无 Go 侧引擎） | 中 | PIPE-3 |
| PIPE-3 | 依赖 fan-out 与 stale 状态机（\`direct_content\` / \`content_collection\` / \`content_template\` / \`site_setting\` 等） | \`03-pipeline.md\` §8.1（L404-426）、§8.2（L428-490）、\`05-implementation-plan.md\` 阶段 4 验收（L216） | 主体已落地（2026-09）：迁移 071（反查索引 + kind 扩展）、依赖记录落库（page + presentation 两侧）、\`direct_content\`/\`content_collection\` 精确反查（不再全站标记）、受影响产物自动重建 + 已发布页面自动回写；**遗留**：menu/media/site_setting 未登记（\`content_template\` 仅 presentation 侧）、page 侧未登记 \`content_template\`、stale 保持 bool | 高 | 无 |
| PIPE-4 | 插件生效流水线（上传 → 校验 → 入库 → **asynq 入队重建受影响页面** → 原子切换） | \`06-plugin-system.md\` §10（L266-279） | 部分完成：安装/编译/原子切换已落地；\`asynq\` 目前仅用于媒体变体（\`media_variant_task.go\`），未接构建 | 中 | PIPE-3 |
| PIPE-5 | Redirect Artifact（\`redirect.json\` + \`route_kind=redirect\` + Static Server 301） | \`03-pipeline.md\` §4.4（L222-247） | 文档滞后-代码已有：\`pipeline/artifact.go\` \`RedirectDirective\`、\`store.go\` \`PutRedirect\`、\`publication.go\` 读取、\`page_publish.go:595\` \`ensureRedirectRoute\`（改 URL 时生成 301） | — | 无 |
| PIPE-6 | AccessGuard（密码保护 / 登录用户可见，Manifest 标记 + 静态守卫页） | \`0-A2-page-routing-meta.md\` §2.2（L37-38）、§5（L84） | 未开始（无 \`AccessGuard\` 相关代码） | 中 | 访客账号域（BIZ-3）做「登录可见」 |
| PIPE-7 | 定时上下线（Scheduled Publishing） | \`0-A2\` §2.2（L39-41）、§5（L85） | 未开始（无 \`internal/task\` 目录、无 scheduled 字段） | 中 | 无 |
| PIPE-8 | Head / Body 代码注入（页面级统计与营销脚本） | \`0-A2\` §2.3（L45-48）、§5（L86） | 未开始（无 \`HeadScripts\`/\`BodyScripts\`） | 中 | 需安全白名单评审 |
| PIPE-9 | Robots meta（\`index/noindex\` + \`follow/nofollow\`） | \`0-A2\` §2.1（L28-30）、§5（L83） | ✅ **已落地（2026-09 复核）**：\`PageSettings.SEO\` 有 \`robotsIndex\` / \`robotsFollow\` 两个字段；构建期由 \`builder/seo_head.go\` 的 \`robotsContent\` 组装并输出 \`<meta name=\"robots\">\`（两项都取默认 index/follow 时**返回空串**，默认页面的产物字节与没这个功能时逐字节一致）；工作台页面设置面板有「搜索引擎收录」与「链接跟踪」两组按钮。**本次回填修正**：上一版说「两个字段都没有」是盘点时的滞后 | — | 无 |
| PIPE-10 | Meta Keywords 字段 | \`0-A2\` §2.1（L27） | 未开始（现代搜索引擎已不看重，可低优先） | 低 | 无 |
| PIPE-11 | OGTitle / OGDescription 独立字段 | \`0-A2\` §2.1（L32）、§5（L83） | 文档滞后-代码已有：\`seo_head.go:46/50\` 已输出 \`og:title\`/\`og:description\`（取自页面 SEO 标题/描述） | — | 无 |
| PIPE-12 | 一键设为全站首页（根路径 \`/\`） | \`0-A2\` §1（L13）、§5（L80） | 未开始（设置面板与 service 无该动作） | 中 | 无 |
| PIPE-13 | CDN 缓存清理（激活后清理旧路径边缘缓存） | \`0-A2\` §3（L67）、§5（L87） | 未开始 | 低 | 部署层 |

---

## 6. 商品与业务模块

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| BIZ-1 | \`commerce\` 本体模块（商品/分类/SKU/库存/订单状态机/支付回调验签/优惠码幂等核销） | \`06-A\` §4（L70-83）、\`06-B\` 变更记录 v2（L70-81） | **商品 / 分类 / SKU / 库存已落地**（\`internal/module/product\` 与 \`product/inventory\`，#5–#32 —— v2 决策里的 \`commerce\` 实际以 \`product\` 命名）；**订单已落地**（\`internal/module/order\`，建单 / 状态机 / 取消 / 退款 / 查询 + 归因冗余，迁移 135/136）；**购物车与访客结算已落地**（\`internal/module/cart\`：HMAC 签名 cookie 购物车 + 六个 anonymous 片段能力 + 结算四步；支付通道当前为**模拟 PayPal**，零迁移）；**归因采集链路已落地**（构建期内联 \`track.js\` + 下单定格）；**支付回调验签已落地**（`cart/inbound/http` 的 `POST /payment/callback`：先验签后解析 + 常量时间比较 + 金额核对 + 幂等落账）；**优惠码已落地**（迁移 141/142/143：口径 / 门槛 / 次数 / 每人限次 / 时间窗 + **与建单同事务的幂等核销** + 后台页 `/admin/coupons`）；**后台订单管理页已落地**（`/admin/orders`）；**访客账号表单已片段化**（login / register / forgot / reset / account 五个片段 + `core.userForms` 组件：表单带 CSRF token 必须现渲染，槽位从此可指向作者自建的登录页 / 个人中心页）；**后台备注可编辑已落地**（`POST /api/order/note`，只开放备注一列、不写流转链，迁移 146）；**退货入库已落地**（迁移 144/145：按订单项申请 → 审核 → **先入库、后退款**；收货门闩保证重复点击不会二次入库；全额才转订单已退款；后台页 /admin/returns，访客侧片段 returnRequest）；**访客自助订单查询已落地**（`ordersList` / `orderDetail` 片段 + `core.orderList` 组件，归属由 SQL 条件收口） | 高 | 已拍板方向，可先行 |
| BIZ-2 | 首批商品 capability（\`productList\` / \`searchResults\` / \`productAvailability\` / \`productLivePrice\` / \`cartSummary\` 接真实数据 / \`cartAdd\`） | \`04-A-dynamic-capabilities.md\` §7（L128-142）、\`04-B\` §6 索引（L152） | ✅ **全部已落地（2026-09 复核）**：\`runtimefragment/\` 下已注册 \`cartSummary\` / \`cartView\` / \`cartAdd\` / \`cartSetQty\` / \`cartClear\` / \`checkout\`（cart.go）、\`productList\`（product_list.go）、\`productVariantAvailability\`（variant_availability.go）、\`productLivePrice\`（live_price.go）、\`searchResults\`（search_results.go）、\`bundleConfigurator\`（bundle_configurator.go）。**本次回填修正**：上一版把 \`searchResults\` / \`productLivePrice\` 记为未开始，是盘点时的滞后 | — | BIZ-1 |
| BIZ-3 | 会员 membership（访客账号领域，admin 之外另建） | \`06-A\` §3 表 #6（L60）、\`AGENTS.md\` 命名约束 | **账号底座已落地（issue #36）**：\`internal/module/user\` 提供注册 / 邮箱验证 / 登录 / 密码重置 / 账号中心（资料 / 偏好 / 登录设备），与 \`admin\` 完全隔离；**会员等级与权益未开始** | 中 | 无 |
| BIZ-4 | 积分 points（流水对账/幂等） | \`06-A\` §3 表 #7（L61） | 未开始 | 低 | BIZ-3 |
| BIZ-5 | 评论 comments（提交走 fragment + 审核） | \`06-A\` §3 表 #9（L63） | 未开始 | 低 | BIZ-3 |
| BIZ-6 | 站内搜索 capability（\`searchResults\`，白名单排序） | \`06-A\` §3 表 #10（L64） | ✅ **已落地（2026-09 复核）**：\`runtimefragment/search_results.go\` 注册 anonymous GET 片段 \`searchResults\`（参数 q / projectId / limit），经收窄只读端口 \`SetContentSearchProvider\` / \`SetProductSearchProvider\` 取数（未注入时降级为空片段而不是 500）；内容检索按原文匹配，**有译文的文章用中文关键词搜不到**（多语言检索属后续票） | — | 无 |
| BIZ-7 | 邮件 smtp（配置 + 队列，表单/会员通知依赖） | \`06-A\` §3 表 #11（L65） | **已落地（issue #37 / #38）**：\`internal/module/mail\`（发信账号 / 模板 / 联系人 / 群发 / 自动化 / 事务发送 / 追踪退订），队列未启用时 \`SendTemplate\` 降级同步发送；\`user\` 模块的验证信与重置信已实际接入 | 低 | 无 |
| BIZ-8 | 统计 analytics（GA4/gtag 注入 + 访问计数） | \`06-A\` §3 表 #5（L59） | ✅ **全部已落地（2026-09 复核）**：① 归因采集 —— 构建期内联 \`track.js\`（UTM 家族 / 广告点击 id / referrer / 会话 / 浏览轨迹 → 签名 cookie → 下单定格进 \`orders.attribution\`）；② GA4/gtag 注入 —— 真源 \`projects.settings.ga4MeasurementId\`，构建期经 \`builder.WithGA4MeasurementID\` 进 head（空值 / 非法值零字节注入）；③ 访问计数 —— \`internal/module/analytics\`（公开面 \`POST /analytics/collect\` 只写一条记录 + 后台只读 \`GET /api/analytics/summary\`，落库只存匿名派生值），后台页 \`/admin/analytics\`，迁移 147/148/149。**本次回填修正**：上一版把后两项记为未开始，是盘点时的滞后 | — | PIPE-8 |
| BIZ-9 | 商品数据进可视化（\`content:product\` 集合源本体化注册） | \`06-A\` §4 前置硬骨头 1（L86）、\`06-B\` 变更记录 v2 理由 4（L79-80） | 文档滞后-部分已有：\`content\` service 已实现 \`core.CollectionResolver\`（\`content:{product\|article\|category}\`），通道已通；缺商品实体本身 | 中 | BIZ-1 |

---

## 7. 基础设施

| # | 事项 | 出处 | 现状 | 优先级 | 依赖/前置 |
|---|---|---|---|---|---|
| INF-1 | CMS 内容**后台管理页** + 文章编辑页 | \`05-implementation-plan.md\` 阶段 4 验收（L215）、\`09-session-handoff.md\` §1.4（L45） | ✅ **已落地（2026-09）**：\`/admin/articles\`（列表 + 新建 + 删除 + 发布状态）、\`/admin/articles/edit\`（Trix 富文本 + 摘要 + 封面 + SEO 字段 + 评测侧栏 + 发布区块）、侧栏「内容」组入口（\`nav_menu.go\`）。写操作复用既有 \`content:*\` 权限点（**未新增权限点**），正文落库前过 \`core.SanitizeRichHTML\`，保存即触发依赖扇出（引用它的页面与已发布实例标记待重建）。迁移 150 只 seed「菜单管理」页的条目（侧栏真源仍是代码配置） | — | 无 |
| INF-2 | CMS 实体变更 → 自动派生 DocumentSnapshot → 自动发布 | \`05\` 阶段 4 任务（L208）、\`03-pipeline.md\` §8.2 典型 fan-out（L462-472） | ✅ **两侧均已落地（2026-09）**：page 侧（PIPE-3）内容变更 → 精确反查 → 自动重建 + 已发布页面自动回写；presentation 侧在 DDL 对齐修复后接入同一 fan-out（内容变更 → 精确反查 → 自动重建并重新发布，\`presentation_dependencies\` 落库） | — | 无 |
| INF-3 | 02-B 媒体中心三表与实现不一致 | \`02-B-media-center.md\` §7（L85-88：\`media_asset\`/\`media_asset_variant\`/\`media_reference\`） | ✅ **已修正（2026-09）**：\`02-B\` §7 改为实际三表，并说明 \`media_asset\`/\`media_asset_variant\`/\`media_reference\` 仅为 \`init_schema.sql\` 建表、无任何 Go 引用的遗留；§6 实现映射同步改为真实代码位置（原引用的 \`internal/builder/media/\` 不存在） | — | 无 |
| INF-4 | 媒体下载接口（单图 + 批量 zip） | \`media-variants-recon.md\` §C（L200-204） | 文档滞后-代码已有：\`media_router.go:37-38\` \`/download\` 与 \`/download/batch\` | — | 无 |
| INF-5 | qiniu 存储不支持批量打包（一期限定 \`storage_type=local\`） | \`media-variants-recon.md\`（L204） | 未开始（已知限制） | 低 | 无 |
| INF-6 | 变体异步生成（goroutine / asynq + \`pending\` 状态） | \`media-variants-recon.md\` §A（L196） | 部分完成：\`media_variant_task.go\` 已接 asynq 任务；上传同步生成仍在 | 低 | 无 |
| INF-7 | \`GenerateVariants\` 导出为契约方法（存量回填 / 重新生成按钮） | \`media-variants-recon.md\` §B（L198） | 文档滞后-代码已有：\`media/contract/media_service.go:33\` 已导出 | — | 无 |
| INF-8 | 后台「菜单管理」页未收录 \`/admin/navigations\` | \`09-session-handoff.md\` §4（L390） | 未开始（侧边栏走代码配置，功能不受影响） | 低 | 无 |
| INF-9 | 复刻页外链图片本地化（走媒体库，顺带验证变体管线） | \`09-session-handoff.md\` §4（L383） | 文档滞后-代码已有：同文 §3（L270-274）已记录完成（10 张图本地化，仅剩 \`<a>\` 链接保留外链） | — | 无 |
| INF-10 | 访客面零 JS 校验 / 依赖 fan-out E2E 验收清单保持规划态 | \`04-runtime-and-delivery.md\`（L148） | 文档滞后：依赖 fan-out 见 PIPE-3；Runtime Fragment / Client Enhancement / PresentationInstance / ContentTemplate 均已落地 | — | 见 PIPE-3 |
| INF-11 | 页面设置面板缺 Robots / Keywords 字段（与 PIPE-9/PIPE-10 同源，此处记录 UI 侧） | \`0-A2\` §2.1、\`templates/fragments/settings_panel.html\` | **部分完成（2026-09 复核）**：Robots 侧已落地 —— 面板有「搜索引擎收录」（index/noindex）与「链接跟踪」（follow/nofollow）两组按钮（见 \`settings_panel.html\` 的 \`data-wb-setting=\"settings.seo.robotsIndex\" / robotsFollow\`），后端 \`settingsViewOf\` 也解析了这两个字段；**Keywords 仍未做**（同 PIPE-10，\`builder.SEO\` 结构里没有 keywords，只有 secondaryKeywords）。**本次回填修正**：上一版写「面板仅 title/description/…」漏了 robots 两组 | 低 | PIPE-10 |

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
| DOC-9 | \`06-A\` 把「L2 CollectionSource 落地」列为**硬依赖 0-A2 content** 的前置硬骨头 | \`06-A\` §4 前置 1（L86）、§5 依赖图（L96） | 部分滞后：\`content\` 模块的集合源（\`content:{type}\`）已落地，仅插件自有 schema 通道（\`plugin:{id}.{table}\`）未做 | 中 | 见 PLG-1 |
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

✅ **已解决（2026-09 复核）**：\`config.yaml:66\` 与 \`config.yaml.example:65\` 均为 \`i18n.site_lang_prefix: true\`，开关已启用；多语言内核、\`page_publications\`/\`page_stagings\`/\`project_locales\`、hreflang、语言切换器、翻译工作台全部已落地并有端到端测试。
（原记录「默认 false」已过时。）
这是全表性价比最高的一条：一行配置 + 一轮回归，就能让 P2–P5c 的投入变成可交付能力。
先决条件：\`I18N-12\`（禁用语言路由清理）同期补，否则开开关后会出现悬挂激活路由。

### 第 2 步：阶段 4 收尾——自动发布主链（3–5 天）

\`PIPE-3\`（依赖 fan-out + stale 状态机）是阶段 4 的核心验收门禁，也是 \`INF-2\`（CMS 变更自动发布）、\`PIPE-4\`（插件生效后重建）、\`I18N-2\` 的共同前置。
它解掉之后：\`INF-2\`（presentation 自动重建）→ \`PIPE-2\`（构建队列）→ \`PIPE-1\`（GC 与取消发布）形成完整生命周期。
\`INF-1\`（CMS 后台内容管理页 + 文章编辑页）✅ **已完成（2026-09）** —— 它只依赖现有 \`content\` API，落地后同时解掉 \`SEO-10\`（文章评分入口）。\`I18N-7\`（CMS 翻译）现在有了入口（文章编辑页），可以接着做。

### 第 3 步：插件生态的 L2 补齐（3–4 天）

\`PLG-1\` + \`PLG-2\`（插件自有集合源 \`collections.json\`）——本体的 \`content:{type}\` 通道已通，缺的只是「插件自带表 → 白名单注册 → 构建期展开」这一段。
解掉后 \`PLG-9\`（首批插件）与 \`PLG-10\`（组件包，无表可直接写）才真正可做。
\`PLG-5\`（zip 签名）应在任何第三方插件分发之前完成，\`PLG-6\`（受限事务 API）仅约束第三方电商类插件，可后置。

### 第 4 步：商品本体模块（按需启动）

\`BIZ-1\`（\`commerce\`）已由 \`06-B\` v2 拍板为**本体模块**，且 \`BIZ-9\`（\`content:product\` 进可视化）通道已就绪，是当前阻力最小的新领域。
它同时解锁 \`SEO-9\`（Product JSON-LD）、\`BIZ-2\`（商品 capability）。
注意：库存/订单/支付必须留在 Go 模块内，不要因为「插件化更快」而退回到 \`06-B\` 决策 1 已否决的路径。

### 第 5 步：上线前的发布管线补全

\`PIPE-1\`（GC/取消发布）、\`PIPE-6\`（AccessGuard）、\`PIPE-7\`（定时上下线）、\`PIPE-8\`（Head/Body 注入）、\`PIPE-9\`（Robots meta）、\`PIPE-12\`（一键设为首页）——这些是「静态站点对外营业」的必备项，但不是其他工作的前置，可排在功能主线之后。

### 可长期挂账（低优先）

\`CMP-3\`（抽库）、\`CMP-12\`（服务端持 AST）、\`PLG-7\`（第三轨）、\`PLG-8\`（插件市场）、\`I18N-8\`（AI 翻译）、\`I18N-13\`~\`I18N-18\`、\`INF-5\`、\`INF-8\` 等。

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

1. ~~**「已落地但未启用」是当前最大浪费**：i18n 语言前缀开关（\`I18N-1\`）是典型——功能完整、测试齐备、默认关闭。~~ ✅ **已解决（2026-09）**：开关已启用（\`config.yaml:66\`），本条不再成立。
2. **插件体系是「半成品最集中」的区域**：P0–P2 已合入主干，但 manifest 的 \`collections\`/\`admin\`/\`fragments\`/\`signature\` 四段全部缺失，导致插件只能做纯展示组件（L0），L1 数据层与 L2 内容源都停在设计文档里。
3. **阶段 4 是唯一真正卡住主链的阶段**：\`PIPE-3\`（依赖 fan-out）未做，导致 content/presentation 只能手动重建，\`05\` 阶段 4 的验收门禁（「内容变更触发自动重建并发布」）无法通过。
4. **商品域已两次改向**（插件 → 重轨 → 本体），建议在动工前把 \`06-B\` 变更记录与 \`02-domain\`/\`01-overview\` 的模块描述统一，避免第三次返工。

---

## 附：条目索引速查

| 分组 | 编号区间 | 条数 | 未开始 | 部分完成 | 文档滞后 | 待决策 |
|---|---|---|---|---|---|---|
| SEO | SEO-1 ~ SEO-10 | 10 | 7 | 3 | 0 | 0 |
| 插件体系 | PLG-1 ~ PLG-10 | 10 | 9 | 1 | 0 | 0 |
| 组件与构建器 | CMP-1 ~ CMP-14 | 14 | 9 | 1 | 4 | 0 |
| 多语言 | I18N-1 ~ I18N-18 | 18 | 16 | 1 | 0 | 1 |
| 发布管线 | PIPE-1 ~ PIPE-13 | 13 | 10 | 1 | 2 | 0 |
| 商品与业务 | BIZ-1 ~ BIZ-9 | 9 | 8 | 0 | 1 | 0 |
| 基础设施 | INF-1 ~ INF-11 | 11 | 6 | 1 | 4 | 0 |
| 文档一致性 | DOC-1 ~ DOC-13 | 13 | 0 | 0 | 13 | 0 |
| **合计** | — | **98** | **65** | **8** | **24** | **1** |

> 说明：\`BIZ-9\` 与 \`INF-10\` 分别计为「文档滞后（含部分完成语义）」与「文档滞后」；\`CMP-9\`/\`CMP-10\`/\`CMP-6\` 等已核销项保留在表中以便追溯，**不应重复施工**。
