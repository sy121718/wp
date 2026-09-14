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

## 3. 实现位置

| 文件 | 内容 |
|---|---|
| `core/component.go` | Node 编辑元数据字段（name/hidden/locked） |
| `core/atom.go` | ValidateNodeID 签名扩展 + ValidateNodeName |
| `components/container/container.go` | Tag+main / Position 系统 / StyleEx（双态背景/遮罩/形状/顺序/组父/属性） |
| `public/test/builder/unit/workbench_test.go` | 5 组测试（元数据/定位/样式扩展/校验拒绝/超长名） |
