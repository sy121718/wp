---
id: go-wp-mod-block
type: module-design
title: block — 可复用结构片段
status: active
parent: go-wp-modules
---

# block — 可复用结构片段

## 职责

跨页面复用的结构片段（AST）与其引用类别词表：页面文档、页面结构、主题槽位、块文档、页面修订、内容模板、自动发布实例。

## 边界

- **引用检查 fail-closed**：查不出引用就不删。
- 块带 FORCE 策略：只给块 id 的入口必须逐工程探测归属再进事务。
- 不跨聚合写：删除前的影响面由引用方判断，本模块只报「在用」。
