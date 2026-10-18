# public 目录约定

- `migrations/`：版本化幂等 SQL + `register.go` 注册；seed 用 `ConditionSQL` 逐条枚举本批对象。**历史迁移 SQL 不改**，改列名时同步改新迁移、触发器函数体与 seed。
- `test/`：feature / 链路测试（真实 PG）；进 CI，不进生产构建。细则见 `public/test/CLAUDE.md`。
- `static/`：前端静态资源（HTMX 基座、`ui/json_write.js`、图片与样式）。属于前端层，不写业务计算。
- `site/`：发布产物输出目录（PublicationStore 的 local provider）；只读语义，不要手改。
- `runtime/` `logs/` `storage/` `backup/` `mockup/` `a/` `docs/`：运行期与资料目录，代码不得依赖其内容。