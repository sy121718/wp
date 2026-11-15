---
id: go-wp-mod-pipeline
type: module-design
title: pipeline — 发布适配层
status: active
parent: go-wp-arch
---

# pipeline — 发布适配层

## 职责

把本应用的编译入口、模板与页面守卫解析注入中立的发布内核：

- `DefaultCompile`：页面文档 → `internal/builder` 编译 → 发布 HTML；
- `PageGuardFunc` / `BuildGuardEntries`：页面文档 → 守卫文件与访问级别；
- `NewPublisher`：用上述两个端口装配 `pkg/publish.Publisher`。

发布器状态机（保存草稿 / 构建 / 发布 / 回滚 / 改 URL / 恢复产物 / 状态）、产物存储与发布存储、路径与语言 URL 规则的**单一真源**都在 `pkg/publish`，不在本包重新实现。

## 边界

- **不实现发布内核**：状态机、产物存储、发布存储与路径规则只经 `pkg/publish`；本包只提供应用侧端口。
- **不反向 import 业务模块**：依赖反查与重建策略由来源模块（页面 / 实例）注入。
- **不放行各自硬编码路径**：访问面与控制面的路径约定取 `pkg/publish`，各模块与模板不得各写一份。
- 发布器形状由内核固定：锁内取快照 → 锁外编译落盘 → 锁内二次校验提交；编译段不持锁。

## 依赖

`pkg/publish`（内核）、`pkg/builder`（通用编译）、`internal/builder`（业务组件与预设）、`internal/templates`（模板资产与渲染）、`internal/seo`（站点文件与 canonical 路径），以及 `project` / `navigation` / `page` / `plugin` / `media` 的契约。

`publication` → `pipeline` 是应用侧唯一反向依赖发布适配的边；内核侧保持中立，新增边要先确认不成环。
