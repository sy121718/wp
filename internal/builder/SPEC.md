---
id: go-wp-mod-builder
type: module-design
title: builder — 编译内核
status: active
parent: go-wp-arch
---

# builder — 编译内核

## 职责

把页面文档 + 构建上下文 + 组件注册表编译成确定性的静态 HTML / CSS / JS。

包含：文档协议与校验、组件注册表、受限样式编译与校验、构建期数据源形状、SEO head 与结构化数据的唯一注入点、站点脚本注入、主题合并、检查器（Inspector）契约、UI 脚本与样式切分。

组件实现在 `internal/builder/components/<组件>/`，一组件一目录，模板与样式就近嵌入。

## 边界

- **只做构建期**：不做控制面数据库写，不处理访客请求。
- **组件不得扩展脚本 / API / 查询**：组件声明的能力边界就是它编译后的形态。
- **共享数据形状在 `builder/source`**，它零依赖；模块的契约可以依赖它，但契约不得反向 import `builder/core`（按传递闭包判，见 `scripts/check-contract-deps.sh`）。
- **SEO head 是唯一出口**：canonical / OG / 结构化数据只在这里拼，其他模块不得自行生成。
- 构建层不得依赖具体业务模块（方向是模块 → builder）。
- 确定性是硬要求：同一输入必须产生相同字节。

## 依赖

`content`、`product`、`media`、`order` 等模块的契约（作为构建期数据源），以及 `internal/seo`。反方向不存在边。
