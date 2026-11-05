# Workbench UI 参考基线（MONO / CANVAS）

本目录是可视化编辑器的**交互与视觉参照实现**，2026-10-27 由用户提供并确认为目标形态。

- 来源：`/tmp/gowp-workbench-demo/`（原型，单文件无构建、无后端）
- 定位：**规格**，不是产品代码。工作台仍走 Jet + 原生 JS modules + 现有 AST / 构建管线；
  不引入第二套布局存储、不引入 React/Vue、不引入新的渲染协议。
- 任务书入口：`GO_WP_FULL_SYSTEM_TASKBOOK_2026-10-27.md` §B09.0
- 文件：`index.html` / `editor.css` / `model.js` / `editor.js`（原型源码，可直接浏览器打开）
- 截图：`*.webp`（1440–1600 宽实测，refined-* 为最终形态）

## 1. 布局：两栏 + 画布 + 浮层结构树

```text
┌──────────────────────────────────────────────────────────────┐
│ topbar 56px：brand │ 文档名+状态 │ 居中设备切换 │ 撤销/重做/状态/预览/发布 │
├────────────┬─────────────────────────────────┬───────────────┤
│ 左栏 280px │ 画布工具条 38px（面包屑+提示）   │ 结构树 240px  │
│ (flex 0 0) │                                  │ (fixed 浮层)  │
│            │ 画布 flex:1 / bg #f4f4f4         │ 可拖动/折叠/关 │
│ 组件库 ⇄   │ padding 24px                     │               │
│ 检查器     │ 画布白底 max-width 1240          │               │
│ (互斥)     │                                  │               │
└────────────┴─────────────────────────────────┴───────────────┘
```

**关键：左栏不是五面板 + 符号 rail 切换，是两态互斥。**

| 状态 | 触发 | 内容 |
|---|---|---|
| 组件库 | 未选中 / 点「返回组件库」(×) / 点画布空白 | 页签 组件 \| 全局 + 搜索 + 分类 chips + 2 列网格 |
| 检查器 | 选中任意节点 | 「正在编辑」+ 节点名 + `type · id` + 四页签 + 字段 |

右栏结构树是 `position: fixed; right: 24px; top: 118px; width: 240px` 的**浮层卡片**，
带拖动手柄（pointerdown 拖 head，夹在 56px 顶部与视口内）、折叠（−）、关闭（×）；
关闭后画布工具条出现「页面结构」按钮唤回。**结构树不参与 flex 布局，不挤压画布宽度。**

## 2. 顶栏

从左到右，固定顺序：

1. `brand`：`mono / canvas`（本项目替换为产品名）
2. `divider` 1px 竖线
3. `document`：`<strong>首页</strong>` + 灰色状态词（`草稿`）
4. `devices`：**绝对居中**（`left:50%; transform:translateX(-50%)`），三档图标按钮
   - desktop「桌面」→ 画布 `max-width:1240px`，面包屑显示「响应式」
   - tablet「平板」→ `max-width:768px`，面包屑显示「768 px」
   - mobile「手机」→ `max-width:375px`，面包屑显示「375 px`
   - 按钮 30×26，激活态黑底白图标；图标是纯 CSS 画的矩形，不用 emoji
5. `top-actions`（margin-left:auto）：撤销 ↶ / 重做 ↷ / 状态文本 / 「预览」次按钮 / 「发布」主按钮（黑底白字）

**没有**符号 rail（▦ ＋ ⚙ 🔍 ◐ ↺ ☰），**没有**五档断点常驻。

## 3. 画布

- 工具条 38px：左面包屑 `页面 / 响应式`；右「双击文字直接编辑」提示 +
  树关闭时的「页面结构」按钮。**提示语用灰字，不做浮层。**
- 画布 shell：`flex:1; overflow:auto; padding:24px`，背景 `#f4f4f4`
- 画布本体：白底、`box-shadow: 0 1px 5px #0000000d`、`width:100%`、
  `min-height: calc(100vh - 142px)`、`transition: width .2s`

### 3.1 节点选中态（三件套，必须同时出现）

```text
选中节点：
  ├ 外框 outline:1px solid #111（hover 时 #d0d0d0）
  ├ 左上 nodelabel：黑底白字 9px，显示 props.name，pointer-events:none
  └ 右上 nodeactions：复制 ⧉ / 删除 ×，23×20 黑底白字按钮
```

### 3.2 空容器（可投放落点）

```css
.emptycontainer { width:100%; min-height:80px; display:grid; place-items:center;
                  border:1px dashed #bbb; color:#777; font-size:11px; }
```
文案：`拖入组件或点击左侧组件`。**空容器永远有可见落点，不是 64px 空白。**

### 3.3 拖放三态反馈

| 类名 | 视觉 | 何时 |
|---|---|---|
| `drop-before` | `box-shadow: 0 -3px 0 #000` | 非容器节点，鼠标在该节点上半 |
| `drop-after` | `box-shadow: 0 3px 0 #000` | 非容器节点，鼠标在该节点下半 |
| `drop-inside` | `outline: 2px solid #000; outline-offset:-3px` | 目标是容器 |

拖放时**先清除全部三态再添加当前态**，`dragleave` 清除。

### 3.4 右键菜单

`position:fixed` 140px 宽白卡：`复制组件` / `隐藏 / 显示` / `锁定 / 解锁` / `删除组件`（danger）。
点击画布任意处或菜单外关闭。隐藏节点 `opacity:.35`，锁定节点 `cursor:not-allowed` 且不可拖。

### 3.5 收起左栏 = 全宽实时预览

点左栏标题右侧「‹ 收起」→ `app.collapsed.previewing`：

- 左栏 `display:none`
- 画布 `max-width:none; width:100%; min-height:100%; box-shadow:none`，shell `padding:0`
- 隐藏全部编辑装饰：`outline` / `nodelabel` / `nodeactions` / `emptycontainer` / `canvas-toolbar`
- 节点 `cursor:default`、不可拖
- 左边缘中央出现「›」把手（24×72，`left:0; top:50%`，右边圆角，白底 + 阴影），点击恢复编辑

**收起入口必须常驻可见**：`#collapse` 是带文字的按钮（`‹ 收起`，min-width 54，28 高，灰底），
不是裸箭头图标。

## 4. 检查器（四页签）

页签顺序固定：**内容 → 布局 → 样式 → 动效**。

- 头部：「正在编辑」小字 + 节点名（15px）+ `type · id`（10px 灰）+ 右「×」返回组件库
- 字段由 `add(label, key, type, tab, options)` 声明，按当前页签过滤渲染
- 内容页签额外：**置顶**「结构名称」输入 + **底部** 2 列动作网格（复制组件 / 隐藏·显示 / 锁定·解锁）
- 控件类型：text / textarea / number / color / select / checkbox
- 字段顺序：内容 → 布局 → 样式 → 动效，每页签内按 add 顺序

### 4.1 各类型字段归属

| 组件 | 内容 | 布局 | 样式 | 动效 |
|---|---|---|---|---|
| heading / text / button / product | 文字内容 | — | 字号、文字对齐（heading/text） | 入场动效 |
| image | 图片 URL | 图片高度 | — | 入场动效 |
| product | 价格 | 图片高度 | — | 入场动效 |
| gallery | — | 列数、图片高度 | — | 入场动效 |
| container | — | **排列方向、水平分布、交叉对齐、项目间距、允许换行** | — | 入场动效 |
| 全部 | 内边距、外边距、宽度百分比、最小高度 | ← 同左（布局） | 背景色、文字颜色、圆角 | 入场动效 |

**容器排版的五要素全部在「布局」页签，不在「内容」或「样式」里。**

## 5. 组件库

- 搜索框（34px 高，focus-within 边框变黑）
- 分类 chips：`全部 / 布局 / 内容 / 媒体 / 商品 / 区块`，激活态黑底白字
- 分组标签：`基础组件`
- 2 列网格 `repeat(2,1fr)`，gap 8px，每项 min-height 76px，symbol 23px + 名称
- **区块类（section）横跨整行**：`grid-column: 1/-1; min-height:62px; flex-direction:row`
- 点击或拖拽都能插入；点击时若当前选中容器 → 插入容器内，否则插入根尾部
- 「全局」页签：页头 / 页脚两条大项（symbol + 名称 + 说明 + ＋）

## 6. 结构树

- 树项 min-height 30px，`padding: 5px 8px`，激活 `background:#f0f0f0; font-weight:500`
- 右侧拖拽手柄 `⋮⋮`（`letter-spacing:-2px`，灰色）
- 嵌套层 `margin-left:12px; border-left:1px solid #eee; padding-left:4px`
- 拖到树项上 `box-shadow: inset 0 0 0 1px #000`
- 隐藏节点前缀 `○`，锁定节点前缀 `▣`
- 点树项 = 选中画布同一节点

## 7. 状态与历史

- **唯一状态出口** `#status`：`所有更改已保存` / `未保存` / `已保存` / `未保存`
- **唯一历史结构** `timeline[] + timelineIndex`，上限 100 条，`commit(label)` 时
  `timeline = timeline.slice(0, index+1); push(snapshot()); index = length-1`
- 撤销/重做按钮 `disabled` 由 `timelineIndex === 0` / `=== length-1` 直接决定 ——
  **按钮态与内部状态同源，不各算一遍**
- 每次 commit 后同步写 localStorage 备份（`mono-canvas-backup`）；保存成功清除

## 8. 键盘

| 键 | 动作 | 边界 |
|---|---|---|
| Ctrl/Cmd + S | 保存 | 始终生效 |
| Ctrl/Cmd + Z | 撤销 | 始终生效 |
| Ctrl/Cmd + Y | 重做 | 始终生效 |
| Delete | 删除选中 | 焦点在 INPUT / TEXTAREA 内时不触发 |
| Esc | 退出直接编辑并**立即还原**原文字 | 见 §9 |

## 9. 直接编辑

- 双击 heading / text → `contentEditable="plaintext-only"`
- **进入编辑前保存原始文字与选区**
- Esc → 同步恢复原 DOM 并完全退出，**不提交、不产生历史**
- blur / Enter → 提交（blur 不得把 Esc 取消的值再提交一次）
- 一次连续编辑只产生一条历史记录

## 10. 视觉基线

- 字号体系：body 12px；`brand` 14px/700；树项与字段 11px；`element-id` 与 `eyebrow` 10px；
  节点名 15px；`nodelabel` 9px
- 颜色：纯黑白灰（`#111 / #777 / #e8e8e8 / #f4f4f4`），无品牌色；
  激活态 = 黑底白字，选中态 = 黑描边
- 圆角：3px（小控件）/ 4–6px（按钮、输入）/ 7px（浮层卡片）
- 阴影：`0 1px 5px #0000000d`（画布）、`0 8px 30px #00000012`（浮层）、`0 6px 24px #0002`（右键菜单）
- `focus-visible` 统一 `outline:2px solid #000; outline-offset:2px`
- 禁用态 `opacity:.28; cursor:default`
- 过渡：`border-color .15s, background .15s, transform .15s`（palette 项 hover 上移 1px）；
  画布宽度 `.2s`
- 响应式：`max-width:1000px` → `.sidebar` 250（`flex-basis:250px`）、`.tree` 210、`.canvas-shell` padding 14、
  `.status` 与 `.hint` 隐藏；
  `max-width:720px` → `.document` 与 `.divider` 隐藏、`.topbar` gap 6 / padding 0 10、`.devices` 改为
  `position:static; transform:none`（不再绝对居中）、`.top-actions` gap 3、`.canvas-shell` padding 8、
  `.sidebar` 改 `position:absolute; top:56px; bottom:0`、`.tree` right 10、`.secondary/.primary` padding 0 10

## 11. 与当前实现的差距（实施时的 checklist）

| # | 现状（`internal/templates/workbench/layout.html` / `static/css/workbench.css`） | demo 目标 |
|---|---|---|
| 1 | 顶栏 7 个符号 rail（▦＋⚙🔍◐↺☰）+ 5 档断点 | 无 rail；3 档设备图标；文档名 + 状态在左 |
| 2 | 左栏 5 个 `<section class="wb-panel">` 共存，靠 rail 切 | 组件库 / 检查器**两态互斥** |
| 3 | 检查器 2 页签（内容/样式） | 4 页签（内容/布局/样式/动效） |
| 4 | 结构树 `position:absolute; right:12px; top:12px` 覆盖画布 | `fixed` 浮层卡片，可拖动/折叠/关闭/唤回 |
| 5 | 空容器无提示（实测 64px 高空白） | `.emptycontainer` 虚线框 + 「拖入组件或点击左侧组件」 |
| 6 | 无「收起左栏 = 全宽预览」 | `collapsed.previewing` + 左边缘把手 |
| 7 | 底栏 `wb-bottombar` 5 个按钮（页面设置/大纲树/撤销/重做/聚焦模式） | 无底栏，全部上移顶栏或左栏 |
| 8 | 画布 `padding:10px` | `padding:24px`，背景 `#f4f4f4` |
| 9 | 选中只有 `outline:1px solid var(--sky-c-primary, #3d444f)`（`workbench.css:90`），无标签无动作 | 纯黑 `#111` 描边 + 左上 nodelabel + 右上 nodeactions |
| 10 | 拖放三态用 `var(--sky-c-primary, #3d444f)` 的 2px 线（`:92-99`）+ 虚线 `放入容器` | `box-shadow: 0 -3px / 0 3px 0 #000`、`outline:2px solid #000; outline-offset:-3px`，纯黑 |

`#3d444f` 已是深灰蓝、观感接近黑；**要改的不是色相而是形态**：现在的选中只有一圈细描边，
没有标签、没有动作按钮；拖放是 2px 发丝线不是 3px 实心条。差距在结构与粗细，不在“蓝色太亮”。

**注意**：以上是**布局与交互形态**的差距，不涉及 AST、组件 schema、构建管线、发布协议。
容器 Flex/Grid 的五要素在当前实现里已经存在（`internal/builder/components/container/container.go`），
差距是**它们出现的位置**（散在内容/样式）与**默认可见性**（折叠）—— 这是 B09.3 要解决的。

## 12. 不得照搬的部分

- 原型用 `localStorage` 存草稿、`document.write` 开预览窗 —— 产品走 `/api/` + 统一写基座
- 原型的空 span 图标（□ T ≡ ▭ ▧ ▦ ▤ — ↕ ◫）是占位，产品应换成 `icons.js` 的真实图标集
- 原型的`初始文档`（FORME 品牌、衬衫、价格）只是演示数据，不进入产品的默认页面
- 原型没有权限、无多语言、无媒体库、无 SEO、无历史版本 —— 这些产品能力一个都不能砍
