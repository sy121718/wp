# go_wp 架构概览

规则与判据只有一处：`AGENTS.md`。本文件只记 AGENTS.md 没有的**形状**：拓扑、核心概念、部署前提。

## 一句话

Go 单进程控制面（后台 + 构建 + 发布）+ 由已激活静态文件组成的访问面，中间隔着一条不可变产物管线。

## 技术栈

Go 1.27 · Gin · Jet（后台与构建期模板）· PostgreSQL（含 RLS 与分区）· Redis · HTMX + 原生 JS（无前端打包工程）· Casbin。

## 拓扑

```text
cmd/                    进程入口
  └─ routers/           唯一装配层
       ├─ internal/module/*        业务模块，跨模块只经 contract
       ├─ internal/templates       Jet 前端层（壳、片段、静态资产）
       ├─ internal/adminui         后台渲染出口
       ├─ internal/middleware      全局链 + 鉴权 + 访问面守卫
       └─ internal/pipeline        发布内核 → internal/builder 编译内核

访问面：PublicationStore 激活的静态文件 → 浏览器
```

## 核心概念

| 概念 | 是什么 | 不是 |
|---|---|---|
| Page | 绑定一个规范化 URL 的手工页面实例 | 内容实体的访问时模板 |
| PresentationInstance | 内容实体（商品/文章/分类）的自动发布页面实例 | 可在构建器里手工编辑的页面 |
| ContentTemplate | 内容实体的页面结构定义，每次构建参与派生 | 一次性初始化工具；运行时模板 |
| Blueprint | 新建页面文档时的版本化初始化输入 | 构建期模板；会传播到已有页面 |
| DocumentSnapshot | 构建期派生出的已解析完整 AST | 手工编辑的源码 |
| Artifact | 不可变产物（HTML + 内容寻址资源 + manifest） | 可原地修改的目录 |
| RuntimeFragment | 白名单处理器按需返回的 HTML 片段 | 页面渲染；数据库查询入口 |

## 部署形态：单实例

当前只能单进程部署。提到 2 个实例前，下表四处必须先改造，否则故障是「部分访客 404 / 限流额度 N 倍」且**不报错**。

| 组件 | 实现 | 多实例后果 |
|---|---|---|
| `ArtifactStore` | `pipeline.LocalStore`（本地磁盘） | A 发布的产物在 B 磁盘上不存在 → 404 |
| `PublicationStore` | `pipeline.LocalPublicationStore`（本地符号链接） | 激活状态不共享 → 同一 URL 指向不同版本 |
| 限流 | 进程内令牌桶 | 额度变 N 倍；重启清零 |
| 组件 schema 快照 | 进程内 `sync.Once` | 各实例 schema 可能不同，直到重启 |

改造方向：产物与激活状态换对象存储 + DB/Redis，限流换 Redis 令牌桶，schema 按 `Assembly.Fingerprint` 缓存。
备份恢复见 `scripts/BACKUP.md`。

## 已知限制

- 存量重构债（控制器侧取词、dto 展示字段、`c.PostForm`、`internal/shell` 迁移期包）按模块逐步收敛，判据见 `AGENTS.md`。
