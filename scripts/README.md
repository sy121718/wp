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
