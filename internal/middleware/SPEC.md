---
id: go-wp-mod-middleware
type: module-design
title: middleware — 全局链与鉴权链
status: active
parent: go-wp-arch
---

# middleware — 全局链与鉴权链

## 职责

两条链：

- **全局链**：恢复、CORS、安全头、体限、限流、请求日志、压缩、静态与缓存。
- **鉴权链**：会话身份 → CSRF → Casbin 判定 → 数据规则上下文 → 页面权限 → 访问面守卫。

「当前操作人是谁」只有这一处入口；装配期把权限码集合与导航树写进请求上下文，渲染出口据此出菜单与按钮。

## 边界

- **刻意不 import 任何渲染代码**：页面被拒绝的形态由调用方注入回调，反向依赖会成环。
- **访问面守卫只读文件系统与签名 cookie**：不查库、不写、不续期，出错即 fail closed。
- **页面权限只判定并标记**，出口形态交给注入的实现。
- 数据规则引擎在 `pkg/datarule`，本层只提供上下文并触发注入。
