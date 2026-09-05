# CLAUDE.md（兼容入口）

> ⚠️ 本文件已被 **AGENTS.md** 取代。go_wp 的项目级开发约定全部见 [AGENTS.md](./AGENTS.md)，DSH 会话以 AGENTS.md 为准。
>
> 本文件仅保留给仍读取 `CLAUDE.md` 的旧工具链，内容与 AGENTS.md 保持一致；新增/修改约定请直接编辑 AGENTS.md，勿在此处维护双份。

## 项目概览（摘要）

go_wp 是 `CMS + Visual Website Builder + Static Publishing Engine`：控制面把 Page Document 与 CMS 内容编译为不可变静态 Artifact，访问面只读取已激活的 HTML/CSS/JS。Go + Jet 只在构建阶段运行，访客请求零查库零模板。

技术栈：Gin + Go + Jet v6 + HTMX + Casbin（自研 Adapter）+ PostgreSQL（主库）+ Redis（会话存储，Critical）。SQLite/SQL Server 驱动已移除，Vue SPA 已废弃。

完整约定：模块现状、认证三层链（Session/CSRF/Casbin）、model 层定位（Repository）、数据库、测试、Git 规范等，见 [AGENTS.md](./AGENTS.md)。
