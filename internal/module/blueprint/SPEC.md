---
id: go-wp-mod-blueprint
type: module-design
title: blueprint — 页面初始化蓝图
status: active
parent: go-wp-modules
---

# blueprint — 页面初始化蓝图

## 职责

新页面文档的版本化初始化输入：草稿版本 + 不可变版本快照，`InitPageDocument` 递归复制 AST 并为每个节点换发新 id。

## 边界

- **用完即弃**：创建出的页面文档是独立快照，后续修改不传播，也不参与构建或访客请求。
- 不是构建期模板，不持有运行时数据。
- AST 的解析、校验、克隆归 `internal/builder`，本模块只调。
