---
id: go-wp-mod-pipeline
type: module-design
title: pipeline — 发布内核
status: active
parent: go-wp-arch
---

# pipeline — 发布内核

## 职责

「编译 → 产物 → 激活」的内核：发布器状态机（保存草稿 / 构建 / 发布 / 回滚 / 改 URL / 恢复产物 / 状态），产物存储与发布存储两个抽象，路径约定与语言 URL 规则的**单一真源**，守卫产物、404、发布计划冻结语言、依赖重建端口。

## 边界

- **内核不认识业务模块**：只经契约取数据；依赖反查与重建策略由来源模块（页面 / 实例）实现后注入。
- **不反向 import 编译与产物实现的细节**，也不承担模块级业务规则。
- **不放行各自硬编码路径**：访问面与控制面的路径约定只能从这里取（`DefaultArtifactRoot`、`ActiveRoot`、语言规则），各模块与模板不得各写一份。
- 发布器形状固定：锁内取快照 → 锁外编译落盘 → 锁内二次校验提交；编译段不持锁。

## 依赖

`project`、`navigation`、`page`、`plugin`、`media` 的契约，`internal/builder`（编译入口）与 `internal/seo`（站点文件与 canonical 路径）。

本模块是唯一「模块反向依赖共享内核」的边（`publication` → `pipeline`）；当前无环，但内核侧已握有多个模块契约，新增边要先确认不成环。
