---
id: go-wp-mod-page
type: module-design
title: page — 手工页面实例
status: active
parent: go-wp-modules
---

# page — 手工页面实例

## 职责

一个 Page 绑定一个规范化公开 URL，独占该 URL，可独立构建、发布、回滚、取消发布。

拥有：页面文档（可编辑 AST）、修订快照、发布计划与语言范围、待重建标记、页面路由与重定向、站点槽位绑定、定时发布、构建意图与草稿版本号。

## 边界

- **不是 CMS 内容的访问时模板**：页面文档不保存文章正文、商品库存、菜单项等业务数据；内容经白名单绑定在构建期注入。
- **不自动派生内容页**：商品/文章的公开详情由 `presentation` 的自动发布实例负责。
- **不直接写发布面状态**：URL 占用与回执归 `publication`，产物记录归 `artifact`，入队归 `build`，三者都经契约；同事务场景只用对方提供的 `*Tx` 变体。
- 页面文档的语义校验、克隆与编译归 `internal/builder`；本模块只调不实现。
- `pages` 等表带 FORCE 策略：批量入口没有工程参数时，逐工程独立作用域执行后合并，读不到工程表就显式失败（不退化成不限工程）。

## 依赖

`contract` → `publication`、`artifact`、`build`、`blueprint`、`media`（均只 import 接口）、`internal/pipeline`、`internal/builder`、`internal/seo`。

反向：`navigation`、`content`、`product`、`presentation`、`admin`、`runtimefragment`、`workbench` 消费本模块契约（`SitePageResolver`、`PageDependencyLookup`、`PagePathKindLookup` 等只读面）。

## 关键契约

- `SitePageResolver.ResolveSitePages`：只返回已绑定且已发布的槽位，未发布不猜默认值。
- `PageDependencyLookup.FindPagesByDependency`：反查命中集合与「按依赖标记待重建」逐字一致（同一张表、同一对指针）。
- `BuildQueueEnqueuer.EnqueuePageBuild`：工程 id、语言、意图由调用方显式带出——队列不反查来源表。
