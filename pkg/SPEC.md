---
id: go-wp-pkg
type: module-design
title: pkg — 基础设施与契约包
status: active
parent: go-wp-arch
---

# pkg — 基础设施与契约包

## 职责

可被任意 `internal` 包 import 的横切能力与契约包：入口绑定、错误事实、JSON 出口、视图结果，以及数据库、缓存、会话、加密、邮件、队列、上传、校验、时区与站点协议等能力。

包清单与硬约束见 `pkg/CLAUDE.md`，这里只记形状。

## 边界

- **不 import `internal/**`**，不带业务语义。
- **唯一入口**：JSON 只经 `jsonx`、时间只经 `JSONTime`、日志只经结构化字段、入参只经 `binding`。
- **能力包不自建全局单例**：provider 形态，账号与密钥是数据。
- **改导出签名等于改全仓契约**：先查调用方再动；不建第二份同义别名。
- **准入**：三处以上复用、无业务语义、有单元测试。

## 能力包的分工

| 能力 | 边界要点 |
|---|---|
| `auth` / `casbin` | 会话是认证载体；Casbin 策略写有「同事务」变体，供业务把策略变更并入自己的事务 |
| `crypto` | 可逆加密，密钥由调用方传，不内置密钥 |
| `mailer` / `queue` / `upload` | provider 注册形态，无全局初始化 |
| `sitehttps` | 部署是否 HTTPS 的**单一声明点**，决定 cookie 的 Secure |
| `sitetz` | 站点时区的唯一解析口径 |
| `datarule` | 数据权限的 GORM 插件：注入行级条件与字段屏蔽；命中已注册域但取不到身份时 fail-closed |
| `rls` | 工程作用域的唯一设置点（事务内局部设置），以及对账与探针 |
