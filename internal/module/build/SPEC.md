---
id: go-wp-mod-build
type: module-design
title: build — 构建任务队列
status: active
parent: go-wp-modules
---

# build — 构建任务队列

## 职责

构建队列的状态机与租约：`pending / running / superseded / failed / succeeded`。跨实例用 `SKIP LOCKED` 认领，租约到期可回收。

## 边界

- **队列不反查来源表**：工程 id、语言、意图、草稿版本、输入哈希全部由调用方显式带出（来源可以是 page 或 presentation，队列不认识它们的表）。
- **执行器不改任务状态**：执行器只回答「这次成功没有」，状态流转归队列。
- **入队前即拒绝非法来源类型**，不留半截任务。
- 零跨模块 import：本模块只有 contract + 表。
