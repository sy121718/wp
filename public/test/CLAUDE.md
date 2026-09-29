# public/test 测试规范

集中管理**需要数据库 / 事务 / HTTP / 完整装配路径**的测试代码与测试资源。

> 纯逻辑测试**不在这里** —— 模块内就近单测（`internal/module/**/*_test.go`）是主流做法，
> 判定标准与理由见 [`docs/rules/testing.md`](../../docs/rules/testing.md)。

## 目录组织

```text
public/test/
├── <模块>/feature/      # 接口链路测试（主）：路由 → 中间件 → 绑定 → handler → service → model
├── <模块>/unit/         # 模块规则单测（辅）：复杂规则、边界、异常分支
├── feature/             # 跨模块顶层链路（api_auth_test.go、admin_pages_casbin_guard_test.go…）
├── architecture/        # 架构红线的静态扫描（边界 / 事务 / model 标签）
├── rls/ partition/ security/ seo/ pipeline/ pkg/   # 横切关注点按主题分目录
├── support/             # 公共测试支撑（见下）
└── fixtures/            # 静态测试数据（json/sql/yaml）
```

**目录名用模块名或领域名，不额外分层**：`public/test/order/feature/`，不是 `public/test/backend/order/...`。
`feature` / `unit` 是固定的二级类型目录（横切目录不适用）。

## 两类测试

**feature（接口链路测试）** —— 从 HTTP 接口入口触发，覆盖路由、中间件、参数绑定、handler、service、model
的完整链路；验证「业务场景是否可用」（登录成功 / 失败、权限不足……）。

**unit（模块规则单测）** —— 测试单个函数或局部规则逻辑，覆盖复杂规则、边界条件、异常分支。
不是临时代码，属长期维护的回归资产。

## 命名

- 文件：`<模块>_<场景>_test.go`（`admin_login_test.go`）—— 一个文件可含同一场景下多个测试点
- 函数：`Test<模块><场景><结果>`（`TestAdminLoginSuccess`、`TestOrderCreateStockShortage`）
- 每个核心场景一个顶层 `TestXxx`，场景内用 `t.Run(...)` 切分测试点

## 用例覆盖面

每个核心场景优先覆盖：正常与边界输入（最大值 / 最小值 / 空字符串 / 空数组）· 参数校验错误
（必传为空 / 类型不合法 / 格式不合法）· 鉴权与权限不足（未登录 / 无角色权限 / 无数据权限 / token 失效）·
资源不存在（可区分「未找到」与「系统异常」）· 状态冲突与并发（版本冲突 / 非法状态流转）·
异常与恶意输入（超长字符串、特殊字符、注入字符、超大报文）· 后端依赖与业务异常
（依赖超时、约束冲突、锁等待、库存不足）。

## 公共支撑

- `support/test_bootstrap.go` —— 只做测试环境初始化与清理（配置、依赖装配、测试路由），**不启动 main**
- `support/test_client.go` —— 统一封装请求构造、发送、响应解析
- `support/migrated_db.go` —— `NewMigratedPGTestDB` / `SeedProjectRow`（复制生产结构模板库）
- `support/pgtest.go` —— `NewPGTestDB`（空库，给自建表 / 故意构造旧 schema 的用例）
- `fixtures/` —— **只放静态测试数据**，不放业务逻辑和流程控制代码

helper 的分工与「为什么表结构必须来自生产迁移」见 [`docs/rules/testing.md`](../../docs/rules/testing.md)。

## 执行

```bash
go test ./public/test/...                              # 全部
go test ./public/test/order/feature -v                 # 某模块 feature
go test ./public/test/admin/unit -v                    # 某模块 unit
go test ./public/test/order/feature -run TestOrderCreate -v   # 按函数名筛选
make test                                              # 全量（按 CPU 核数并发）
```

## 边界约束

- 测试代码统一放在 `public/test`，不要求分散到业务代码目录
- **不启动生产服务进程**进行测试，使用测试进程内构造的路由与依赖
- 测试配置与开发配置隔离，避免误操作真实数据
- 表结构一律来自生产迁移，**禁止手抄 `CREATE TABLE` 伪造「看起来像生产」的表**
