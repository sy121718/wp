---
id: go-wp-mod-common
type: module-design
title: common — 无状态服务端点
status: active
parent: go-wp-modules
---

# common — 无状态服务端点

## 职责

不属于任何业务模块、也没有自己数据的小端点。目前只有验证码：签发与校验。

## 边界

- 只有 `inbound/http`：无 contract、无 service、无 model、无表。
- 直调 `pkg/captcha` 的能力包，不复制能力实现。
- 出现第二份状态或业务规则时，它就不再属于本模块，应当升级为独立模块。
