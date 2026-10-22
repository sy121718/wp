---
id: go-wp-mod-adminui
type: module-design
title: adminui — 后台渲染出口
status: active
parent: go-wp-arch
---

# adminui — 后台渲染出口

## 职责

后台页面的两个出口：注册 `PageSlot` / `BareSlot` 把模板名交给视图层，注入后台壳需要的键（语言、取词函数、权限集合、按钮码、导航树、csrf），并提供页面侧的错误形态、拒绝形态与跳转页。

## 边界

- **无业务语义、无数据派生**：只做「把渲染器和壳键交给视图」这一件事。
- **与 JSON 出口不可互换**：本出口经模板取词层，JSON 出口在 `pkg/response`；混用会把裸 key 弹给用户。
- JSON 处理器默认不 import 这里的错误形态助手。
