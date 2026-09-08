# 06-A · 插件生态路线图（规划）

> 本文回答「哪些能力做进本体、哪些做成插件、按什么顺序做」。
> 架构依据：[06-plugin-system.md](./06-plugin-system.md)（L0/L1/L2 三级能力 + 双轨制，权威）；
> 动态能力路径判断见 [04-B-dynamic-development-guide.md](./04-B-dynamic-development-guide.md)。

## 0. 本体收口（不做的都出去）

本体只保留四件事，其余一律插件化或重轨模块化：

| 本体职责 | 现状 |
|---|---|
| 文章/内容（content + contenttemplate + presentation + page） | ✅ 已落地 |
| 用户/权限（admin，经 `contract.AuthzContextService` 对外） | ✅ 已落地 |
| 可视化（dashboard + workbench + builder 内核） | ✅ 已落地 |
| 发布管线（build → artifact → publication） | ✅ 已落地 |

SEO 基建（sitemap/meta/结构化数据）是构建产物的组成部分，做进本体管线；
SEO 分析器（Yoast 式评分）做插件（编辑器增强，只读分析）。

## 1. 轨道判定速查

```
纯展示/弱逻辑（无表或简单表）        → 轻轨 zip 插件（L0/L1）
需要数据进可视化绑定                 → 轻轨 + L2（等 CollectionSource 落地）
强状态/并发/资金（扣库存/核销/支付）  → 重轨 Go 模块（可配插件壳提供管理页/展示）
构建产物的一部分（sitemap/meta）      → 本体管线
编辑器增强（分析/建议）              → 插件（只读，L0）
```

## 2. SEO 能力（本体基建 + 插件增强）

### 2.1 本体：构建期 SEO 基建（发布管线内置）

| 能力 | 落点 | 说明 |
|---|---|---|
| sitemap.xml / robots.txt | Publish Compiler 产物 | 每次 Publication 激活后重新生成，含全部已发布 URL（含 presentation 自动页）；多语言预留 hreflang |
| canonical / OG / Twitter 卡片 | Page settings.seo → 构建期 head 输出 | 字段白名单进 Page/ContentTemplate 的 settings schema，工作台 SEO 页签编辑 |
| 结构化数据（JSON-LD） | 构建期组件输出 | Article/Product/BreadcrumbList 按页面类型自动注入；商品结构化数据由商品插件经 L2 数据提供 |
| Google Search Console 验证 | SiteSettings | 验证文件/元标签配置，发布时落到根路径 |
| 性能基建（Core Web Vitals） | 构建产物 | 纯静态零 JS 布局已是最大优势；图片变体（media variant）按视口出 srcset |

> 原则：SEO 基建影响 Artifact 字节与 URL 结构，必须确定性构建，不能进插件。

### 2.2 插件：SEO 分析器（Yoast 式，L0）

- 内容分析：关键词密度/标题宽度/内链数/可读性（编辑期实时计算，纯前端或 fragment）
- 元描述/标题长度预览（Google SERP 快照预览）
- 只读建议，不写 Artifact——落 L0，无自有表；进 workbench 检查器侧栏

## 3. 首批插件规划（按优先级）

| # | 插件 | 轨道 | 依赖 | 优先级 | 说明 |
|---|---|---|---|---|---|
| 1 | **表单 forms** | L1 (+L2) | — | ★★★ | 报名/联系/调研表单：自有表（submissions）+ 后台管理页 + 表单组件（L0）+ 提交走白名单 fragment（POST+CSRF）。营销基建 |
| 2 | **营销 marketing** | L1 + L2 | L2 落地 | ★★★ | 优惠码/活动/倒计时/Hero 预设（L0 已能做）；活动列表进可视化需 L2 CollectionSource |
| 3 | **商品 commerce** | 重轨模块 + 插件壳 | L2 + 受限事务 API | ★★★ | 见 §4 专述 |
| 4 | **SEO 分析器** | L0 | — | ★★☆ | §2.2，编辑器增强 |
| 5 | **统计 analytics** | L0/L1 | — | ★★☆ | GA4/gtag 注入（SiteSettings 级）+ 访问计数（自有表，经 fragment/像素） |
| 6 | **会员 membership** | 重轨 + 壳 | admin 访客账号领域 | ★★☆ | 访客账号是本体缺口（AGENTS.md 已预留「另建领域模块」）；等级/权益重轨，展示层插件 |
| 7 | **积分 points** | 重轨 + 壳 | membership | ★☆☆ | 流水对账/幂等，纯重轨 |
| 8 | **多语言 i18n** | 本体管线 + 插件 | publication URL 结构 | ★☆☆ | hreflang 本体管；翻译工作流可插件 |
| 9 | **评论 comments** | L1 (+L2) | 访客身份 | ★☆☆ | 提交走 fragment（限流+审核）；展示构建期静态 + 增量 fragment |
| 10 | **搜索 search** | fragment | — | ★☆☆ | 站内搜索 capability（白名单枚举排序），静态站标配补丁 |
| 11 | **邮件 smtp** | L1 | — | ★☆☆ | SMTP 配置 + 队列（表单/会员通知依赖） |
| 12 | **组件包 packs** | L0 | — | 随需 | 轮播/动效/图标库/定价表/团队墙/时间线——纯 L0，上传即用（对标 WP 模板市场） |

> 组件包（L0）是生态走量主力：无表无迁移，样式声明引擎（06 §6）+ presets 已全部就绪，**现在就能写**。

## 4. 商品（commerce）：重轨模块 + 插件壳

定位：商品是「数据模型 + 事务 + 状态机」，核心逻辑必须编译期；插件壳提供可视化展示与管理页。

```
本体/重轨（Go 模块，编译期）          插件壳（zip，轻轨）
├─ 商品/分类/SKU/库存 model            ├─ 商品卡/商品列表组件（L0，吃 L2 集合源）
├─ 扣库存事务（SELECT FOR UPDATE）     ├─ 详情页预设（presets）
├─ 订单状态机 + 支付回调验签           ├─ 后台管理页增强（Jet 模板）
├─ 优惠码幂等核销                      └─ 结构化数据 JSON-LD 片段
└─ 经 contract 对外（CollectionResolver/DTO）
```

前置硬骨头（按序）：
1. **L2 CollectionSource 落地**（硬依赖 0-A2 content）——商品数据进工作台的命脉；
2. 插件**受限事务 API**（service 层 Transaction 透传给受信插件）——在此之前扣库存只能留重轨；
3. zip **签名校验**（06 §5.4 预留）——商品插件分发安全底线。

## 5. 依赖关系图（落地顺序）

```
本体 SEO 基建(sitemap/meta) ──独立，可立即做
表单 forms（L1）──────────── 独立，可立即做 ★ 首推第一个练手插件
组件包 packs（L0）────────── 独立，可立即做
0-A2 content ──→ L2 CollectionSource ──→ 营销/商品展示/评论列表
admin 访客账号领域 ──→ membership ──→ points / 评论身份
受限事务 API + zip 签名 ──→ 商品重轨核心（订单/支付）
```

## 6. 决策备忘

- 表单/营销/组件包走轻轨 zip——对齐「WP 式杂七杂八插件」的预期；
- 商品/会员/积分核心走重轨模块（可配插件壳），资金与并发逻辑永不进明文 zip；
- SEO 基建（sitemap/canonical/JSON-LD/GSC 验证）进本体管线，Yoast 式分析做插件；
- 每落地一个插件或本体能力，同步更新 §2/§3/§6 与 06 §13 阶段表（同批提交）。
