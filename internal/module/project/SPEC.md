---
id: go-wp-mod-project
type: module-design
title: project — 工程（站点）与主题
status: active
parent: go-wp-modules
---

# project — 工程（站点）与主题

## 职责

一个工程 = 一套站点：站点设置（GA4/GSC、head/body 脚本、语言 URL 模式）、主题（多套，一套激活）、站点语言清单。

站点语言清单是本仓「站点支持哪些语言」的唯一真源。

## 边界

- **不认识内容模板表**：需要模板选项或语言退役影响面时，经消费者侧端口（`StructureTemplateOptionsPort`、`LocaleRetirePort`）由调用方注入，本模块不 import 对方 service/model。
- **语言 URL 模式只读**：装配后不做进程级热更新。
- 工程作用域缺失时不做兜底查询。
