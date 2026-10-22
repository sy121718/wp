---
id: go-wp-mod-artifact
type: module-design
title: artifact — 不可变产物记录
status: active
parent: go-wp-modules
---

# artifact — 不可变产物记录

## 职责

产物行（`(页面或实例, 版本, 语言)`）与内容寻址对象闭包：哪些产物引用了哪些内容对象，对象的 `available / gc_pending / deleted` 生命周期，以及 GC 候选与译文缺失探查。

## 边界

- **不判断业务引用**：产物是否仍被线上占用由 `publication` 与 `internal/pipeline` 判断，本模块只回答问题。
- **GC 默认 dry run** 且保留保留下限；仍被现存产物行引用的对象不得删除。
- 零跨模块 import：外部引用经注入的 `ExternalContentRefs` 进入，不反向认识业务表。
- 版本号靠 `MAX+1` 派生，调用方需持锁，唯一性由 `(主体, 版本, 语言)` 兜底。
