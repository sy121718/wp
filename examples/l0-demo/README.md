# L0demo — L0 最小示例插件（OSS-016）

与 `go run ./cmd/plugin init l0demo` 生成物**逐字节一致**的示例插件
（一致性由 public/test/plugin/unit 的 parity 测试钉住）。这是插件体系
L0（展示层）+ L1 迁移的最小完整样例：上传安装即通过 manifest 校验、
迁移建表，组件 `plugin.l0demo.l0demo_card` 进入工作台组件库。

## 目录

- `manifest.json`   组件（含 props 白名单控件与 styles 样式声明）+ 区块预设 + 迁移声明
- `components/`     Jet 模板（`{{ .V.title }}` 走检查器 props，输出默认转义）
- `migrations/`     001_init.sql（CREATE SCHEMA plugin_l0demo + 建表，安装时按文件名序执行）
- `assets/`         静态资产说明（css/js/图片放这里，打包进产物）

## 验证链路

zip 打包本目录 → 后台「插件管理」上传 → 启用 →
`EnabledAssembly` 含 `plugin.l0demo.l0demo_card` 规格与 `l0demo-hero` 预设。
同链路的自动化验收见 `public/test/plugin/unit/scaffold_e2e_test.go`。

## 能力缺口（不要在本示例上伪造）

- **L2 内容源未包含**：manifest 的 `collections` 字段与 CollectionSource
  注册链路尚未实现（依赖 OSS-001/002/003），示例没有也不应有 collections.json；
- **L1 后台管理页未包含**：manifest 的 `admin` 菜单注册未实现，
  插件自建后台页面暂不可用；
- 插件只能用 `plugincomp` props 控件白名单
  （text / textarea / number / select / color / media / unit），
  不能新增检查器控件类型。
