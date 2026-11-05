# go_wp 架构概览

**规则与判据只有一处：[AGENTS.md](AGENTS.md)。** 本文件只记 AGENTS.md 没有的**形状**：拓扑、核心概念、部署前提。

## 一句话

Go 单进程控制面（后台 + 构建 + 发布）+ 由已激活静态文件组成的访问面，中间隔着一条不可变产物管线。

## 架构风格

**MVC + Service**：Controller 选模板并带业务 data → Jet 渲染；Service 承载业务规则与事务；Model 管本模块表；View 是 Jet 模板 + HTMX。
前端开发体验对标 Laravel Blade（模板继承 / 布局 / 组件复用 / 全局取词 / 表单与统一交互），是**体验目标**，不引入 Laravel 依赖、不照搬其全部功能。

## 技术栈

Go 1.27 · Gin · Jet（后台与构建期模板）· PostgreSQL（含 RLS 与分区）· Redis · HTMX + 原生 JS（无前端打包工程）· Casbin。

## 拓扑

```text
cmd/                    进程入口
  └─ routers/           唯一装配层
       ├─ internal/module/*        业务模块，跨模块只经 contract
       ├─ internal/templates       Jet 前端层（壳、片段、静态资产）
       ├─ internal/text            按请求语言注入 t/tf/tn（共享渲染基础设施）
       ├─ internal/adminui         后台共享渲染基础设施
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
| 后台页面 | Go + Jet 在控制面实时渲染（Controller 选模板 + 业务 data） | 访问面产物；运行时 HTML 片段 |
| 访客业务 API | 明确注册的 JSON 端点（购物车/订单/搜索等） | 页面渲染链路；运行时 HTML 片段 |

> **访问面的运行时 HTML 片段出口已退役**：原 22 项动态片段能力已迁为所属模块 JSON 或清退，不得恢复（[AGENTS.md](AGENTS.md) 架构不变量 1）。

## 响应协议

见 [AGENTS.md](AGENTS.md) 的「响应协议」一节（页面 GET 成功回模板，所有非页面请求与页面失败回 JSON `{code, message, data}`，字节流走预览/下载/SSE 例外）。本文件不复制协议表。

## 部署形态：单实例

当前只能单进程部署。提到 2 个实例前，下表四处必须先改造，否则故障是「部分访客 404 / 限流额度 N 倍」且**不报错**。

| 组件 | 实现 | 多实例后果 |
|---|---|---|
| `ArtifactStore` | `pipeline.LocalStore`（本地磁盘） | A 发布的产物在 B 磁盘上不存在 → 404 |
| `PublicationStore` | `pipeline.LocalPublicationStore`（本地符号链接） | 激活状态不共享 → 同一 URL 指向不同版本 |
| 限流 | 进程内令牌桶 | 额度变 N 倍；重启清零 |
| 组件 schema 快照 | 进程内 `sync.Once` | 各实例 schema 可能不同，直到重启 |

改造方向：产物与激活状态换对象存储 + DB/Redis，限流换 Redis 令牌桶，schema 按 `Assembly.Fingerprint` 缓存。
备份与恢复（含 `restore.sh --drill` 真恢复演练）见 [scripts/README.md](scripts/README.md)。

## 待核对的代码差距

完整清单在 [TODO.md](TODO.md)（**待核实**，不是规范）。以下几条来自文档整理时的阅读印象，尚未逐条按当前代码核对——不是实测结论，也不是留下的许可。动手前先核实，并把核实结果落到具体文件：

- 待核对：控制器侧是否仍有取词残留；展示分档（`Tone`/`Kind` 一类）是否仍在后端计算并由 service 交成品标签。
- 待核对：`dto` 里是否仍有换语言要重算的成品句；`c.PostForm` 是否已全部改走 `binding`。
- 待核对：`internal/shell` 是否仍作为转发层存在（终局是删除）。
- 待核对：页面失败是否仍有渲染成页面的出口；是否仍有模块保留旧包装链。
- 待核对：前端模板统一封装的完成度，见 [internal/templates/CLAUDE.md](internal/templates/CLAUDE.md) 的「待实施」清单。
- 已确认的现状：`internal/templates` 里同时有模板资产与渲染基础设施 Go（引擎适配、模板函数、嵌入）；`pkg/view` 只做接线与失败出口。

判据见 [AGENTS.md](AGENTS.md)；架构门禁在 `internal/architecture/`。