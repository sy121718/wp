---
id: go-wp-mod-masterdata
type: module-design
title: masterdata — 主数据变更留痕
status: active
parent: go-wp-modules
---

# masterdata — 主数据变更留痕

## 职责

主数据（商品、变体、货源等）的字段级变更审计：改前改后快照、变更列表与实体时间线。

## 边界

- **不认识商品表与库存表**：调用方递进字段快照，本模块只记录与查询。
- 与库存流水严格分工：数量变动属 `inventory`，配置变更的 diff 属本模块。
- 只增不改。
