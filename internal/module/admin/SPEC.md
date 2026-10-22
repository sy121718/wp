---
id: go-wp-mod-admin
type: module-design
title: admin — 后台账号与权限治理
status: active
parent: go-wp-modules
---

# admin — 后台账号与权限治理

## 职责

后台账号、角色、权限点、菜单、部门、数据规则的 CRUD，以及登录（密码哈希 + 验证码）与「有效权限投影」：权限码集合、授权菜单树、按钮码。

数据模型的分工：权限点是唯一最小单元，菜单是它的可视化勾选载体，角色是权限组，Casbin 是规则验证器。

## 边界

- **与 `user` 是两套账号体系**：这里是后台账号（后台会话 cookie、走 Casbin 权限点）；访客/客户账号归 `user`（另一套 Redis 会话命名空间）。对客户的后台运营视图由 `user` 提供契约，账号主体不迁过来。
- 权限点常量在 `internal/permission` 是代码侧单一来源，**逐字不可改**（改名等于新建权限点，旧授权失效）；启动期幂等 upsert，不删除既有行。
- 数据规则引擎在 `pkg/datarule`，本模块只管理规则与分配，不实现注入。
