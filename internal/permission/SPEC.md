---
id: go-wp-mod-permission
type: module-design
title: permission — 权限点声明与登记
status: active
parent: go-wp-arch
---

# permission — 权限点声明与登记

## 职责

权限点常量的代码侧单一来源，路由注册处的声明校验（一条 API 路由一个独立权限点），以及启动期的幂等 upsert（只补缺失，绝不删除）。

这是一条横切能力，不是业务模块——它没有表的所有权之外的业务语义。

## 边界

- **常量字符串逐字不可改**：改名等于新建权限点，旧 code 上的角色与用户授权会失效。
- **一条 API 路由一个独立权限点**；页面路由可复用读接口的权限点。
- 登记只增不减；删除权限点是破坏性操作，必须显式处理引用。
