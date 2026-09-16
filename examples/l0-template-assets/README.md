# l0-template-assets — 模板片段 + 静态资产档

比 `l0-style-only` 多两样东西：**Jet 模板里有结构**（条件输出、子元素类名），
以及 **`assets/*.css` 静态资产**。这一档适合「组件有自己的 DOM 骨架，
样式声明不够用，需要几行手写 CSS」的插件作者。

## 安装

```bash
cd examples/l0-template-assets
zip -r /tmp/notecards-1.0.0.zip manifest.json components assets
# 后台：插件管理 → 上传 /tmp/notecards-1.0.0.zip → 启用
```

安装后组件库出现两个组件（`plugin.notecards.notecards_card`、
`plugin.notecards.notecards_badge`），区块预设多出「三卡特性区」。

## 这个档位展示了什么

- **模板片段**：`notecards_card.jet` 用 `{{ if }}` 做条件输出（副标题 / 价格为空时整块不渲染），
  子元素类名 `.nc-card__title` 与 styles 里的 `target` 一一对应；
- **静态资产**：`assets/card.css` 按文件名序拼接、注入产物主 CSS 之后，
  可以写 `::first-letter` 这类声明引擎不表达的细节；
- **变量桥**：`accent` 控件 → `--nc-accent`，`assets/card.css` 里的
  `var(--nc-accent, #2563eb)` 直接消费它 —— **这是静态 CSS 拿到检查器值的唯一途径**；
- **区块预设**：`presets[0].document` 引用内置 `core.container` / `core.heading`，
  以及本插件自己的组件，插入工作台即得一整块内容区。

## 三条容易踩的线

1. 静态 CSS 是全局的，**必须自己做类名前缀隔离**（这里用 `.nc-`）。它不会
   被自动包进组件作用域：写 `.card { ... }` 会和主题、别的插件的同名类互踩；
2. `assets/*.css` 里的 `@import` 会被平台剥掉（外联是数据外泄通道），
   需要引第三方字体/样式请改用平台统一管理的 `<link>`；
3. 预设里的 `props` 键必须是该组件 manifest 声明过的键，否则页面编译时
   按「未声明的键」拒绝（白名单），错误信息会点名组件类型与键名。

能力边界（能做什么 / 不能做什么）见 `docs/06-E-plugin-authoring.md`。
