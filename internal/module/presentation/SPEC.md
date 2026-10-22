---
id: go-wp-mod-presentation
type: module-design
title: presentation — 自动发布实例
status: active
parent: go-wp-modules
---

# presentation — 自动发布实例

## 职责

内容实体（商品 / 文章 / 分类）的自动发布页面实例。从 `contenttemplate` 派生完整 AST 快照（DocumentSnapshot），经与手工页面相同的编译与发布管线生成产物。

拥有：实例与快照、实例产物记录、实例依赖、逐语言发布账本、待重建标记、URL 设定、双轨（模板 / 文档）模式状态机。

## 边界

- **不可在构建器里手工编辑**：实例的结构来自内容模板，管理路径由 CMS 实体驱动。
- **URL 从实体推导**，不脱离 CMS 实体手工设置。
- **快照是自足的**：即使内容模板被删除，已有快照仍可编译为产物。
- **编译段禁止持锁**：实例锁只覆盖取快照与提交两段。
- **不自行拼 meta / 结构化数据**：统一走构建器的 SEO head 唯一注入点。
- 依赖反查与重建策略属于本模块（`MarkStaleByDependency` / `MarkStaleForI18n`），不由发布内核代劳。

## 依赖

`contract` → `contenttemplate`、`project`、`publication`、`page`、`media`、`product`、`navigation`、`plugin`、`block`、`internal/pipeline`、`internal/builder`、`internal/seo`。

实现 `media` 的 `StaleMarker` 端口（媒体换图后标记受影响实例）。

## 未登记越界

`service/presentation_instance.go` 直接 import `contenttemplate/model`，绕过「跨模块只经 contract」的判据，登记在 `internal/architecture/module_boundary_test.go` 的债务清单里。
