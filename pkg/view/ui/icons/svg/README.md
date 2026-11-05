# 图标库（Lucide）

本目录是 go_wp 的**内置图标库**，供组件（list / infobox / button / divider / icon 等）通过 `core.IconSVG` / `core.IconContent` 引用。数据经 `go:embed`（`icons/*.svg`）打包进二进制，构建期零 IO、确定性。

> 注意：本目录只有 `*.svg` 会被 `core/icons.go` 的 `//go:embed icons/*.svg` 打包，本 README 与其它文件不影响二进制体积。

## 数据源

- **Lucide Icons**：<https://github.com/lucide-icons/lucide>（ISC 协议，Feather 官方超集）
- 图标规模：1821 个描边图标（24 viewBox，stroke 风格）+ 46 个实心 `-fill` 版本 = **1867 个**
- Lucide 的每个图标在仓库里有对应 `icons/{name}.json`（含 `tags` 英文关键词 + `categories` 英文分类），是分类/关键词元数据的来源

## 目录与格式约定

```
icons/
├── arrow-right.svg      # 描边图标（默认风格）
├── star.svg             # 描边
├── star-fill.svg        # 实心（-fill 后缀）
├── heart-fill.svg
└── README.md            # 本文档（不被打包）
```

**SVG 必须规范化**（参照现有文件）：

```svg
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="..."/></svg>
```

- 单行、无 `xmlns` / `width` / `height` / `class` 属性
- 描边图标：`fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"`
- 实心图标（`-fill`）：`fill="currentColor"`（去掉 stroke 系列属性）

## 元数据（core/icons.go）

图标元数据集中在 `internal/builder/core/icons.go` 的 `iconCatalog`：

```go
var iconCatalog = map[string]IconMeta{
    "arrow-right": {Category: "arrows", Label: "右箭头", Keywords: []string{"arrow", "右箭头", "方向"}},
    "star-fill":   {Category: "social", Label: "实心星", Keywords: []string{"star", "星形", "评分"}},
}
```

- `IconMeta{Category, Label, Keywords, Style}`
- `Category`：18 个语义分类（见下）
- `Label`：中文标签（图标选择器 hover 提示 + 搜索匹配）
- `Keywords`：英文名 + 中文同义词（搜索匹配）
- `Style`：`outlined`（描边）/ `filled`（实心），由 `-fill` 后缀在 `init()` 自动派生

### 18 个分类

`basic` `arrows` `communication` `social` `commerce` `media` `editor` `files` `weather` `security` `navigation` `development` `devices` `gaming` `layout` `food` `time` `health`

> 分类名在 `IconCategories()` 的 order 里登记，改分类需同步前端 `icons.js` 的 `CATEGORY_LABELS`。

## 查询接口（core/icons.go）

| 函数 | 用途 |
|---|---|
| `IconSVG(name)` | 返回完整内联 SVG |
| `IconContent(name)` | 返回内容片段（如 `<path .../>`，不含 `<svg>` 骨架） |
| `IconNames()` | 全部图标名 |
| `IconCategories()` | 全部分类（固定顺序） |
| `IconCategory(name)` / `IconLabel(name)` / `IconStyle(name)` | 单项元数据 |
| `FilterIcons(category, style, keyword)` | 分类 + 风格 + 关键词联合筛选 |

## 如何新增一个图标

1. 从 Lucide（或其它 stroke 风格库）拿 SVG，规范化为上述单行格式，存 `icons/{name}.svg`
2. 在 `iconCatalog` 加一条（分类 + 中文标签 + 关键词）
3. 若需要实心版：再存 `icons/{name}-fill.svg`（`fill="currentColor"`），分类/标签会自动从描边版派生
4. 同步前端 `internal/templates/static/js/icons.js`（`ICONS` + `CATEGORY_OF` + `LABELS`）
5. 验证：`go test ./internal/builder/core/` + `node --check internal/templates/static/js/icons.js`

## 实心版本（-fill）约定

- 只有**闭合形状**图标才有实心意义（路径闭合后 `fill` 才能填充）；线条类（arrow / check / chevron / menu / plus 等）不要生成实心版
- 已跳过「实心后退化成纯几何块或语义丢失」的图标（如 alert-circle 实心后内部线条消失只剩圆）

## 前端同步

`internal/templates/static/js/icons.js` 暴露 `window.WPIcons`，与 Go `iconCatalog` 一一同步：

```js
window.WPIcons = {
    names, categories,
    svg(name), label(name), categoryOf(name), styleOf(name),
    categoryLabel(c), filter(category, style, keyword)
}
```

- 图标选择器（workbench.js 的 `iconFilterBar`）用 `filter()` 做「搜索 + 分类 + 实心描边」三路筛选
- **改图标/分类/标签时，Go 与 JS 必须同步**，否则选择器与后端查询结果不一致

## 换库 / 扩展

- 想换图标库：替换 `icons/*.svg` + 重建 `iconCatalog` + `icons.js`，渲染逻辑（`core.IconSVG` / 组件 `.jet`）不用动
- 想用 Lucide 剩余未收录的图标：按「如何新增」流程逐个补即可，不影响现有
