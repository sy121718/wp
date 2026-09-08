# 06-B · ADR：能力开发双轨制与商品插件定位（决策固化）

> 本文把「插件 vs 原生模块」的对话决策固化为架构记录（ADR），避免后续反复。
> 上游：[06-plugin-system.md](./06-plugin-system.md) §4（双轨制规范）、
> [06-A-plugin-ecosystem-roadmap.md](./06-A-plugin-ecosystem-roadmap.md)（生态路线图）。

## 决策 1：不「统一走插件」，按逻辑轻重分轨

| 轨 | 形态 | 适用 | 例 |
|---|---|---|---|
| 轻轨 | zip 数据插件（L0/L1/L2） | 纯展示 + 弱逻辑，营销类 80% | 表单/活动/组件包 |
| 重轨 | Go 编译期模块（可配插件壳） | 强状态/并发/资金 | 商品核心/订单/支付/积分 |
| 本体 | 管线内置 | 构建产物组成 | SEO 基建/sitemap/meta |

理由（否决「统一走插件」）：
- 事务一致性：扣库存等跨表事务必须 service 层 `Transaction()` 编排，zip 沙箱做不到；
- 并发正确性：需要 `SELECT FOR UPDATE` + 原子更新 + race 测试背书；
- 安全：当前 zip 是明文无签名（06 §5.4 预留未做），资金/核销逻辑进 zip 等于裸奔；
- 性能：强逻辑走解释器有 1/2~1/20 损耗（06 §4.3 已否决第三轨）。

## 决策 2：本体收口四职责

文章（content/contenttemplate/presentation/page）、用户（admin + AuthzContextService 契约）、
可视化（dashboard/workbench/builder）、发布管线（build→artifact→publication）。
商品/会员/积分等业务域**不进**原生模块表——收口保持本体精简。

## 决策 3：admin 契约的双消费口

| 消费方 | 路径 | 场景 |
|---|---|---|
| 重轨原生模块 | 直引 `admincontract.AuthzContextService` | 编译期强逻辑判断操作员权限 |
| 轻轨 zip 插件 | 经 plugin 模块 `pluginSvc.AdminAuthz()` 转贷 | 插件渲染/管理页拿角色权限（插件不得 import admin service/model） |

两个口子都已就位，新模块按轨道选口，不再增设中转。

## 决策 4：商品 = 重轨模块 + 插件壳

```
重轨（Go 模块）                      插件壳（zip）
商品/分类/SKU/库存 model              商品卡/列表组件（L0+L2 集合源）
扣库存事务（FOR UPDATE）             详情页预设（presets）
订单状态机 + 支付回调验签             后台管理页增强
优惠码幂等核销                        Product JSON-LD 片段
经 contract 对外（CollectionResolver/DTO）
```

前置硬骨头（按序，缺一不做商品核心）：
1. L2 CollectionSource（硬依赖 0-A2 content）——商品数据进工作台命脉；
2. 插件受限事务 API——就位前扣库存只能留重轨；
3. zip 签名校验——商品插件分发底线。

## 决策 5：正文的「富文本 ⇄ 可视化」等价转换（双视图单真源）

- 底层唯一真源 = 组件树（AST 片段森林）；富文本只是编辑视图；
- 等价规则：块级标签 ↔ 组件一一映射（h2→heading/p→text/img→image/ul→list/…），
  行级格式（strong/em/a）留在 core.text 富文本片段内；双向唯一、round-trip fuzz 背书；
- 白名单外标签降级不静默（剥壳保内容→core.text+警告）；
- 复杂组件在富文本视图显示为只读占位块（Notion 模式）；
- 不做 WP Gutenberg 式区块编辑器（投入大、与 workbench 重叠、作者体验差）。

## 决策 6：SEO 分工

- 基建（sitemap/canonical/OG/JSON-LD/GSC 验证）→ 本体管线（影响 Artifact 字节，必须确定性）；
- 评分器（Yoast 式）→ 编辑期只读建议，永不进构建管线；规则源=本地 rubric + 公开算法印证
  （详见 [02-E-seo-scoring-engine.md](./02-E-seo-scoring-engine.md)）；
- redirects/IndexNow/social 预览 → 插件/管线增强（参考 Yoast premium 产品形态）。

## 变更记录

### v2（2026-09）：商品改做本体模块

**决策 4 作废**「商品 = 重轨模块 + 插件壳」，改为：**商品整体做本体模块**（`internal/module/commerce`）。

变更理由：

1. 本体模块自带隔离（internal/module 表隔离约定 + 独立迁移），插件壳的隔离收益不再显著；
2. 后台菜单/权限点可控——不启用即不出现入口，不产生运行消耗（挂载矩阵已有先例）；
3. 商品是站点的核心域（不是可选增值），与 content/presentation 同级更符合领域定位；
4. 简化 L2 依赖：商品做本体后可直接向 builder 的 CollectionResolver 注册商品集合源
   （`content:product` 通道本体化），不必等插件 L2 注册机制，商品进可视化提前解锁；
5. 插件壳（商品卡组件/详情页预设）后续仍可作为 L0 插件独立分发，与本体模块不冲突。

会员/积分维持原决策（重轨模块），是否本体化视站点形态再定。
