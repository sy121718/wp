# 2026-09 全面审查

本目录为审查结论与整改进度追踪。主索引是 [audit-2026-09.json](./audit-2026-09.json)，分维明细在 [dimensions/](./dimensions/)。

## 状态（2026-09-14 更新）

- **已 resolved**：115 条（**P0 已全部收口**：SEC-004 debug 护栏 / SEC-007 cookie Secure 单一判定 / SEC-009 响应 DTO 凭据守卫 / TX-016 订单域并发测试 / CQ-023 建单拆分；更早一批含 PERF-002 片段缓存、PERF-004 评分聚合、CQ-026 定价审计同事务等，见 `resolutionNote`）
- **仍 open**：153 条（含 **I18N-001** 后台全量抽 key、**OSS-001~007** 插件轻轨、**DB-004** 大表增长等大项）
- 批量标记脚本：`scripts/audit-mark-resolved.py`

## 怎么读

先看主索引的 `summary` 与 `verdicts`，再按关心的维度打开对应 JSON。每条 finding 的字段固定：

| 字段 | 含义 |
|---|---|
| `id` | 稳定编号（如 `UIK-014`），跨文档引用用它 |
| `severity` | `critical` / `high` / `medium` / `low` |
| `type` | 缺陷分类（design-flaw / implementation-bug / missing-feature / …） |
| `confidence` | 目前全部是 `confirmed`（有代码证据） |
| `problem` / `evidence` / `impact` / `rootCause` | 事实与判断分开写 |
| `remediation` | 建议方案 |
| `status` | `open` 或 `resolved` |
| `resolutionNote` | 已修复项的落地说明（仅 resolved 时有） |
| `phase` | 建议排期（P0–P8） |

## 分维文件

| 文件 | 维度 | 条数 |
|---|---|---|
| [01-visual-system.json](./dimensions/01-visual-system.json) | 组件 / 区块 / 页面 / 主题四层 | 15 |
| [02-dynamic-pages-editor.json](./dimensions/02-dynamic-pages-editor.json) | 动态页 × 可视化编辑器 | 18 |
| [03-i18n.json](./dimensions/03-i18n.json) | 多语言 | 25 |
| [04-seo.json](./dimensions/04-seo.json) | SEO | 25 |
| [05-performance.json](./dimensions/05-performance.json) | 性能与内存 | 21 |
| [06-code-quality.json](./dimensions/06-code-quality.json) | 边界、可读性、体系整合 | 26 |
| [07-plugin-opensource.json](./dimensions/07-plugin-opensource.json) | 插件与开源就绪 | 21 |
| [08-ui-multidevice.json](./dimensions/08-ui-multidevice.json) | 多端、无障碍、编辑器体验 | 22 |
| [09-postgres-schema.json](./dimensions/09-postgres-schema.json) | PostgreSQL 结构与约束 | 24 |
| [10-index-lifecycle.json](./dimensions/10-index-lifecycle.json) | 索引与数据生命周期 | 20 |
| [11-security.json](./dimensions/11-security.json) | 安全 | 16 |
| [12-transaction-correctness.json](./dimensions/12-transaction-correctness.json) | 事务、并发、金额 | 16 |
| [14-ui-kit-and-registry.json](./dimensions/14-ui-kit-and-registry.json) | **控件基座、基础动画、组件注册** | 19 |

合计 **268** 条。没有 `13-*`：运维/CI 并入 `07`，库结构与生命周期拆成 `09`/`10`。

## 控件基座 + 动画 + 注册（维 14 摘要）

1. 产物侧白名单与协议已部分收口（`UIK-001`/`UIK-014` resolved）；toast/busy 仍仅后台。
2. `prefers-reduced-motion` 已无条件注入产物（`UIK-006` resolved）。
3. 内置组件继续 Go 注册；插件走 manifest（`REG-003` 仍为 open 设计决策）。
