# 02-E · SEO 评分引擎与内置评判（设计 + 执行计划）

> 本文回答三个问题：① 内置 SEO 评判对页面/文章是否合适（合适合，但分层）；
> ② /home/sky/project/seo 工作区的评判标准如何复用；③ Yoast 源码能否解析成「计算器」。
> 上游规划：[06-A-plugin-ecosystem-roadmap.md](./06-A-plugin-ecosystem-roadmap.md) §2（SEO 基建进本体、分析器做插件）。

## 1. 结论先行

| 问题 | 结论 |
|---|---|
| 内置 SEO 评判 | **合适**。但必须分层：评分器是**编辑期只读建议**（控制面），永不进构建管线（确定性构建不变量不受评分影响） |
| seo 工作区标准复用 | `on-page-seo-checker` 的 scoring-rubric（8 维度加权卡 + 全部基准表）**直接采用**为评分规则来源，经实际站点校准过 |
| Yoast 解析成计算器 | **不反编译 premium bundle**（混淆 min.js + GPL 许可风险）。评分算法规则是公开知识 + Yoast 核心分析器本就开源（GitHub Yoast/javascript）。路径：自研纯函数评分器（规则来自 rubric + Yoast 公开 assessments），或直接引入开源 `@yoast/seo` 包（GPL，自用可） |

## 2. 数据源盘点（已探查）

### 2.1 /home/sky/project/seo（多站点 SEO 工作区）

可用资产：

- **`on-page-seo-checker` 评分卡**（.agents/vendor/.../scoring-rubric.md）：
  - 8 维度加权：Title 15% / MetaDesc 5% / 标题结构 10% / 内容质量 25% / 关键词优化 15% / 内外链 10% / 图片优化 10% / 页面技术 10%
  - 基准表：内容长度按查询类型（信息型 1500+ 词满分）、关键词密度 0.5-2.0% 满分 / >3% 判 stuffing、内链数按篇幅（1000-2000 词理想 5-10 条）、图片大小分级目标（Hero<200KB WebP…）、CWV 阈值（LCP≤2.5s / INP≤200ms / CLS≤0.1 / TTFB≤800ms）
  - WCAG 2.2 AA 无障碍叠加（alt/标题嵌套/对比度/焦点可见）→ HIGH 优先级
- 其他 skill（关键词研究/内容缺口/SERP 分析）是**运营流程**，不内置进产品，但 rubric 是纯规则可内置。

### 2.2 Yoast premium 源码（/home/sky/115open/.../wordpress-seo-premium.zip）

已解包核对（/tmp/yoast-src）：

- premium 是**增值包**：redirects 重定向管理、内链建议、social previews、AI 修复、IndexNow ping（src/integrations/index-now-ping.php）
- **核心评分分析器不在包内**（在免费版 analysis-worker；算法本体开源在 GitHub `Yoast/javascript`，npm `@yoast/seo`，GPLv2）
- 可借鉴的产品功能：redirects 管理器（publication 模块邻接）、IndexNow 主动推送（发布激活后 ping 搜索引擎）、OG/Twitter 预览卡片

## 3. 架构分层：评分器放哪

```text
控制面（编辑期）                          访问面（不变）
┌─────────────────────────────┐        ┌──────────────────┐
│ workbench/文章编辑页 SEO 侧栏 │        │ 静态 Artifact     │
│  ├ 评分器（纯函数，只读建议）  │  构建→ │ （head 全量 meta  │
│  ├ SERP 预览（标题/描述截断） │  管线  │  由 02-D 后续基建  │
│  └ 逐项改进建议（可点跳转）    │        │  产出，见 §5）     │
└─────────────────────────────┘        └──────────────────┘
评分输入：Page Document AST + settings.seo + 正文 + 已注册集合源
铁律：评分器不写 Document、不进构建管线、不影响确定性
```

### 3.1 维度裁剪（go_wp 架构天然优势）

rubric 8 维度中「页面技术」在 go_wp 下大部分**构建期已保证**，评分器按架构裁剪：

| 技术检查项 | go_wp 处置 |
|---|---|
| HTTPS / 移动友好 / 纯净 URL | 部署层保证，评分器内置「架构满分」标记（展示但不扣分） |
| LCP/INP/CLS/TTFB | 纯静态产物天然优势；图片 srcset（media variant）落地后 Hero 图大小检查可静态预判 |
| canonical / schema | 检查「是否配置」而非运行时（构建产物确定性生成） |
| 图片大小 | 构建期可知产物图重（media variant 尺寸表），可静态评分 |

## 4. 评分引擎设计（自研计算器）

### 4.1 形态

- 位置：`internal/seo/scoring`（纯函数库，无 IO、无依赖注入）+ 前端侧栏 UI（workbench SEO 页签扩展）
- 输入 DTO：`ScoringInput{ Title, MetaDescription, URL, FocusKeyword, SecondaryKeywords, Headings[], BodyText, WordCount, Images[], InternalLinks, ExternalLinks, Locale }`
- 输出：`ScoringResult{ Total(0-100), Sections[]{ Key, Score, Max, Weight, Checks[]{ Pass, Actual, Benchmark, Hint } } }`
- 可测性：全部规则是纯函数 → 表驱动测试 + 边界用例（与 builder 确定性测试同标准）

### 4.2 首批规则集（来源标注）

| 规则 | 来源 | 要点 |
|---|---|---|
| 关键词密度 | Yoast 公开 | 0.5-2.0% 满分，>3% stuffing 告警 |
| 关键词位置 | Yoast 公开 | title/H1/首 100 词/URL/alt/meta 五点位 |
| Flesch 阅读容易度 | Yoast 公开 | 英文系直接用；中文按字数/句长变体（首版可先做句长/段长替代） |
| 标题像素宽度 | Yoast 公开 | 50-60 字符 + 像素宽（防 SERP 截断） |
| 标题结构 | rubric | 唯一 H1、层级递进、H2 覆盖面 |
| 内容长度 | rubric | 按查询类型分档（信息/商业/交易/本地） |
| 内外链计数 | rubric | 按篇幅基准表 |
| 图片 alt 覆盖率 | rubric+WCAG | 全部内容图有功能性 alt |
| 段落/句子长度分布 | Yoast 公开 | >20 词句子占比、>150 词段落告警 |
| 次级关键词/LSI | rubric | 2-3 个次级词出现检测 |

### 4.3 Yoast 复用策略（三选一，推荐 A）

| 路径 | 说明 | 取舍 |
|---|---|---|
| **A. 自研纯函数规则集**（推荐） | 按 §4.2 公开规则实现 Go 纯函数库 | 零许可争议、确定性好测、可控；工作量约等于把 rubric 翻译成代码 |
| B. 引入开源 `@yoast/seo` 前端包 | 编辑器内实时评分直接用官方引擎 | 省规则开发但 GPLv2（商用需评估）、中文规则支持弱、黑盒不可裁剪 |
| C. 反编译 premium bundle | 不做 | 混淆 min.js 解析成本高 + GPL 边界风险 + 分析器本来就不在 premium 里 |

premium 包的正确用法：当**产品功能参考**（redirects/IndexNow/social 预览），不当算法来源。

## 5. 与 SEO 基建的衔接（上一轮已定）

评分器是「建议」，meta 管道是「产物」，两者衔接：

- 构建期 meta Profile（OG/Twitter/JSON-LD/canonical 全量输出 + 三级默认回落）→ 评分器检查「显式配置了什么」
- sitemap/robots/IndexNow → 发布管线产物（IndexNow 借鉴 premium，发布激活后 ping）
- 评分器给出的改进建议可直接写回 settings.seo（显式按钮，不自动改）

## 6. 执行计划

| 步骤 | 内容 | 依赖 |
|---|---|---|
| 1 | `internal/seo/scoring` 纯函数库：规则集 §4.2 + 表驱动测试（含 rubric 基准表全部边界） | 无 |
| 2 | 中文阅读度变体规则（字数/句长/段长），英文先 Flesch | 步骤 1 |
| 3 | workbench SEO 页签扩展：评分侧栏 + SERP 预览 + 逐项建议跳转 | 步骤 1 |
| 4 | meta Profile 管道（OG/Twitter/JSON-LD/canonical + 三级默认回落） | 无，可与 1 并行 |
| 5 | sitemap.xml/robots.txt 发布产物 | 步骤 4 |
| 6 | IndexNow ping（发布激活后，借鉴 premium） | 步骤 5 |
| 7 | redirects 管理器（插件形态，参考 premium）——与 publication URL 占用协同 | 后置 |

> 规则来源版权说明：rubric 出自本地 SEO 工作区（自有资产）；Yoast 规则采用「公开算法思想重写」
> （关键词密度/Flesch/五点位均为公开方法论），不复制其代码。GPLv2 代码（若选路径 B）需单独法务评估。
