# go_wp

go_wp 是一个 `CMS + Visual Website Builder + Static Publishing Engine`。

系统在管理控制面编辑内容与页面结构，在发布阶段将其编译为不可变的 HTML/CSS/JS Artifact，再由静态服务器或 CDN 直接交付。普通访客请求不解释模板，也不读取页面编辑态数据。

## 当前阶段

仓库当前处于基础框架整理阶段：

- 已保留 Go 管理控制面的启动、配置、认证、权限和基础设施能力
- 已接入 PostgreSQL 兼容的 `sys_i18n` migration 与内存缓存组件
- 已移除与本项目无关的旧业务模块和路由
- CMS、Visual Builder、Publish Compiler、ArtifactStore、PublicationStore 等核心能力将按设计文档分阶段实现

当前源码不包含独立前端工程。`internal/embed/dist/` 是已有的嵌入式管理端构建产物，不作为新业务源码的实现位置。

## 架构边界

```text
Admin
├── CMS Admin
└── Visual Builder
        │
        ▼
BuildContext + Component Registry
        │
        ▼
Publish Compiler
        │
        ▼
Immutable Artifact
        │
        ▼
ArtifactStore + PublicationStore
        │
        ▼
Static Server / CDN
```

核心约束：

- 控制面与访问面分离
- Page Document 保存页面结构，不复制 CMS 业务数据
- Jet 只在 Preview/Publish 构建阶段执行
- 公开 URL 激活不可变 Artifact，不在访问时动态套模板
- 不同语言使用 `/{lang}/{path}` 独立构建和发布
- 跨系统状态通过 receipt、inspect/verify 与恢复任务最终收敛
- 相同输入必须产生确定性构建结果

完整设计从 [`DESIGN.md`](DESIGN.md) 开始阅读。

## 技术栈

当前后端基础：

- Go + Gin
- GORM + PostgreSQL
- Viper
- JWT
- Casbin
- Redis
- Zap / lumberjack
- Asynq（按配置启用）

规划中的发布链路使用 Go 构建编译器、Jet 构建期渲染，以及可替换的 ArtifactStore / PublicationStore 实现。

## 目录结构

```text
go_wp/
├── cmd/                  # 进程入口
├── config/               # 配置与组件生命周期编排
├── docs/                 # 产品与系统设计
├── internal/
│   ├── embed/            # 嵌入式管理端构建产物
│   ├── middleware/       # HTTP 中间件
│   ├── module/           # 业务模块
│   ├── routers/          # 路由聚合
│   └── task/             # 异步任务注册
├── pkg/                  # 通用基础设施 facade 与 provider/driver
├── public/
│   ├── backup/           # 初始化与备份资源
│   ├── docs/             # 历史方案与辅助资料
│   ├── migrations/       # 数据迁移
│   └── test/             # 集成测试与测试支撑
├── scripts/              # 构建和运维脚本
├── config.yaml.example   # 配置样例
├── DESIGN.md             # 设计索引
└── README.md
```

## 模块分层

业务模块平铺在 `internal/module/`：

```text
module_name/
├── contract/             # 模块对外契约
├── dto/                  # 输入输出数据结构
├── enums/                # 响应与业务消息
├── inbound/http/         # HTTP 入口与装配
├── model/                # 本模块持久化访问
├── outbound/             # 可选的外部依赖适配
└── service/              # 业务用例
```

依赖规则：

- `inbound` 负责输入绑定与响应输出
- `service` 实现业务用例，只通过其他模块的 `contract` 跨模块调用
- `model` 只访问本模块数据表
- 通用技术能力放入 `pkg/`，不得扩散业务语义

当前管理控制面模块包括 `admin`、`permission`、`role`、`menu`、`dept`、`datarule` 和 `common`。具体约束见 `internal/module/CLAUDE.md`。

## 启动链路

后端采用显式生命周期编排：

1. `config.Init("config.yaml")`
2. `config.GetServer()`
3. `config.InitComponents()`
4. `middleware.Setup(router)`
5. `routers.SetupRoutes(router, config.ValidateReady)`
6. 启动 `http.Server`
7. 收到退出信号后关闭 HTTP，再逆序执行 `config.CloseComponents()`

组件初始化失败时返回错误，不自行结束进程。

## 本地运行

准备配置：

```bash
cp config.yaml.example config.yaml
```

按本地环境配置数据库、Redis、JWT、日志等项目，然后运行：

```bash
go mod download
go run ./cmd
```

健康检查：

- `GET /livez`
- `GET /readyz`

## 验证

```bash
go test ./...
go build ./...
go vet ./...
```

部分功能测试依赖 PostgreSQL、Redis 或其他外部组件；连接信息必须来自本地配置，不应硬编码。

## 设计文档

- `DESIGN.md`：设计索引与阅读顺序
- `docs/01-overview.md`：产品定位、架构与边界
- `docs/02-domain.md`：领域模型与数据表
- `docs/03-pipeline.md`：构建、发布、恢复与确定性规则
- `docs/04-runtime-and-delivery.md`：Runtime、交付阶段与测试策略

目录级开发规则：

- `internal/module/CLAUDE.md`
- `pkg/CLAUDE.md`
- `public/CLAUDE.md`
- `public/test/CLAUDE.md`