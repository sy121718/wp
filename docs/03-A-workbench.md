# 03-A · 可视化编辑器实现说明（后端契约部分）

> 基于用户规范《03-A Visual Workbench Specification》。本文件为后端/编译侧契约的实现映射，
> 编辑器前端外壳（顶栏/底栏/画布/大纲树 UI、拖拽交互、快捷键）属 03-B 前端工作台范围。

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
- Drawer 零 JS 实现：`position: fixed` + 移出视口 transform，`:target`（触发 `href="#wp-drawer-<id>"`）滑入。
- 语义标签白名单新增 `<main>`。
- `styleEx.order`：flex/grid 子项顺序（-1~99）。

**Tab2 样式**：

- 背景双态：`backgroundHover`（悬停背景，含过渡）。
- 背景覆盖层 `overlay`：::before 半透明遮罩 + 子内容提升 z-index（文本可读性）。
- 形状分隔线 `shapeDivider`（wave/slant/curve 纯 SVG 白名单，top/bottom 位置、
  着色随背景色）。单层 DOM 承诺内的装饰元素（容器内部 span.wp-shape，非包装节点）。

**Tab3 扩展**：

- 组父联动 `groupParent`：输出 `data-wp-group="true"` 标记，子组件经该标记实现 hover 联动
  （后续子组件 hover 反馈默认挂父联动协议）。
- 自定义属性 `attributes`：key 白名单（data-*/aria-*/role/title/tabindex）+
  value 独立安全白名单（允许中文，禁引号/尖括号/反斜杠/反引号防属性逃逸）。

### 1.3 构建契约（规范 §6）

前端产 AST JSON → `builder.Compile` 全链路已经闭环（02 系实现），本次校验层完备：
Node 新字段均过白名单后持久化，编译输出零影响。

## 2. 明确不做（03-B 前端工作台）

四区布局 UI、顶栏/底栏控件、选择器/大纲树渲染、拖拽重排与嵌套、快捷键体系、
右键菜单、多选、历史快照（Undo/Redo 前端状态机）、Live Preview 新标签页、保存/发布按钮。
上述均为编辑器前端；后端已提供其全部数据契约（AST/元数据/编译接口）。

## 3. 实现位置

| 文件 | 内容 |
|---|---|
| `core/component.go` | Node 编辑元数据字段（name/hidden/locked） |
| `core/atom.go` | ValidateNodeID 签名扩展 + ValidateNodeName |
| `components/container/container.go` | Tag+main / Position 系统 / StyleEx（双态背景/遮罩/形状/顺序/组父/属性） |
| `public/test/builder/unit/workbench_test.go` | 5 组测试（元数据/定位/样式扩展/校验拒绝/超长名） |