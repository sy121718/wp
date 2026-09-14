# API 稳定性分级

> 最后核对：2026-09-13。路由形态：`/api/{module}/{action}`，**无** `/v1` 前缀（大规模改路径成本高且收益不明确）。

## 1. 分级

| 等级 | 含义 | 变更策略 |
|---|---|---|
| **stable** | 后台与集成可依赖 | 破坏性变更须大版本/迁移说明；优先加字段、加接口，少改语义 |
| **experimental** | 已挂载但契约可能调整 | 可在 minor 版本变更；集成方需跟进 CHANGELOG |
| **internal** | 仅服务本仓库后台/构建流程 | **不对外承诺**；路径或响应形状可随时改 |

未在本文列出的 `/api/*` 默认视为 **internal**，除非模块文档明确标注 stable。

## 2. 当前 stable 接口（摘要）

面向「自建后台 / 运维脚本 / 同版本二次开发」的稳定面（Session + CSRF + Casbin，除另有说明）：

- **认证**：`POST /api/admin/login`、`GET /api/captcha`（公开）
- **工程与主题**：`/api/project/*`（list/get/create/update、主题 list/activate/settings）
- **页面生命周期**：`/api/page/{create,save-draft,build,publish,rollback,list,get,...}`（手工页核心链）
- **发布与产物**：`/api/publication/*`、`/api/artifact/*`（激活、回滚、审计）
- **内容与自动发布**：`/api/content/*`、`/api/presentation/*`（含 preview、update-url）
- **商品只读聚合**（若做外部 ERP 同步）：`/api/product/list`、`/api/product/get` 等查询类（写路径仍随业务演进）

**公开访问面**（无 Session，靠接口形状收窄）：

- `POST /analytics/collect` — 仅写匿名浏览记录，204 空体
- `POST /payment/callback` — 支付通道服务端回调，验签
- `GET /_fragments/{type}` — Runtime Fragment，按 Registry 白名单
- `/user/*` — 访客账号（与 `/api/admin/*` 隔离）

## 3. experimental / internal 示例

| 路径模式 | 等级 | 说明 |
|---|---|---|
| `/workbench/*` | internal | 可视化工作台 HTMX 片段，随编辑器迭代 |
| `/admin/*` 页面 | internal | Jet SSR，非 JSON 契约 |
| 新增权限点对应的 CRUD | experimental → stable | 新模块上线后观察一版再标 stable |

## 4. 破坏性变更沟通

1. 仓库根目录 **CHANGELOG**（或 Release Notes）记录 API 语义变更、权限点新增、迁移编号。
2. 需要数据迁移的变更：迁移 SQL 注释写清「从哪版起」与回滚限制。
3. **后续**新增的、明确对外的 HTTP API 建议使用 `/api/v1/...` 前缀；现有路径保持不动。

## 5. 相关文档

- `AGENTS.md` — 路由、三层链、权限点 seed 约定
- `docs/11-foundation-and-open-source.md` — 开源与贡献流程（LICENSE / CONTRIBUTING 待维护者补齐）
