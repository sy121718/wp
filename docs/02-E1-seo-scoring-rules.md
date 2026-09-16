# 02-E1 · SEO 评分规则表（实施用）

> 上游设计：`docs/02-E-seo-scoring-engine.md`（架构/分层/执行计划）。
> 本文件是**规则数据表**：每个检查项的基准、权重、红黄绿阈值、来源标注。
> 规则源：本地 SEO 工作区 `scoring-rubric.md`（自有资产）+ Yoast 公开方法论 + RankMath 公开文档 + Lighthouse 类目。
> 落地位置：`internal/seo/scoring`（纯函数、无 IO、表驱动）。

## 1. 权重卡（8 维度，合计 100）

| # | Section | Key | 权重 | Max | 检查项要点 | 来源 |
|---|---|---|---|---|---|---|
| 1 | 标题标签 | `title` | 15% | 15 | 主关键词在**前半段**；50-60 字符；唯一；有利益点/修饰；意图匹配；品牌置尾；截断风险低 | rubric + Yoast |
| 2 | 元描述 | `meta` | 5% | 5 | 关键词自然出现；150-160 字符；含 CTA；唯一；准确概括 | rubric + Yoast |
| 3 | 标题结构 | `headings` | 10% | 10 | **唯一 H1**；H1 含关键词；H1→H2→H3 层级递进；H2 覆盖关键侧面 | rubric |
| 4 | 内容质量 | `content` | 25% | 25 | 按查询类型长度；覆盖完整；独有价值；可读排版；阅读难度适中；E-E-A-T 信号 | rubric |
| 5 | 关键词优化 | `keywords` | 15% | 15 | 关键词出现在 title/H1/首 100 词/URL/alt/meta；2-3 个次级词；LSI 语义词；密度自然 | rubric + Yoast |
| 6 | 内外链 | `links` | 10% | 10 | 每千字 3-5 条上下文内链；目标相关；锚文本描述性；权威外链；无死链 | rubric |
| 7 | 图片优化 | `images` | 10% | 10 | 文件名描述性；体积达标；WebP/AVIF/SVG；首屏下懒加载；alt 完整 | rubric + WCAG |
| 8 | 页面技术 | `tech` | 10% | 10 | URL 干净；canonical 正确；移动友好；移动 LCP≤2.5s；HTTPS；结构化数据 | rubric + Lighthouse |

## 2. 基准表（数据化常量，改规则=改表）

### 2.1 内容长度（按查询意图）

| 查询类型 | 满分 | 部分分 | 差 |
|---|---|---|---|
| 信息型 informational | ≥1500 词 | 500-1499 | <500 |
| 商业型 commercial | ≥1200 | 400-1199 | <400 |
| 交易型 transactional | ≥500 | 200-499 | <200 |
| 本地型 local | ≥400 | 150-399 | <150 |
| 定义型 definition | ≥800 | 300-799 | <300 |

### 2.2 关键词密度

| 密度 | 得分影响 |
|---|---|
| 0.5-2.0% | 满分 |
| 2.0-2.5% | -1 |
| 2.5-3.0% | -2 |
| >3.0% | 密度项 0 分 + 标 stuffing |
| <0.5% | -1（除非查询本身不需要重复） |

> 印证：RankMath 建议区间 1-1.5%，主判仍用 rubric 的 0.5-2.0%。

### 2.3 内链数量（按篇幅）

| 篇幅 | 最低 | 理想 | 过多 |
|---|---|---|---|
| <500 词 | 2 | 2-4 | >8 |
| 500-1000 | 3 | 3-6 | >12 |
| 1000-2000 | 4 | 5-10 | >20 |
| 2000+ | 5 | 8-15 | >25 |

### 2.4 图片体积

| 类型 | 目标 | 格式 |
|---|---|---|
| Hero/横幅 | <200KB | WebP |
| 内容图 | <150KB | WebP |
| 截图 | <100KB | WebP/PNG |
| 图标/图形 | <30KB | SVG/WebP |
| 缩略图 | <50KB | WebP |

### 2.5 页面速度（Core Web Vitals）

| 指标 | 好 | 待改进 | 差 |
|---|---|---|---|
| LCP | ≤2.5s | 2.5-4.0s | >4.0s |
| INP | ≤200ms | 200-500ms | >500ms |
| CLS | ≤0.1 | 0.1-0.25 | >0.25 |
| TTFB | ≤800ms | 800-1800ms | >1800ms |

## 3. 评分与红黄绿（"红点"定义）

```text
总分 = Σ (Section 得分 / Section Max × 权重 × 100)
```

**总分等级**

| 区间 | 等级 | 判定 |
|---|---|---|
| 90-100 | A+ | 优秀，只需微调 |
| 80-89 | A | 良好，少量优化点 |
| 70-79 | B | 及格，若干项需处理 |
| 60-69 | C | 一般，需显著改进 |
| 50-59 | D | 偏下，存在重要问题 |
| <50 | F | 差，需整体重做 |

**单项色标（侧栏圆点用这个）**

| Section 百分比 | 色 | 含义 | 动作 |
|---|---|---|---|
| 90-100% | 🟢 绿 | 优秀 | 无需处理 |
| 70-89% | 🟢 浅绿 | 良好 | 可选优化 |
| 40-69% | 🟡 黄 | 需改进 | 本周内修 |
| 1-39% | 🔴 红 | 差 | 立即修 |
| 0% | 🔴 深红 | 缺失/损坏 | 阻塞项 |

## 4. 逐项检查函数（Go 落地签名）

```go
type Check struct {
    Key       string                  // "title_length"
    Label     string                  // "标题长度"
    Score     func(in *Input) (int, string) // 返回得分 + 实测值描述
    Max       int
    Benchmark string                  // "50-60 字符 / ≤580px"
    Hint      string                  // 改进建议（Playbook 映射）
}
```

首批检查项（按 Section）：

| Section | Check Key | 判定 |
|---|---|---|
| title | `title_length` | 50-60 字符满分；<30 或 >65 扣分 |
| title | `title_keyword_first_half` | 关键词出现在前半段 |
| title | `title_unique` | 全站唯一（需站点级输入） |
| meta | `meta_length` | 150-160 字符 |
| meta | `meta_has_cta` | 含动词型 CTA |
| headings | `single_h1` | 恰好 1 个 H1 |
| headings | `h1_has_keyword` | H1 含主关键词 |
| headings | `heading_hierarchy` | 无跳级（H2→H4） |
| content | `content_length` | 查 2.1 表 |
| content | `paragraph_length` | >150 词段落告警 |
| content | `sentence_length` | >20 词句子占比 |
| keywords | `keyword_density` | 查 2.2 表 |
| keywords | `keyword_positions` | 五点位命中数 |
| keywords | `secondary_keywords` | 2-3 个次级词出现 |
| links | `internal_link_count` | 查 2.3 表 |
| links | `external_authority` | ≥1 条权威外链 |
| links | `anchor_descriptive` | 无 "点击这里" 类锚文本 |
| images | `alt_coverage` | 内容图 100% 有功能性 alt |
| images | `image_weight` | 查 2.4 表 |
| images | `image_format` | WebP/AVIF/SVG 占比 |
| tech | `url_clean` | 无参数/无大写/长度 <75 |
| tech | `canonical` | 显式配置或可推导 |
| tech | `schema_present` | 按页型应有 JSON-LD |
| tech | `https` | 强制 HTTPS |

## 5. 页型调权（Profile）

| 页型 | 提高权重 | 降低权重 |
|---|---|---|
| 商品页 product | images / tech / schema | content 长文深度 |
| 长文指南 guide | content / keywords / links | images |
| 落地页 landing | tech / title / meta | content 深度 |
| 本地服务 local | tech / local schema / links | keyword density |

> 调权必须在结果里回显「本页型权重及理由」。

### 5.1 实现口径与未做部分

上表的页型档案在 `internal/seo/scoring` 里就是 `ProductProfile()` / `LandingProfile()` / `GuideProfile()` 三个函数，由 `ProfileFor(kind)` 统一选（生产入口唯一的选择处），`ScoreEntityPage` 是唯一入口。

- **已接线**（审计 SEO-016）：商品页走 product 档案，分类页与品牌页走 landing 档案；
  页面草稿与文章保持默认权重 —— 没有依据的页型不现造档案，`ProfileFor` 返回 nil 即默认。
- **一致性由测试钉住**：`TestProductProfileWeightsMatchDoc` 断言商品档案与上表逐项一致，
  `TestProfileForWiring` 断言页型 → 档案的映射，`TestScoreEntityPageEchoesProfile` 断言调权在结果里回显（本文档末尾的那条硬要求）。

**未做：站点级自定义权重**（审计 SEO-023）。当前的调权粒度是「页型」，站点自己改不了 ——
权重写在 `defaultWeights()` 与 `benchmarks.go` 的基准表里，改它要改 Go 代码重新编译。

先不做站点级配置的理由：上表的数字来自 rubric 的 Weight Adjustments 表，是有出处的经验值；
一旦开放站点自配，同一页型在不同站点的评分就不再可比，还需要配套一套「改完看什么」的界面。
代价大于收益，直到出现真实需求为止。

触发条件：出现「同一页型在不同站点该有不同权重」的实际诉求时再启动。届时的做法是把 `Profile` 变成可持久化的站点配置（含默认回落），而不是让每个站点从零配一遍。

## 6. 与开源算法印证（2026-09 调研，来源 docs/02-E §7）

| 维度 | 本地 rubric | Yoast（开源 content-analysis） | RankMath（公开文档） | Lighthouse |
|---|---|---|---|---|
| 标题长度 | 50-60 字符 | 像素宽 ≤600px | 50-60 字符 | — |
| 关键词密度 | 0.5-2.0% | 0.5-3% 提示 | 1-1.5% 建议 | — |
| 内容长度 | 按查询类型分档 | 300+ 词 | 建议 1500+ | — |
| 内链 | 按篇幅表 | 有建议 | 有建议 | 不检查 |
| 可读性 | Flesch + 中文变体 | Flesch（英文） | 有 | 不检查 |
| 图片 alt | 100% 覆盖 | 有 | 有 | 有 |
| 技术项 | canonical/HTTPS/schema | — | — | SEO 类目 |

**结论**：rubric 与 Yoast/RankMath 核心维度高度一致，且独有「按查询类型分档 + 按页型调权 + WCAG 叠加」，可作为主规则源。

## 7. 中文适配（首版策略）

| 项 | 英文 | 中文变体 |
|---|---|---|
| 可读性 | Flesch Reading Ease | 句长（>40 字告警）+ 段长（>200 字告警）+ 生僻词比例 |
| 分词 | 空格切词 | 词典分词（jieba 类）+ 关键词精确/模糊匹配 |
| 标题宽度 | 字符数 + 像素宽 | 中文字符按 2 倍宽估算 |

## 8. 诚实边界（不自动评分）

- E-E-A-T（作者资质/经验）→ 只给 Hint 引导人工
- 搜索意图匹配 → 只提示，不判定
- 竞品对比/内容独特性 → 不自动判
