# 06-A · 插件生态路线图（规划）

> 内部过程文档（非对外使用）

> 本文回答「哪些能力做进本体、哪些做成插件、按什么顺序做」。
> 架构依据：[06-plugin-system.md](./06-plugin-system.md)（L0/L1/L2 三级能力 + 双轨制，权威）；
> 动态能力路径判断见 [04-B-dynamic-development-guide.md](./04-B-dynamic-development-guide.md)。

## 当前状态（冻结 · 明确不做）：插件生态不在当前路线内

**本轮决定：冻结。** 本体之外的插件生态**全部不在当前路线内** —— 不是「延后待补」，盘点与排期时
不得再把它当施工项。冻结范围：

- **L1 数据层与 L2 内容源**：L1 插件自有表（独立 PG schema `plugin_<id>`，安装时建 schema + 跑迁移）
  与 L2 内容源注册（`manifest.collections` → `collections.json`，即插件自有表经 CollectionSource
  白名单只读进可视化）；
- **`manifest.admin.menu`**：插件注册后台菜单；
- **插件包 `fragments/` 目录**：运行时片段不对插件开放；
- **`signature`**：zip 包签名校验 / 插件市场公钥验签；
- **受限事务 API**：service 层事务透传给受信插件；
- **第三轨**：Yaegi / Wasm 受限逻辑运行时；
- **插件市场与签名分发**；
- **首批 12 个插件**（§3 清单）与**组件包 packs**（§3 第 12 项）。

本文其余章节（§1 轨道判定、§3 首批清单、§5 依赖关系图）是冻结期的**设计备查** —— 它们记录
「将来重启时该怎么做」，不构成当前排期，也不代表这些项即将开工。

重启的判据见下节「决策记录」，其原文原样保留。

## 决策记录（2026-09-15）：插件生态延后

**决定**：插件生态（L2 集合源解析器、插件注册运行时片段 / 后台菜单、插件自声明权限点、事件总线与钩子、
安装包签名校验、注册表对插件开放）**整体延后**，先不做；等**本体收口完成**之后再启动。

**理由**：本体的四件事（内容 / 权限 / 可视化 / 发布管线）已落地，但仍在收敛期（审计里 `CQ-007` dashboard
上帝模块、`UIK-003` 四套视觉体系并行都属本体重构）。此时定插件契约，等于把一个还在移动的边界冻进对外 API ——
插件作者拿到的是随时会变的东西，代价由他们承担。插件的价值来自稳定内核，内核不稳时先修内核。

**影响**：审计里 9 条插件项标为延后（`OSS-001/002/003/004/005/007/008/019`、`SEC-005`），见各条的
`deferredNote`；`.wp` 插件安装与迁移的既有代码保留原样，不新增能力也不拆。

**重启条件**（三条同时满足再开）：① `CQ-007` 与 `UIK-003` 收口；② 本体的对外契约（`contract/` 与
文档里的稳定性分级）定稿；③ 至少有一个真实的第三方扩展需求，而不是「先把口子开着」。

**不属于本次延后的**：`OSS-018`（发行方式不是单二进制）—— 它关乎本体自身的部署形态，与本决策无关，仍按原计划推进。

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
需要新的检查器控件类型 / 新 CSS 原语  → 本体内核（扩白名单或 style 引擎原语）
需要新的 Go 组件（执行逻辑/读写数据）  → 重轨 Go 模块，或 0-D runtimefragment
强状态/并发/资金（扣库存/核销/支付）  → 重轨 Go 模块（可配插件壳提供管理页/展示）
构建产物的一部分（sitemap/meta）      → 本体管线
编辑器增强（分析/建议）              → 插件（只读，L0）
```

> 第三、四行是同一件事的两面：**插件不能携带可执行组件**（确定性构建 + 不执行第三方代码，
> 见 [06-plugin-system.md](./06-plugin-system.md) §11A.1）。撞到这条边界的判断与路径见
> [06-E-plugin-authoring.md](./06-E-plugin-authoring.md) §2。

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
| 3 | **商品 commerce** | 本体模块（v2） | — | ★★★ | 见 §4：直接注册商品集合源进可视化，商品卡组件后续可 L0 插件化 |
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
>
> 已经写好的三个档位范例（可直接安装，验收见 `public/test/plugin/unit/example_tiers_test.go`）：
> `examples/l0-style-only/`（纯样式声明）、`examples/l0-template-assets/`（模板片段 + 静态资产）、
> `examples/l1-marketing/`（L1 数据层 + 容器/主题查询）。字段参考与排错见
> [06-E-plugin-authoring.md](./06-E-plugin-authoring.md)。

## 4. 商品（commerce）：本体模块（v2 决策，见 06-B 变更记录）

定位：商品整体做本体模块 `internal/module/commerce`（与 content/presentation 同级）。
理由：模块自带隔离 + 菜单不启用即零消耗 + 商品是核心域 + **商品集合源可直接本体化注册进
CollectionResolver（提前解锁商品进可视化，不等插件 L2）**；商品卡组件/详情页预设后续仍可作 L0 插件分发。

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
受限事务 API + zip 签名 ──→ （原商品通道，v2 改本体后仅约束第三方电商插件）
商品 commerce 本体模块 ──→ 直接注册 content:product 集合源进可视化（不等插件 L2）
```

## 6. 决策备忘

- 表单/营销/组件包走轻轨 zip——对齐「WP 式杂七杂八插件」的预期；
- 商品/会员/积分核心走重轨模块（可配插件壳），资金与并发逻辑永不进明文 zip；
- SEO 基建（sitemap/canonical/JSON-LD/GSC 验证）进本体管线，Yoast 式分析做插件；
- 每落地一个插件或本体能力，同步更新 §2/§3/§6 与 06 §13 阶段表（同批提交）。
