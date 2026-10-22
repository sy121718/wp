---
id: go-wp-mod-mail
type: module-design
title: mail — 邮件营销与事务邮件
status: active
parent: go-wp-modules
---

# mail — 邮件营销与事务邮件

## 职责

邮件账号（SMTP，凭据加密）、模板、联系人、抑制名单、群发活动与事件、自动化流程与运行日志、发送日志，以及跟踪令牌签名、退订与发送限流。

## 边界

- **不 import 任何模块契约，也不认识用户表**：收件人与事务邮件内容由调用方给出（`TransactionalSender` 是本模块对外的收窄接口）。
- 邮件正文渲染用构建器的模板能力，但邮件不是站点产物，不进发布管线。
