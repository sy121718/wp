# RLS 角色切换与回滚（DB-009 / DB-04）

> 适用范围：把应用的业务连接从管理角色（超级用户）切到非超级的次级管理员角色
> （默认 `go_wp_app`），让迁移 199 / 215 铺下的工程隔离策略真正生效。
> 配套工具：`scripts/rls-role-setup.sh`（建角色 + 授权 + 现场验证）、
> `pkg/rls`（作用域原语 + 连接身份探针）、`database.require_rls_role`（启动门禁）。

## 1. 为什么必须切角色

迁移 215 给 53 个带 `project_id` 的对象装了 `ROW LEVEL SECURITY` + `FORCE`，
策略谓词读会话变量 `app.project_id`（未设置即行不可见，fail closed）。但
**PostgreSQL 的超级用户总是绕过 RLS** —— `FORCE` 约束的是表属主，约束不了
superuser / `BYPASSRLS` 角色。应用原先连的是 `root`（`rolsuper=t`），
所以策略一行都挡不住。

实测（本地库，`project_locales` 有 3 行、分属 3 个工程）：

- `root` 未设 `app.project_id` → 读出 3 行（策略完全无效）
- `go_wp_app` 未设 `app.project_id` → 读出 0 行（fail closed）
- `go_wp_app` 事务内设 `app.project_id=P1` → 本工程 1 行；同作用域下查 P2 → 0 行

## 2. 两角色分工

| 用途 | 角色 | 需要的能力 |
|---|---|---|
| 业务连接（`database.user`） | `go_wp_app`（NOSUPERUSER NOBYPASSRLS） | `USAGE` on schema、DML、序列 `USAGE/SELECT` |
| 迁移 / 运维（`-migrate-only`、脚本） | 管理连接（超级用户） | DDL、`BYPASSRLS`（迁移要建表、seed 要写内置行） |

两角色分工是**结论不是临时妥协**：`FORCE` 让属主也受 `WITH CHECK` 约束，
seed 与 DDL 若跑在业务连接上会被策略挡住或需要在每个迁移里手工设作用域。
角色**不由迁移创建**（migration ledger 里没有角色这一概念，运维动作走脚本）。

## 3. 顺序不变量（反了会静默丢数据）

**先给各模块的读写路径包上 `pkg/rls.InProjectScope`，再切 `database.user`。**

反过来时，没包 scope 的路径会**返回 0 行而不报错**（fail closed 是策略的设计目标），
表现为「功能突然查不到数据」且日志里没有任何错误 —— 这是本项目对 DB-009 定下的硬顺序，
`public/test/rls/rls_scope_test.go` 专门钉住它。

## 4. 切换步骤

### 4.1 建角色并授权（幂等，可重复执行）

```bash
# 口令不进命令行：优先环境变量，其次 ~/.config/go_wp/rls-role.env（权限 600）
export GOWP_RLS_ROLE_PASSWORD="$(openssl rand -hex 16)"
bash scripts/rls-role-setup.sh go_wp_app
```

脚本做四件事：建 / 改角色（显式 NOSUPERUSER NOBYPASSRLS）、授权（含
`ALTER DEFAULT PRIVILEGES`，将来新建的表自动授权）、核对角色属性、
再用该角色连库验证「未设变量 0 行 / 设了变量只见本工程 / 别的工程不可见」。
含口令的 SQL 只经 stdin 交给 psql，因此不会出现在 `ps` 的 argv 里。

### 4.2 盘点并补齐工程作用域

这一步属于 DB-05（各模块 model 接线），本文件只给可重复执行的检索命令（见 §7）。
判断标准不是「包里有 import」，而是**每一条访问带 `project_id` 表的路径**都在
`rls.InProjectScope` / `rls.ScopeTx` 里执行。

### 4.3 换业务连接（不改管理连接）

```yaml
database:
  user: go_wp_app
  password: ""            # 走环境变量，别把明文写回 config.yaml
```

```bash
# 启动前注入（口令仍然不落盘）
export GOWP_DATABASE_PASSWORD="$(grep -E "^GOWP_RLS_ROLE_PASSWORD=" ~/.config/go_wp/rls-role.env | cut -d= -f2-)"
```

### 4.4 打开启动门禁

```yaml
database:
  require_rls_role: true
```

启动期探针始终以 INFO 打印 `session_user` / `current_user` /
`pg_roles.rolsuper` / `rolbypassrls` / 策略覆盖的表数；
`require_rls_role=true` 时，连接角色仍会绕过 RLS 就**直接拒绝启动**（返回 error）。
默认 `false` 只打 WARN —— 迁移与运维复用同一个 `database` 组件、走管理连接，
默认 fail fast 会把正常运维挡在门外。**换完业务连接必须置 true**，这道门禁才算闭环。

### 4.5 验收

1. 启动日志里出现 `连接角色 go_wp_app 不绕过 RLS：N 个对象上的工程隔离策略生效`；
2. 后台逐模块抽样：本工程数据读得到、另一工程的数据读不到（列表 / 详情 / 导出都要看）；
3. 跨工程写入被拒（策略的 `WITH CHECK`）；
4. 后台任务与调度器（构建、发布、清理、投递）逐条跑一遍 —— 它们最容易漏作用域。
5. `go test -count=1 ./public/test/rls/...`（非超级角色下才有意义）。

## 5. 回滚步骤

1. `config.yaml` 的 `database.user` 改回管理角色（root），
   `require_rls_role` 改回 `false`，重启应用 —— 这一步立刻恢复原行为，
   因为超级用户绕过全部策略。
2. （可选）删角色：`DROP OWNED BY go_wp_app; DROP ROLE go_wp_app;`（脚本只授权不建依赖，
   删角色不需要动任何业务对象）。
3. 若已在 4.4 置 true 却忘了改 user，应用会启动失败 —— 这是**设计如此**：
   比「以为有隔离、实际没有」安全，错误信息里带角色名与命令，照着做即可。

## 6. 当前缺口（本分支 `93bcd17a` 实测，DB-05 收敛）

结论（按风险从高到低）：

1. **`build_jobs` 不在策略覆盖里**：它是全库唯一「有 `project_id` 但没有
   `ROW LEVEL SECURITY`」的表（`project_id` 是迁移 295 新加的）。
   `internal/module/build/model` 也没有接 `pkg/rls` —— 切角色后它**不受影响**
   （既不会被拦，也不会有隔离），这正是最容易被漏判的一类：查「哪些表没策略」才能发现。
2. **跨工程的后台清理**：`internal/retention` 是声明目录，真正执行在各模块的
   Sweep（按时间列批量删）。这类语句天然跨工程，**不能**简单包 `InProjectScope`：
   要么按工程逐个展开后带作用域执行，要么明确走管理连接。`page/service/page_retention.go`
   已经是「逐工程展开」的样板；其余按时间删 scoped 表的路径要逐个确认。
3. **构建 / 发布期读取**：`internal/pipeline` 与 `internal/builder/core` 自身不 import
   `pkg/rls`（grep 已确认），它们的工程隔离完全依赖被调用的 model / contract 是否带了作用域。
   切角色前应逐条走一遍构建与发布。
4. 已接线的 11 个模块（analytics / block / contenttemplate / masterdata / navigation / order /
   page / presentation / product(含 inventory) / project / publication）里，
   **个别方法**仍可能绕过包装（例如直接 `DB(ctx)` 拼查询）——
   静态扫描只能给出候选，最终按 §7 的命令逐条看。

## 7. 复核命令

```bash
# (a) 带 project_id 但没有 RLS 的表（当前只应剩 build_jobs）
psql -h 127.0.0.1 -U root -d wp -tAc "SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p') AND NOT c.relrowsecurity AND EXISTS (SELECT 1 FROM information_schema.columns col WHERE col.table_schema='public' AND col.table_name=c.relname AND col.column_name='project_id') ORDER BY 1"

# (b) 已接线的包 / 模块
grep -rl "go_wp/pkg/rls" --include=*.go internal/ | xargs -n1 dirname | sort | uniq -c | sort -rn

# (c) model / service 里以 gorm 访问 scoped 表、但所在包没 import pkg/rls（候选缺口）
grep -rln "ProjectID" --include=*.go internal/module/*/model internal/module/*/*/model | xargs -n1 dirname | sort -u | while read -r d; do grep -rq "go_wp/pkg/rls" "$d" || echo "候选: $d"; done

# (d) 当前连接身份（结论与启动探针一致）
psql -h 127.0.0.1 -U root -d wp -tAc "SELECT session_user, current_user, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user"

# (e) 策略覆盖对象数
psql -h 127.0.0.1 -U root -d wp -tAc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p') AND c.relrowsecurity"
```
