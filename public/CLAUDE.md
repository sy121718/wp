# public 目录约定

判据见 [AGENTS.md](../AGENTS.md)，本文件只写本目录的事实。

- `migrations/`：版本化幂等 SQL + `register.go` 注册；seed 用 `ConditionSQL` 逐条枚举本批对象（禁用前缀 LIKE 与全库总量）；结构迁移先跑、seed 后跑；新 SQL 上线前先做 PREPARE 校验。**历史迁移 SQL 不改**，改列名时同步改新迁移、触发器函数体与 seed；删能力时连 seed 的 SQL 与幂等条件一起收口。数据冲突一律打回并列可定位数据，不自动加后缀、不静默合并。
- `schema/CURRENT.sql`：**当前**数据库结构的快照（`scripts/schema-snapshot.sh` 生成，勿手改）。迁移是变更历史（唯一真相、只追加），快照是当前形态（可读、可 diff）。迁移改了结构就要重生成快照，CI 的 `--check` 会拦。
- `test/`：feature / 链路测试（真实 PG）；进 CI，不进生产构建。细则见 [test/CLAUDE.md](test/CLAUDE.md)。
- `static/`：前端静态资源的**运行期出口**，经 `/static` 与构建期内联两个出口提供；**本仓不存在独立的 `public/static` 源目录**。资产源按属主分工：公共 UI（控件、基座、模板、图标）在 `pkg/view/ui/assets`，应用专用脚本在 `internal/templates/static/`；两边由同一个复合 FS 装配成一个出口。属前端层，不写业务计算。
- `site/`：发布产物输出目录（PublicationStore 的 local provider）；只读语义，不要手改。
- `runtime/` `logs/` `storage/` `backup/` `mockup/` `a/` `docs/`：运行期与资料目录，代码不得依赖其内容。
