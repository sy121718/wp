# examples/ — 可安装的示例插件

四个示例插件，按表达力分档。每个目录都能直接打包上传安装，也可以作为写自己插件的起点。

| 目录 | 档位 | 展示什么 | 需要 L1 迁移 |
|---|---|---|---|
| `l0-demo/` | 最小样例 | 与 `go run ./cmd/plugin init <id>` 脚手架产物**逐字节一致**，用来验证「脚手架生成即可安装」 | 有（示例表） |
| `l0-style-only/` | 纯样式声明 | 全部样式来自 `manifest.json` 的 `styles.rules`：绑定、变体、伪类三态、结构伪类、变量导出、断点、容器/主题查询 | 无 |
| `l0-template-assets/` | 模板片段 + 静态资产 | Jet 模板条件输出、`assets/*.css` 消费导出的 CSS 变量、区块预设 | 无 |
| `l1-marketing/` | 完整档 | L1 迁移建自有 schema、容器查询三桶（size/theme/local）、多组件、presets | 有 |

## 安装任何一个

```bash
cd examples/l0-style-only           # 换成你要装的目录
zip -r /tmp/plugin.zip manifest.json components assets 2>/dev/null || zip -r /tmp/plugin.zip manifest.json components
# 后台 → 插件管理 → 上传 /tmp/plugin.zip → 启用
```

带迁移的档位（`l1-marketing`）记得把 `migrations` 一起打进包：

```bash
cd examples/l1-marketing
zip -r /tmp/campaignkit-1.0.0.zip manifest.json components assets migrations
```

安装成功后：组件出现在工作台组件库（类型 `plugin.<插件id>.<组件名>`），
带 `presets` 的档位会在区块库里多出预设，带迁移的档位在插件列表里能看到 `schemaVersion`。

## 失败时看什么

上传失败会返回明确的前缀错误（`ErrInstallParse` / `ErrUnsafePackage` / `ErrMigrationFailed`），
逐条对照表见 [docs/06-E-plugin-authoring.md](../docs/06-E-plugin-authoring.md) §2.3。

## 自动化验收

- `public/test/plugin/unit/example_parity_test.go` — `l0-demo` 与脚手架产物一致性；
- `public/test/plugin/unit/example_tiers_test.go` — 三档示例的真实安装（含 L1 迁移）与
  真实编译渲染（HTML + 分层 CSS），断言覆盖变量导出、触屏三态、容器/主题查询、模板条件输出、
  预设文档可编译。
