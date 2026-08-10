# scripts 目录说明

本目录保留构建、部署和辅助脚本。

## 当前状态

现有前端打包脚本来自基础框架迁移，但当前仓库不包含它们依赖的 `web/` 前端源码，也没有脚本引用的 GoReleaser 配置。因此以下脚本暂不属于可用发布链路：

- `build-all.sh` / `build-all.ps1`
- `build-frontend.sh` / `build-frontend.ps1`
- `prepare-embed.sh` / `prepare-embed.ps1`
- `deploy.sh`

在前端源码位置、嵌入策略和发布产物格式确定前，不应使用这些脚本生成正式发布包。

当前可验证的后端命令：

```bash
go test ./...
go build ./...
go vet ./...
```

后续实现 ArtifactStore、PublicationStore 和静态交付链路时，应重新定义发布脚本职责，而不是直接沿用当前失效的前后端打包流程。