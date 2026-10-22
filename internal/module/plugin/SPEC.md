---
id: go-wp-mod-plugin
type: module-design
title: plugin — 插件注册与装配
status: active
parent: go-wp-modules
---

# plugin — 插件注册与装配

## 职责

插件注册表、安装 / 启停 / 卸载 / 巡检，以及把插件声明的组件与样式装配进构建器。

## 边界

- **不热加载 Go 代码**：插件只能提供构建器支持的组件、模板与样式声明，能力边界由装配面决定。
- 复用后台账号的鉴权上下文读权限（依赖 `admin` 契约），不自建权限模型。
