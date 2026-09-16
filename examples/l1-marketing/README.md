# l1-marketing — 完整档（L1 数据层 + 预设 + 容器/主题查询）

轻轨插件能走到的天花板：**自带数据 schema**（L1）+ 区块预设 + 多组件 +
容器查询 / 主题档位。适合「营销活动」这类既要展示、又要存自己数据的插件。

## 安装

```bash
cd examples/l1-marketing
zip -r /tmp/campaignkit-1.0.0.zip manifest.json components assets migrations
# 后台：插件管理 → 上传 /tmp/campaignkit-1.0.0.zip → 启用
```

安装时会执行 `migrations/001_init.sql`：建 `plugin_campaignkit` schema 与 `campaigns` 表。
**卸载会 `DROP SCHEMA plugin_campaignkit CASCADE`** —— 插件自有数据不残留。

## 这个档位展示了什么

- **L1 迁移**：`manifest.migrations` + `schemaVersion`，SQL 文件按文件名字典序执行；
- **容器查询**：`queries.kind=size` 在容器宽度 ≥ 640px 时放大内边距（`@layer sky-auto`）；
- **主题档位**：`queries.kind=theme` 在主题密度 compact 时收紧内边距（`@layer sky-theme`）；
- **局部样式查询**：`queries.kind=local` 响应容器自定义开关（`@layer sky-local`）；
- **变量导出**：`accent` / `accentSoft` 两个控件 → `--ck-accent` / `--ck-accent-soft`，
  模板里同一个按钮的常态与悬停态共用它们；
- **结构伪类与专用桶**：`.ck-coupon__expires:first-child`、hover / hover-none / active。

## L1 的三条硬约束

1. 迁移 SQL **只能操作自己的 schema**：执行器锁定 `search_path`，并拒绝
   `DROP SCHEMA` / `CREATE ROLE` / `GRANT` / 访问 `public.` / 文件与系统目录；
2. `schemaVersion` 是整数：**同版本重装幂等跳过**，升版本按「DROP 旧 schema + 跑全量」重建；
3. 表建好了也**不能直接进可视化**：`collections.json`（L2 内容源）尚未实现，
   组件模板目前拿不到集合数据（见 `docs/06-E-plugin-authoring.md` §2 的「不能做什么」）。

## 排错

- 上传报 `迁移语句被安全策略拒绝（命中 ...）`：SQL 里出现了被禁的模式，改成只操作自己的 schema；
- 上传报 `迁移目录 "migrations" 下无 .sql 文件`：zip 里没带上 `migrations/` 目录；
- 组件渲染出「未声明的键」：页面/预设里的 props 键不在 manifest 声明内。
