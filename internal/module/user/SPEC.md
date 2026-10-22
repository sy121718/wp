---
id: go-wp-mod-user
type: module-design
title: user — 访客与客户账号
status: active
parent: go-wp-modules
---

# user — 访客与客户账号

## 职责

访客/客户账号：注册、邮箱激活、改密与重置、资料与偏好、会话，以及访客访客下单的账号代建。

会话的唯一真源是 Redis（不落库、不复用后台账号的 cookie 命名空间）。

## 边界

- **依赖方向单向 user → mail**：发信走本模块声明的收窄接口 `MailSender`，`mail` 不认识 `user`。
- 访客会话不落库、不续期写入线上状态以外的东西。
- 后台对客户的查看/禁用是运营视图，账号主体仍归本模块。
