---
id: go-wp-mod-routers
type: module-design
title: routers — 唯一装配层
status: active
parent: go-wp-arch
---

# routers — 唯一装配层

## 职责

把模块、中间件、前端模板与渲染出口装配成一个进程：路由注册、依赖接线、页面出口挂载、装配期自检、站点访问面挂载。

装配是**线性且顺序敏感**的：基础层 → 核心 CRUD → 身份与交易 → 商品库存端口 → 运行时访问面 → 发布模块 → 发布端口 → 内容模板影响面 → 常驻任务 → 后台页面 → 自检 → 站点访问面。顺序错了表现为「端口注入到空实现上」，编译不报错。

## 边界

- **只装配，不含业务逻辑与 SQL**。
- **页面出口只在这里声明**：`adminui.PageSlot` / `BareSlot` 决定模板名，处理器只交出数据对象。
- 端口台账是本模块的资产：每个未注入就降级的能力必须登记「未注入时用户看到什么」；必需端口在自检阶段失败即拒绝启动。
- 装配顺序与路由集合由快照测试守护，改动会产生可审阅的 diff。

## 依赖

全部模块的 `inbound` 与 `contract`、`internal/adminui`、`internal/middleware`、`internal/templates`、`internal/pipeline`。
