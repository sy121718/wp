---
id: go-wp-mod-workbench
type: module-design
title: workbench — 可视化工作台
status: active
parent: go-wp-modules
---

# workbench — 可视化工作台

## 职责

可视化建站工作台的编排与计算：页面文档编辑的入口动作、预览、组件与主题选项、复用插入、自动化动作。

**没有表、没有 contract、没有 model**：它把其他模块的能力编排成工作台需要的数据对象。

## 边界

- 跨模块只经契约；缺装配时降级返回空，不猜数据。
- service 不认识 gin、不写响应、不持数据库句柄。
- 编译语义不在本模块：预览复用页面契约的编译能力，错误类型由契约的哨兵标识，状态码由本模块的 enums 下发。
