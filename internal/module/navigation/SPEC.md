---
id: go-wp-mod-navigation
type: module-design
title: navigation — 站点导航
status: active
parent: go-wp-modules
---

# navigation — 站点导航

## 职责

公开站点的导航树（多 kind）：编辑、定位、渲染，以及条目来源解析与失效派发。

## 边界

- **与后台权限菜单严格隔离**：`navigation` 是站点对外导航，`admin` 的 `sys_menus` 是后台权限外显，两者不共用表也不互相读取。
- 不写页面产物：导航变化只标记依赖它的页面待重建。
