---
id: go-wp-mod-architecture
type: module-design
title: architecture — 可执行的分层判据
status: active
parent: go-wp-arch
---

# architecture — 可执行的分层判据

## 职责

把 `AGENTS.md` 与各 `CLAUDE.md` 写下的边界变成会变红的测试：控制器形状、dto 形状、模块边界、取词归属、HTML 边界、模板逻辑、裸 SQL、JSON 入口、model 列型、文档指针等。

**判据先于实现**：新边界先落一条判据让现状变红，再逐条修到绿；台账类判据双向登记（新增与清理未同步都判失败）。

## 边界

- **不被生产代码 import**：这里只放测试与判据工具。
- **绿灯不等于没问题**：部分判据是启发式或台账式，结论前要回读语义。
- 判据必须能指向修复方向，而不是只报「违规」。
