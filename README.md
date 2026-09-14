# go_wp

go_wp 是 CMS、可视化建站工具与静态发布引擎。后台编辑页面文档和内容，构建器把它们编译为不可变 HTML/CSS/JS，发布存储负责激活 URL。普通静态访问不执行模板、不查数据库；需要实时数据的功能经独立的受限 Runtime Fragment 端点提供。

项目处于开发阶段，协议和扩展接口仍在整理。CMS、工作台、页面与自动实例发布、回滚、组件库、主题、插件注册以及商品与库存业务均已有实现。它们的成熟度和接入范围不同，不能把「模块存在」等同于已具备完整的开源发行与插件生态。

## 架构

```text
CMS 内容 + Page / ContentTemplate
              ↓
DocumentSnapshot + BuildContext + Registry
              ↓
Publish Compiler → 不可变 Artifact
              ↓
ArtifactStore → PublicationStore 激活 → Static Server / CDN

实时交互 → Runtime Fragment Registry → 受限业务能力 → HTML Fragment
```

Page 是手工页面；PresentationInstance 是由内容与展示模板生成的自动发布实例。两者共享编译与发布管线。Blueprint 只初始化页面，后续修改不传播。

技术栈为 Go 1.26、Gin、Jet、PostgreSQL、Redis、Session + Cookie 和 Casbin。后台由 Jet 渲染 HTML，交互使用原生 JavaScript 与 HTMX，没有独立前端打包工程。Redis 是必需的关键组件。

## 目录

| 路径 | 职责 |
|---|---|
| `cmd/`、`config/` | 进程入口、配置与生命周期装配 |
| `internal/builder/` | 文档协议、组件注册、受限样式编译、产物生成 |
| `internal/builder/components/` | 组件实现及就近嵌入的模板、样式、增强资产 |
| `internal/module/` | CMS、页面、发布、后台及业务模块；跨模块经契约协作 |
| `internal/templates/` | 后台与工作台模板、公共控件和静态资产 |
| `pkg/` | 基础设施能力 |
| `public/migrations/` | 数据迁移 |
| `public/test/` | 功能、集成测试与测试支撑 |
| `docs/` | 架构、协议、开发指南 |

组件模板与样式、部分构建输入通过 `go:embed` 打入二进制。后台模板和 `/static` 当前仍从文件系统提供；部署不能只复制一个二进制后假定后台资产齐全。

## 本地运行

准备 PostgreSQL 与 Redis，然后配置本地连接及会话密钥：

```bash
cp config.yaml.example config.yaml
go mod download
go run ./cmd
```

健康检查为 `GET /livez` 和 `GET /readyz`。生产构建在 Git 工作区执行包路径命令，保留 VCS 构建信息供组件变更识别：

```bash
go build -o app ./cmd
```

组件实现变化后，已发布 Artifact 不会自行改变；启动时可标记待重建页面，再经重建或发布流程更新。

## 生产部署检查清单

debug 模式带三处放宽 —— 一键登录入口、CORS 反射任意 Origin、信任所有代理，
所以下面几条不是建议而是发布前必须核对的取值：

| 配置 | 生产取值 | 误配的表现 |
|---|---|---|
| `server.mode` | `release` | debug 实例注册 `/admin/dev-login`（无需凭据的登录入口）、所有响应带 `X-GOWP-Debug: 1`、未配白名单时反射任意 Origin |
| `server.cors_allowed_origins` | 显式列出后台域名 | release 下白名单为空会拒绝一切跨域（fail-closed，不会静默放开），需要跨域访问的运维台连不上 |
| `server.site_https` | `true` | 站点对外的协议真源。未显式配置时 release 默认 `true`；只有明确声明纯 HTTP 站点才写 `false`。错写 `false` 会让会话 cookie 丢掉 `Secure` |
| `server.debug_allow_public` | `false` | 为 `true` 时 debug/test 绑定全部网络接口（默认只绑 `127.0.0.1`），启动日志会有一条醒目警告 |
| `auth.session_secret` | ≥32 字符随机值 | release 下弱密钥直接拒绝启动 |

反向代理必须正确传递 `Host`（否则产物里的绝对链接指向错误域名）。
`X-Forwarded-For` 与 `X-Forwarded-Proto` 当前**不参与任何安全判定**：release 模式
`SetTrustedProxies(nil)`，`c.ClientIP()` 恒取直连对端，cookie 的 `Secure` 由
`server.site_https` 决定。也就是说伪造这两个头影响不了 cookie 安全属性；
代价是前置反代时审计日志记到的是反代地址而不是真实访客 IP。
## 验证与契约生成

```bash
go test ./...
go vet ./...
go build ./...

# 工作台完整检查：要求 Node，只用于开发验证，不参与生产打包
bash scripts/check-workbench.sh

# 修改组件的对齐重复项声明后生成；生成文件随源码提交
go run ./cmd/workbench-contracts
go run ./cmd/workbench-contracts -check
```

普通 `go test` 会检查生成文件漂移；没有 Node 时部分 JS 检查会跳过，完整工作台检查则明确失败。数据库测试应使用专用测试实例，可通过 `PGHOST`、`PGPORT`、`PGUSER`、`PGPASSWORD`、`PGDATABASE` 和 `TEST_REDIS_ADDR` 配置；部分测试支持容器回退。验收时须检查 skip 原因，不能只看退出码。

不依赖数据库的交互验证页可按需启动，监听回环地址，15 分钟后退出：

```bash
GOWP_REPEATER_BROWSER=1 go test ./public/test/dashboard/feature \
  -run TestRepeaterBrowserFixture -v -timeout 20m
```

它使用真实面板、控件、撤销逻辑与编译器，测试文档只保存在内存。它不覆盖完整登录、持久化发布或画布交互。

## 阅读入口

- `docs/11-foundation-and-open-source.md`：2026-09-12 基础体系审视、本轮优化、开源准备与后续验收。
- `docs/01-overview.md`：产品定位与冻结边界。
- `docs/02-domain.md`、`docs/03-pipeline.md`：领域模型、发布与恢复。
- `docs/02-F-ui-kit.md`：公共控件、视觉与投递边界。
- `docs/04-B-dynamic-development-guide.md`：动态领域与构建期数据源接入。
- `AGENTS.md` 及目录规则：仓库开发约定。

开源发行尚需确定许可证、贡献与安全报告流程，并建立自动检查和可复现发行包；这些不由已有业务模块数量替代。
