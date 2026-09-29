# go_wp

[English contributor documentation](docs/README.en.md)

go_wp 是 CMS、可视化建站工具与静态发布引擎。后台编辑页面文档和内容，构建器把它们编译为不可变 HTML/CSS/JS，发布存储负责激活 URL。普通静态访问不执行模板、不查数据库；需要实时数据的功能经独立的受限 Runtime Fragment 端点提供。

项目处于开发阶段，协议和扩展接口仍在整理。上面这些能力的成熟度与接入范围各不相同，不能把「模块存在」等同于已具备完整的开源发行与插件生态。

## 能力概览

下面这些是**已经跑通并带测试**的路径，不是规划清单：

- **可视化建站**：组件库 + Page Document 文档协议 + 工作台画布；组件自带模板、样式与增强资产，经 `go:embed` 打进二进制。
- **自动发布实例**：内容与展示模板组合成 PresentationInstance，与手工 Page 共用同一编译与发布管线。
- **静态发布引擎**：不可变 Artifact + 内容寻址资源 + URL 激活与回滚；发布前可预览，且预览字节 == 发布字节。
- **受限动态片段**：Runtime Fragment Registry 提供购物车、商品筛选、实时可用量、站内搜索等能力，都是白名单只读或纯计算，不把访客请求带进应用主链路。
- **CMS**：文章、媒体库、复用区块、分类与标签。
- **商品与库存**：商品 / 变体 / 属性 / 分类 / 品牌 / 标签 / 定价工具 / 捆绑配置；仓库、库存真源、流水、物料清单、货源、采购入库。
- **交易链路**：购物车（客户端签名 cookie）、结算、订单状态机、优惠码、退货入库、访客订单自助查询。
- **站点运营**：SEO 评分、sitemap、结构化数据、内链建议、多语言站、多主题、访问统计、邮件、访客账号。
- **治理**：Casbin 三层鉴权链、权限点与菜单 seed、主数据字段级审计（append-only）、构建任务队列。

各模块的完整职责与边界见 [docs/13-module-inventory.md](docs/13-module-inventory.md)。

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

## 快速开始

需要 Go 1.26+、GNU Make、本机 PostgreSQL 与 Redis。开发阶段不依赖 Docker。三条命令：

```bash
make migrate   # 使用管理连接执行结构迁移与 seed
make dev       # 开发模式，air 热重载
```

然后访问 `http://127.0.0.1:8080`。不带参数运行 `make` 会列出全部目标：`dev` / `build` / `run` / `test` / `test-short` / `lint` / `check` / `migrate`。

`make migrate` 使用本机 `pg_isready` 检查 PostgreSQL，并将 `PGHOST` / `PGPORT` / `PGUSER` / `PGPASSWORD` / `PGDATABASE` 映射为 `GOWP_DATABASE_*` 后执行 `-migrate-only`。
服务是否在 air 重启时自动迁移由 `database.run_migrations` 控制：`true` 自动执行，`false` 需要手动执行迁移。

`scripts/` 下有两类脚本，用途不同：

- **当前入口**：`dev.sh`（`make dev` 调用）、`check-*.sh`（CI 门禁，`make check` 调用）。
- **历史脚本**：`build-all.*`、`build-frontend.*`、`deploy.sh` 是早期按 Windows/PowerShell 环境写的，
  其中的打包步骤已被 `go build ./...` 与 CI 覆盖。保留是为了不打断仍在使用它们的本地流程，
  但**新流程不要依赖它们**——直接编译就是全部步骤，没有前端打包工程需要编排。

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
- `docs/13-module-inventory.md`：模块实现清单（每个模块的完整职责、不变量与落地细节）。
- `docs/README.en.md`：英文贡献者文档入口（快速开始、架构、插件开发、贡献指南）。
- `AGENTS.md` 及目录规则：仓库开发约定。

## 许可与贡献

本仓库以 MIT 许可证发布，全文见 [LICENSE](LICENSE)。

发现安全漏洞请按 [SECURITY.md](SECURITY.md) 私下报告，**勿开公开 Issue**；报告范围与响应目标写在那份文件里。
改动纪律、评审期望与验证方式见 [docs/contributing.en.md](docs/contributing.en.md) 与 [AGENTS.md](AGENTS.md)。

开源发行仍待补的是可复现发行包与更完整的自动检查 —— 这些不由已有业务模块数量替代。
