# 02-C4 · 共享控件组与面板分组规范 (Control Groups & Sections)

本文档规范组件 Controls 体系的共享组（Group）与 Inspector 面板分组（Section），
对标 Elementor Group_Control + start_controls_section 的理念，声明式实现（无 PHP 嵌套）。

## 1. 共享组（Groups）

对标 `Group_Control_Typography`：跨组件复用的属性集合定义为 core 共享组，
一份校验 + 一份 CSS 生成，组件嵌入即用。

| 组 | 位置 | 内容 |
|---|---|---|
| `core.TextStyle` | `core/groups.go` | 三端字号/行高/对齐（含 clamp 流式字号白名单）；`ValidateTextStyle` 校验、`BreakpointDecls(bp)` 断点 CSS |

已接入：`core.heading.Typography`、`core.text.Typography`（原各自一份的 ResponsiveTypography/appendTypography 已删除，净减约 90 行重复）。

后续组按需增补：`core.TextShadow`、`core.Border`（Advanced 已有 BorderProps，可提升为组）、`core.Spacing`。

## 2. 面板分组（Sections）

ct tag 参数 `sec=` 声明控件归属，Inspector 面板按固定顺序渲染：

| 组 | 顺序 | 内容 |
|---|---|---|
| `content` | 0（默认） | 内容与专属配置 |
| `style` | 1 | 排版/颜色/视觉 |
| `advanced` | 2 | Advanced 通用层（02-C0） |

```go
Weight string `json:"weight" ct:"string,maxlen=10,sec=style"`
Text   string `json:"text"   ct:"text,maxlen=500"`            // 默认 content
```

`SchemaJSON` 按 section 分桶输出（桶内保持字段声明序），确定性排序。

## 3. 新增控件类型

| 控件 | 语义 | 校验 |
|---|---|---|
| `slider` | int 值域 + `step=`（编辑器滑块交互元数据） | 与 int 同（min/max，零值放行） |
| `url` | 链接字段 | 协议白名单：http/https/mailto/站内相对路径/`#` 锚点；字符集白名单（含 @ 供 mailto），禁引号/尖括号 |

## 4. 实现映射

| 条目 | 实现 |
|---|---|
| TextStyle 共享组 | `core/groups.go`（ValidateTextStyle / BreakpointDecls） |
| sec= 分组 + step= | `core/controls.go`（parseControlTag / SchemaJSON 分桶） |
| slider / url 控件 | `core/controls.go`（ControlSlider / ControlURL + isSafeHrefURL） |
| 组件接入 | heading（Typography 换共享组、style 字段加 sec=style）、text（Typography 换共享组） |
| 单元测试 | `public/test/builder/unit/groups_test.go`（分组解析/schema 顺序/slider 值域/url 协议/TextStyle 组） |