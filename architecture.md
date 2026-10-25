---
id: go-wp-arch
type: architecture-design
title: go_wp 架构与不变量
status: active
parent: go-wp-goal
references: [go-wp-modules, go-wp-mod-routers, go-wp-mod-pipeline, go-wp-mod-builder, go-wp-mod-templates, go-wp-pkg, go-wp-mod-presentation]
---

# go_wp 架构与不变量

## 一句话

Go 单进程控制面（后台 + 构建 + 发布）+ 由已激活静态文件组成的访问面，中间隔着一条不可变产物管线。

## 技术栈

Go 1.27 · Gin · Jet（后台与构建期模板）· PostgreSQL（含 RLS 与分区）· Redis（必需）· HTMX + 原生 JS（无前端打包工程）· Casbin。

## 拓扑

```text
命令入口 cmd/
   └─ config/            组件注册与生命周期（logger → validate → database → migrations → casbin → i18n → cache → auth → upload → queue → captcha）
        └─ routers/      唯一装配层，线性不可重排
             ├─ internal/module/*   30 个业务模块，契约互通
             ├─ internal/templates  Jet 前端层（后台壳、片段、静态资产）
             ├─ internal/adminui    后台渲染出口
             ├─ internal/middleware 全局链 + 鉴权链 + 访问面守卫
             └─ internal/pipeline   发布内核
                   └─ internal/builder  编译内核

访问面：PublicationStore 激活的静态文件 → Static Server / CDN → 浏览器
        实时局部 → /_fragments/* → runtimefragment 白名单处理器 → HTML 片段
```

两条发布路径共享同一条管线：手工 Page 与自动 PresentationInstance 都走 编译 → ArtifactStore → PublicationStore 激活。

## 分层依赖

依赖方向单向：`cmd → routers → module/* → pkg/*`。`pkg` 不 import `internal/**`。

| 层 | 位置 | 只准 | 一律禁止 |
|---|---|---|---|
| 前端 | `internal/templates/**` | 结构、样式、取词、徽标分档、空态、分页、时间与金额显示 | 业务规则的第二份实现、金额求和、Go 代码 |
| 后端 | `internal/**`（除 templates）、`pkg/**`、`cmd/**` | 数据、规则、事务、事实 | HTML 字面量、给模板造形状 |
| 基础设施 | `pkg/**` | 横切能力 | import `internal/**`、业务语义 |

模块内部的分层（`contract` / `dto` / `enums` / `model` / `service` / `inbound` / `outbound`）与每层的准入禁令见 `internal/module/CLAUDE.md`；跨模块只能 import 对方的 `contract`。

这条判据目前是**逐条登记**的：`module_boundary_test.go` 只禁止清单里列出的那几对（含 `contenttemplate/model`、`membership/dto` 等若干历史欠债）。清单外的跨模块 dto 引用仍然存在（`cart` → `membership/dto`、`product` → `presentation/dto`、`navigation` → `content/dto`），它们不在门禁的句子里。

## 核心概念

| 概念 | 是什么 | 不是 |
|---|---|---|
| Page | 绑定一个规范化 URL 的手工页面实例 | CMS 内容的访问时模板 |
| PresentationInstance | 内容实体（商品/文章/分类）的自动发布页面实例 | 可在构建器里手工编辑的页面 |
| ContentTemplate | 内容实体的页面结构定义，每次构建参与派生 | 一次性初始化工具；运行时模板 |
| Blueprint | 新建页面文档时的版本化初始化输入 | 构建期模板；会传播到已有页面 |
| DocumentSnapshot | 构建期派生出的已解析完整 AST | 手工编辑的源码 |
| Artifact | 不可变产物（HTML + 内容寻址资源 + manifest） | 可原地修改的目录 |
| RuntimeFragment | 白名单处理器按需返回的 HTML 片段 | 页面渲染；数据库查询入口 |

## 不变量

1. **控制面与访问面分离**：访客请求不查库、不执行 Jet、不解释 AST；URL→文件由 PublicationStore 的文件状态决定。例外只有两处——`POST /analytics/collect`（只写一条、恒 204）与访问面守卫的只读会话检查（不查 PG、不写、不续期、fail closed）。
2. **两条发布路径共享同一管线**：Page 与 PresentationInstance 都走 编译 → ArtifactStore → PublicationStore。
3. **Blueprint 用完即弃**：创建后页面文档是独立快照；ContentTemplate 每次构建参与。
4. **Binding 不是 Query DSL**：文档只存白名单字段/集合/媒体绑定。
5. **确定性构建**：同一输入产生相同 Artifact 字节。
6. **冻结边界不可越权**：模块、组件、协议各有负责与禁止范围。
7. **构建期数据源依赖方向**：共享形状在 `internal/builder/source`；契约包不得反向 import `builder/core`（按传递闭包判）。

两条发布路径、编译内核与访问面的具体界限见 `go-wp-mod-page`、`go-wp-mod-presentation`、`go-wp-mod-pipeline`、`go-wp-mod-builder`。

## 写操作与数据

- 一次用户可感知的写操作涉及两处以上持久化写入时必须在同一事务；事务边界在 service，句柄从 model 下传（`…Tx` 方法）。
- 读-改-写必须行锁或原子 SQL；批量要么全成要么全败，拒绝时把可定位数据放进 `data.items`。
- 表只被一个模块读写；跨模块走契约。聚合口径的共享筛选条件抽成常量，多序列共用横轴时粒度一致。
- RLS 顺序固定：先 `pkg/rls.InProjectScope` → 再把连接角色换非超级 → 最后启用强制策略。带 FORCE 策略的表在缺少工程作用域时表现为「不存在」而不是报错。

## 响应与文案

| 出口 | 何时 | 形态 |
|---|---|---|
| JSON | 所有写动作、所有非页面请求、页面 GET 失败 | `{code,message,data}` + 真实状态码 |
| 页面视图 | 页面 GET 成功 | 控制器交出数据对象，router 上的 `adminui.PageSlot` / `BareSlot` 渲染 Jet |
| 字节流 | 构建预览、runtime fragment、下载、SSE | 直接写 body 后 `response.ErrHandled` |

文案两条链：响应走 `pkg/response`（键 → `pkg/facing` → i18n），界面走模板的 `t` / `tf`。

**取词的边界是「谁拥有词条」，不是「谁调 i18n」**：service 可以按 ctx 里的取词函数把声明处已有的事实取成当前语言（那只是一次内存查询），被禁的是自造词条与把成品句写进 dto 的语言绑定字段。详见 `internal/module/SPEC.md`。

## 采纳的规则文档

`AGENTS.md` 与 `internal/module/CLAUDE.md`、`internal/templates/CLAUDE.md`、`pkg/CLAUDE.md`、`public/test/CLAUDE.md` 是本架构的规则原文，保留在原位（仓库内大量事务性注释按 `AGENTS.md §章节` 引用它们，搬走会断链）。本文件与各模块 SPEC 不复述细则，只记录判据的形状。

## 部署形态：**单实例**

本架构当前**只能单进程部署**。这不是待优化项，而是一条硬前提：把实例数从 1 提到 2 之前，
下表四处必须先改造，否则故障形态是「部分访客 404 / 限流额度变成 N 倍」，而且**不会报错**。

| 组件 | 实现 | 多实例下的后果 |
|---|---|---|
| `ArtifactStore` | `pipeline.LocalStore`（本地磁盘） | 实例 A 发布的产物在实例 B 磁盘上不存在 → 命中 B 的访客拿到 404 |
| `PublicationStore` | `pipeline.LocalPublicationStore`（本地符号链接） | 激活状态不共享 → 同一 URL 在两个实例上指向不同版本 |
| 限流 | `builtin.RequestRateLimitMiddleware`（进程内令牌桶） | N 个实例 = 实际额度 N 倍；重启即清零 |
| 组件 schema 快照 | `workbench/inbound/http`（进程内 `sync.Once`） | 插件变更后各实例的检查器 schema 可能不同，直到各自重启 |

**改造方向**（不是本仓当前范围）：ArtifactStore / PublicationStore 换对象存储 + 统一的激活状态
（DB 或 Redis），限流换 Redis 令牌桶，schema 快照按插件指纹缓存（`Assembly.Fingerprint` 已提供
这个键）。四处都改完之后，「单实例」才成为可选而非前提。

**单实例 + 反向代理是当前被支持的生产形态**：静态产物由本地磁盘直接服务（访客请求不查库、
不执行 Jet，见不变量 1），这台机器就是唯一的权威。备份与恢复见 `scripts/backup.sh` / `scripts/restore.sh`（说明：`scripts/BACKUP.md`）。

## 已知限制

- **存量重构债**：控制器取词、dto 展示字段、`c.PostForm`、`shell.RenderJump`、`*_err.go` / `*_jump.go` 等旧形状仍在收敛中；`internal/shell` 为迁移期包，终局是删除。判据与台账见 `PLAN.md` 与各门禁测试。
