---
id: go-wp-public
type: module-design
title: public — 迁移、测试与运行目录
status: active
parent: go-wp-arch
---

# public — 迁移、测试与运行目录

## 职责

- **`public/migrations/`**：版本化幂等 SQL 与 `register.go` 注册。结构迁移先跑，seed 后跑；seed 的生效门槛逐条枚举本批对象（禁用前缀 LIKE 与全库总量）。新 SQL 上线前先做 PREPARE 校验。
- **`public/test/`**：接口链路与集成测试（真实 PG），一个模块一个目录。表结构一律来自生产迁移，禁止手抄建表。不进生产构建。
- **`public/site/` 等运行目录**：发布产物的输出位置（本地 provider）。只读语义，不手改，代码不依赖其内容。

## 边界

- 历史迁移 SQL 不改；改列名要同时改新迁移、触发器函数体与 seed。
- 删能力时连 seed 的 SQL 与幂等条件一起收口。
- 冲突一律打回并列可定位数据，不自动加后缀、不静默合并。

## 与代码的偏差

`public/CLAUDE.md` 把 `public/static/` 描述为前端静态资源目录，但本仓**不存在该目录**：前端静态资产的真实源是 `internal/templates/static/`，经 `/static` 与构建期内联两个出口提供。以代码为准。
