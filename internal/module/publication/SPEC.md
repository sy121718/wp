---
id: go-wp-mod-publication
type: module-design
title: publication — URL 占用与发布回执
status: active
parent: go-wp-modules
---

# publication — URL 占用与发布回执

## 职责

访问面的 URL 账本：路径从预留到激活的占用状态机、重定向、发布回执（可幂等重试与故障恢复），以及站点级文件（sitemap / robots / feed / 404）。

## 边界

- **不构建、不修改产物**：产物归 `artifact` 与 `internal/pipeline`。
- **不自行解析语言 URL 方案**：语言规则只经 `internal/pipeline` 的单一映射点取。
- `*Tx` 变体方法**不得新开事务**，必须在外层事务上执行（页面发布时由 `page` 的事务穿入）。
- 已激活路径不得直接改名，必须走「取消激活 → 改名」。
- URL 只存 path，不存 scheme/host/query；系统路径（`/admin/`、`/api/`、`/_fragments/` 等）不可被页面占用；文件系统键由安全编码器从 id/hash 生成，不拼用户 URL。
