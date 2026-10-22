---
id: go-wp-mod-membership
type: module-design
title: membership — 会员等级与权益
status: active
parent: go-wp-modules
---

# membership — 会员等级与权益

## 职责

会员等级、等级权益、用户等级分配，以及按周期重算。

## 边界

- **`Resolve` 在访问面绝不写库**：缺默认等级时打回，不静默归到最小档。
- 等级与权益是数据，不是规则：谁能升到哪一级由消费方判断并调用分配。
- 只有 project 一个跨模块依赖。
