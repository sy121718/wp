# internal/templates 约定

后台页面与片段的 Jet 模板。渲染入口是 `NewJetHTMLRender`（内嵌文件系统，生产不依赖工作目录）。

## 可选数据键必须用 isset 判断

```jet
{{if isset(.Blueprints)}} … {{end}}   {* 正确 *}
{{if .Blueprints}} … {{end}}          {* 数据里没有这个键时整页渲染中断 *}
{{if len(.Blueprints) > 0}} … {{end}} {* 同上 *}
```

Jet 的 `if` 要求 bool，而缺失的 map 键求值成 nil —— 类型不符会让渲染在那一行中断。
现象有很强的欺骗性：**HTTP 状态码仍是 200**，中断点之前的 HTML 正常输出，之后的内容
整块消失（列表、表格全没了）。从「列表里少了一行数据」这种表象，几乎不可能定位到
模板中间某一行 if。

因此：只有**保证存在**的键才直接参与判断；可选键（新增的、只由部分渲染路径提供的）
一律用 `isset` 包裹。`admin/pages.html` 的蓝图下拉踩过一次（直接渲染模板的单测不带该键）。

## 键名大小写

`admin/layout.html` 按 `{{.title}}` / `{{.menu}}` 取小写键，各页面的列表数据用大写
（`{{.Pages}}` / `{{.Projects}}`）。新增页面时对齐既有页面的写法，不要自创一种。

## 数据准备

页面数据由各 handle 的 `templateMap()` 组装（gin.H），模板只做渲染、不做查询。
