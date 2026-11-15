# public 目录约定

判据见 [AGENTS.md](../AGENTS.md)，本文件只写本目录的事实。

- `schema/`：**数据库结构的唯一来源**，一表一文件（`tables/<表>.sql`）+ `base.sql`（扩展、schema、函数）+ `order.txt`（系统数据装载顺序）。表文件按 `-- @phase pre|ddl|constr|idx|fk|comment|data` 分段；建表、约束、索引、外键、注释与系统数据改都在对应段里，不再追加逐版本迁移。`schema.Apply` 是唯一装载入口：空库一次事务建完并写 `sys_baseline` 水位，同摘要水位跳过，摘要不符拒绝重放；现有库接入用 `scripts/schema-adopt.sh`。系统数据只放词条、权限、菜单、字典与默认配置，不放业务数据。
- `test/`：feature / 链路测试（真实 PG）；进 CI，不进生产构建。细则见 [test/CLAUDE.md](test/CLAUDE.md)。
- `static/`：前端静态资源的**运行期出口**，经 `/static` 与构建期内联两个出口提供；**本仓不存在独立的 `public/static` 源目录**。资产源按属主分工：公共 UI（控件、基座、模板、图标）在 `pkg/view/ui/assets`，应用专用脚本在 `internal/templates/static/`；两边由同一个复合 FS 装配成一个出口。属前端层，不写业务计算。
- `site/`：发布产物输出目录（PublicationStore 的 local provider）；只读语义，不要手改。
- `runtime/` `logs/` `storage/` `backup/` `mockup/` `a/` `docs/`：运行期与资料目录，代码不得依赖其内容。
