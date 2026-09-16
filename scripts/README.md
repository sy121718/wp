# scripts 目录说明

本目录保留构建、部署和辅助脚本。

## 当前状态

现有前端打包脚本来自基础框架迁移，但当前仓库不包含它们依赖的 `web/` 前端源码，也没有脚本引用的 GoReleaser 配置。因此以下脚本暂不属于可用发布链路：

- `build-all.sh` / `build-all.ps1`
- `build-frontend.sh` / `build-frontend.ps1`
- `prepare-embed.sh` / `prepare-embed.ps1`
- `deploy.sh`

在前端源码位置、嵌入策略和发布产物格式确定前，不应使用这些脚本生成正式发布包。

当前检查入口：

```bash
go test ./...
go build ./...
go vet ./...
bash scripts/check-workbench.sh
bash scripts/check-permission-gaps.sh   # 需要本地数据库
```

`check-permission-gaps.sh` 审计「有路由、但没有对应权限点」的接口：把运行时装配出来的路由表（`WP_DUMP_ROUTES=1 go test ./internal/routers/`）与 `sys_permission` 比对。这类接口在 `authorizedAPI` 组下会让**任何账号（含超管）**都被拒 403 —— 新增接口忘了 seed 权限点时，它是最便宜的发现手段。有意豁免的接口（登录 / 验证码 / 当前用户资料）列在脚本顶部。

`check-workbench.sh` 要求开发环境有 Node：检查 Go 生成契约是否漂移、工作台 ESM 能否解析及加载、公共控件资产和实际面板编辑链路。Node 不参与生产资产构建。修改对齐重复项声明后，在仓库根目录运行 `go run ./cmd/workbench-contracts` 更新生成文件。

ArtifactStore、PublicationStore 和静态交付链路已经实现；失效的旧脚本仍不代表当前发行流程。生产构建用 `go build -o app ./cmd`，后台模板和静态文件须一并交付。发行包设计见 `docs/11-foundation-and-open-source.md`。
## 巡检与守卫脚本

CI 里跑的四个门禁（改了权限点 / service 层数据访问 / 后台文案 / 多端 CSS 之后，本地先过一遍再推）：

| 脚本 | 拦什么 | 依赖 |
|---|---|---|
| `check-permission-gaps.sh` | 有路由但没有对应权限点（该组挂 CasbinMiddleware 按实际路径 enforce，缺权限点时**含超管全员 403**） | 本地数据库 |
| `check-service-db-boundary.sh` | service 层直查数据库、跨模块引用 model/service | 无 |
| `check-i18n-coverage.sh` | 后台模板里的硬编码中文超出基线（基线数值在 `i18n-coverage-baseline.txt`） | 无 |
| `check-multidevice-css.sh` | 组件产出里的裸 `:hover`、写死宽度等违反多端硬规则的写法（`SKY_CSS_GUARD=error` 时是硬门禁） | 无 |

运维脚本：

- `rls-role-setup.sh <角色> <密码>` —— 建非超级角色并授权（含 `ALTER DEFAULT PRIVILEGES`，让将来新建的表也自动授权）。这是 DB-009 把 `config.yaml` 的 `database.user` 换成非超级用户之前的准备步骤；**顺序不能反**，先换角色会让未包工程作用域的路径静默返回 0 行。
- `index-usage-report.sh` —— 索引使用量快照与增量对比（审计 IDX-018）。无参数打印当前快照；`--save <名字>` 建基线、`--diff <名字>` 列出零增量候选、`--list` 列已有快照。**单次快照没有区分度**：空表上连唯一约束索引都是 0 次扫描（本地实测 376 个索引里 206 个零增量），真正有判据的是「生产库上间隔一个完整业务周期的两次快照之差」。增量为零只意味着进入待复核清单，删除前必须逐个确认：没有唯一 / 主键约束语义、没有 EXPLAIN 断言测试依赖它、且有替代索引覆盖同一查询面。快照落在 `public/logs/index-usage/`（已在 `.gitignore` 内）。
