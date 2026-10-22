---
id: go-wp-mod-siteservices
type: module-design
title: 站点级横切服务
status: active
parent: go-wp-arch
---

# 站点级横切服务

几个体量小、无业务表但被多处引用的横切包，共同点是「只回答一个问题」。

| 包 | 回答的问题 | 边界 |
|---|---|---|
| `internal/siteurl` | 内容实体的详情 URL 长什么样（模式、规范化、校验） | 只做表单预填与派生；发布时显式路径优先 |
| `internal/backref` | 失败后回退到哪个后台路径 | 构造与判据在 `pkg/backpath` / `pkg/safepath`，这里只组 |
| `internal/retention` | 各类历史数据保留多久、按什么列清 | 纯声明，不持有表访问权；清理由各模块给执行器 |
| `internal/partition` | 分区表要不要提前建、什么时候 detach | 只做分区与 RLS 对账，不含业务判断 |
| `internal/uispec` | AI 展示指令的封闭白名单与校验 | 指令不带数据；数字一律服务端查 |
| `internal/mcp` | 工具注册表、参数校验、逐请求鉴权、写操作确认与幂等 | 鉴权与页面按钮同源；write 必须带确认与幂等键 |
| `internal/shell` | 迁移期兼容层 | **终局是删除**；新代码走 `pkg/backpath` / `pkg/bulkids` / `adminui`，新增导出符号必须登记终局归属 |
