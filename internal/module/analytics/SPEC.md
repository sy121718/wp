---
id: go-wp-mod-analytics
type: module-design
title: analytics — 访问统计
status: active
parent: go-wp-modules
---

# analytics — 访问统计

## 职责

访问打点采集、归一化与匿名化（哈希盐），日粒度聚合与查询。

## 边界

- **`Collect` 是访问面唯一的写入口**：只写一条、恒返回 204、不做鉴权。这是「访客请求不查库」的唯一明文例外之一，改动它等于改架构不变量。
- 不在访客请求里做实时推算；访问面不在 Go 请求路径上。
