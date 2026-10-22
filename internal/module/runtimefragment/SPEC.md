---
id: go-wp-mod-runtimefragment
type: module-design
title: runtimefragment — 受限动态片段
status: active
parent: go-wp-modules
---

# runtimefragment — 受限动态片段

## 职责

把「必须实时」的能力做成白名单片段端点：购物车、商品筛选、实时可用量、站内搜索、订单与退货查询、访客账号表单、评论、会员权益、捆绑配置。

能力以注册表形式声明：每个 capability 绑定固定协议与处理器，附带匿名/登录策略与短 TTL 缓存。

## 边界

- **不读页面文档、不执行 Jet、不解释绑定**：处理器只实现固定协议并返回 HTML 片段。
- **不持有 ORM、不 import 其他模块的 service/model**：全部能力经装配期一次注入的依赖（`SetDependencies`，只允许调用一次）。
- 装配缺失是**显式降级**而不是静默：缺依赖的片段返回「功能尚未接入」提示，缺可选哈希器时略过对应维度；装配自检负责在启动期拦住必需端口。
- 这是本仓唯一的非标准模块形态（无 contract / dto / model / service）：片段协议是它对外的全部契约。

## 依赖

`product`、`cart`、`order`、`comment`、`content`、`membership`、`page`、`presentation`、`project`、`sysconfig`、`user` 的契约，以及 `internal/builder` 的片段渲染面。

装配点在 `internal/routers/`；字段可缺/必填的判据只在装配层写一次。
