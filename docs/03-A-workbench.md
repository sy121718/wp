# 03-A · 可视化编辑器实现说明（后端契约部分）

> 基于用户规范《03-A Visual Workbench Specification》。本文件为后端/编译侧契约的实现映射，
> 编辑器前端外壳（顶栏/底栏/画布/大纲树 UI、拖拽交互、快捷键）属 03-B 前端工作台范围。

> **实现状态以代码为准**（最后核对：2026-09-13）。撤销/重做（100 层栈）、画布拖拽、
> 结构树 HTMX、检查器 HTMX 等已在 `internal/templates/static/js/workbench/` 落地；
> 下文 §2「明确不做」仅指 03-B 规划项中**尚未实现或未迁入本仓库**的部分。
>
> 2026-09-12 补充：tabs/accordion 的对齐重复项契约改由组件的 `AlignedRepeaterProvider`
> 就近声明，注册时核对真实 Props；SSR 直接消费，JS 经 `go run ./cmd/workbench-contracts`
> 生成。生成漂移和真实 HTTP→JS→Go 编译链路纳入 `scripts/check-workbench.sh`。
> 基础体系、公共 UI 边界与后续收敛见 `docs/11-foundation-and-open-source.md`。

## 1. 本次实现范围（后端契约）

### 1.1 Page Document 编辑元数据（规范 §4.1 重命名/辅助控制）

| 字段 | 语义 | 编译行为 |
|---|---|---|
| `node.name` | 大纲树重命名显示名（如"首屏 Banner 容器"） | 校验（≤100 字符）后持久化；**不进入产物** |
| `node.hidden` | 编辑期临时显隐（遮挡辅助） | 不进入产物（纯编辑器状态） |
| `node.locked` | 编辑期锁定防误触 | 不进入产物（纯编辑器状态） |

校验入口：`core.ValidateNodeID(id, name, ids)`（基座）+ 容器入口补 `ValidateNodeName`。

### 1.2 容器能力扩展（规范 §3.1）

**Tab1 布局**：

- 定位系统 `position`：static / relative / absolute（top/right/bottom/left 坐标，至少一个）/
  sticky / **drawer**（left/right/bottom 滑出 + 遮罩 + 唯一触发 ID）。
- Drawer 零 JS 实现：`position: fixed` + 移出视口 transform，`:target`（触发 `href="#sky-drawer-<id>"`）滑入。
- 语义标签白名单新增 `<main>`。
- `styleEx.order`：flex/grid 子项顺序（-1~99）。

**Tab2 样式**：

- 背景双态：`backgroundHover`（悬停背景，含过渡）。
- 背景覆盖层 `overlay`：::before 半透明遮罩 + 子内容提升 z-index（文本可读性）。
- 形状分隔线 `shapeDivider`（wave/slant/curve 纯 SVG 白名单，top/bottom 位置、
  着色随背景色）。单层 DOM 承诺内的装饰元素（容器内部 span.sky-shape，非包装节点）。

**Tab3 扩展**：

- 组父联动 `groupParent`：输出 `data-sky-group="true"` 标记，子组件经该标记实现 hover 联动
  （后续子组件 hover 反馈默认挂父联动协议）。
- 自定义属性 `attributes`：key 白名单（data-*/aria-*/role/title/tabindex）+
  value 独立安全白名单（允许中文，禁引号/尖括号/反斜杠/反引号防属性逃逸）。

### 1.3 构建契约（规范 §6）

前端产 AST JSON → `builder.Compile` 全链路已经闭环（02 系实现），本次校验层完备：
Node 新字段均过白名单后持久化，编译输出零影响。

## 2. 前端实现状态（原 03-B 范围）

| 能力 | 状态 | 落点 |
|---|---|---|
| 撤销 / 重做 | ✅ 已实现 | `methods/state.js`（栈深 100，Ctrl+Z / Ctrl+Y） |
| 画布拖拽 / pointer 交互 | ✅ 已实现 | `methods/canvas.js` |
| 结构树渲染与拖拽 | ✅ 已实现 | HTMX `outline_tree` + `methods/tree.js` |
| 检查器 / 设置 / 全局 / 历史面板 | ✅ 已实现 | HTMX 片段 + 薄 JS 绑定 |
| 四区布局 UI、顶栏/底栏完整视觉 | 部分 | 外壳在 Jet `workbench/layout.html`，非 Elementor 式四区 |
| Live Preview 新标签页 | 未做 | 预览仍在 iframe 内 |
| 多选、完整快捷键矩阵 | 部分 | 见 `methods/shortcuts.js` |

后端 AST/编译契约（§1）始终有效；前端以 ES modules + HTMX 为主路径（见 `docs/09-session-handoff.md` §3）。

### 2.1 检查器控件的双轨分工（审计 EDT-009）

检查器面板的字段来自两个渲染来源：服务端 `inspector_handle.go` 出片段，
客户端 `methods/controls/*.js` 出控件本体。两种来源并存是既成事实，
**规则不是「统一到一边」，而是「按需不需要控件本体分派」** —— 写在这里，
免得每加一个字段都重新争论一次。

**服务端负责**（`inspectorFieldOf`）：

- 把组件 `ct` 标签声明的 schema 转成字段（label / min / max / step / placeholder）；
- **取数**：实体引用的候选项要查库（`entityRefInspectorOptions` 按工程与实体类型取），
  这一层客户端拿不到，必须服务端做；
- **判型并出完整控件**：`bool` / `select` / `textarea` / `number` / `classes` / `cssdecls` —— 它们的形态是「一个值 + 一组选项」，
  服务端直接渲染即可。

**客户端负责**（`schemaField` / `fillInspectorSlots`，见 `controls/` 下各文件）：
服务端对复杂控件只输出 `data-wb-slot` 占位，客户端按 slot/kind 就地填充 ——
这样既保留了服务端「知道有哪些字段、什么顺序、什么分组」的权威，
又不用把第三方 UI 实例的初始化塞进 Go 模板。

| Slot / kind | 控件 | 为什么必须在客户端 |
|---|---|---|
| `richtext` | Trix | 第三方编辑器实例（`core.text` 的 plaintext 模式回退多行输入） |
| `color` | 取色器 | 需要调色板 UI 与即时预览 |
| `spacing` / `boxspacing` / `margin` | 四向输入 + 联动锁 | 一个字段对应多个输入，交给模板反而更难维护 |
| `rtext` | 三端响应式输入 | 要在断点之间切换同一字段 |
| `media` / `mediaList` | 媒体库选择器 | 要打开媒体库、支持多选与排序 |
| `dimension` | 带单位数值 | 数值与单位分离 |
| `collectionfield` / `bindingfield` | 白名单下拉 | 选项依赖当前节点的「内容集合」，随节点变化要重取；手填会绕过白名单 |

另有一类**服务端片段里根本没有**的内容，由客户端整体补上（`renderInspectorExtras`）：
repeater 类手写面板（list / infobox / faq / social / nav / marquee / gallery）、
排版与动效面板、以及面板底部的「复制组件 / 粘贴样式」按钮。
它们经 `sectionFor()` / `into()` 落进**服务端渲染的** `<details>` 分组里，
按 `data-wb-section` 找容器、找不到才新建 —— 顺序与折叠状态因此仍由服务端决定。

**新增字段时的判据**：字段值能不能表达成「一个字符串 + 一组选项」？
能 → 服务端出完整控件；需要多输入、第三方 UI 实例、跨字段联动或异步取数 → 服务端只出 slot。
两种都做不算违规，但**同一个字段不要两边各写一份**：slot 为空即服务端负责，客户端不要再接管。

外观一致性由 `workbench.css` 的字段容器类与客户端控件共同保证，
新增控件时沿用既有类名，不要另起一套。

## 3. 实现位置

| 文件 | 内容 |
|---|---|
| `core/component.go` | Node 编辑元数据字段（name/hidden/locked） |
| `core/atom.go` | ValidateNodeID 签名扩展 + ValidateNodeName |
| `components/container/container.go` | Tag+main / Position 系统 / StyleEx（双态背景/遮罩/形状/顺序/组父/属性） |
| `public/test/builder/unit/workbench_test.go` | 5 组测试（元数据/定位/样式扩展/校验拒绝/超长名） |
