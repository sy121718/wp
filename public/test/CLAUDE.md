# public/test 约定

## 结构
- 一个模块一个目录：`<mod>/feature/*_test.go`；跨模块链路放 `feature/`；基建放 `support/`。
- 用例名说清「谁在什么条件下应当怎样」；一个函数一个断言主题。

## 表结构来源
- 需要真实 schema：`support.NewMigratedPGTestDB(t)`（复制模板库，含生产迁移）。
- 空库自建表 / 故意构造旧 schema：`support.NewPGTestDB(t)`。
- 禁止手抄 `CREATE TABLE` 伪造生产表；`AutoMigrate` 不等于简化版建表（列型会分叉）。

## 断言判据
- 断言**等式**而不是各自的值（两侧之和 == 总量；两种粒度展开后相等）。
- 错误响应：断言状态码 + 对外文案；断言**不含**内部原文（SQLSTATE / 表名 / 列名）。
- 页面渲染：断言真实 HTML 片段（含闭合标签），不要只断言状态 200。
- 计数类断言要写清口径（哪批行、哪个时间窗）。

## 执行
- 全量并发：`make test`（`-p $(nproc)`）；并发敏感的包加 `-race`。
- PG/Redis 不可用时 `t.Skip`；不要用 mock 掩盖缺失的依赖。