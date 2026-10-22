---
id: go-wp-mod-sysconfig
type: module-design
title: sysconfig — 系统配置、字典与行政区
status: active
parent: go-wp-modules
---

# sysconfig — 系统配置、字典与行政区

## 职责

按分组的系统配置、字典选项、行政区数据，以及它们向其他模块暴露的只读面。

## 边界

- **写必须带版本**：整组替换、乐观锁，不合并、不重试、不静默胜出——冲突打回让调用方看见。
- 不做业务语义解析：字典值与行政区只是数据。
