# Ant Design 参考：把成熟组件库的操作逻辑与交互范式，映射到本项目

> 用途：本项目的后台组件是**手写 CSS + 原生 JS 基座**（`static/js/ui/`、`ui.css`），
> 已自成体系但存在「操作逻辑不统一、控件形态粗糙」的问题。本文提取 Ant Design（及 React
> 生态通行做法）中**可迁移的交互契约**，供逐组件改造时对照。
>
> **本文不是「把项目改成 antd」**。技术栈不同（本项目是 Go + Jet + HTMX 服务端渲染，
> 无 React 运行时），能迁移的是**交互契约与视觉语言**，不是组件实现。
>
> 参考源：`/ant-design/ant-design`（context7，2026-09-08）与官方文档
> <https://ant-design.antgroup.com/components/overview-cn/>

## 0. 迁移原则（先读这条，否则会走偏）

| 能迁移 | 不能迁移 |
|---|---|
| 交互契约（点击后发生什么、状态如何流转） | React 组件实现（`<Table />` / `useState`） |
| 视觉语言（间距节奏、层次、状态色语义） | CSS-in-JS / 主题 token 系统 |
| 信息密度与对齐规范 | 客户端渲染的数据流（本项目是服务端渲染 + HTMX 局部替换） |
| 键盘与无障碍行为 | 受控组件的受控语义 |

**本项目已有的等价物**（改造时优先复用，不要新造）：
`.page-head` / `.filter-bar` / `.list-card` / `.data-table` / `.col-actions` / `.bulk-bar` /
`.empty-state` / `.section-fold` / `.kv` / `.help` / `data-drawer-*` / `data-confirm` /
`WBUI.select` / `WBUI.toast` / `WBUI.busy`。

## 1. 表格（Table）—— 本项目 `.data-table` 的差距

antd Table 的成熟约定（可迁移部分）：

| 约定 | antd 做法 | 本项目现状 | 该不该跟 |
|---|---|---|---|
| **列宽策略** | 关键列固定宽，弹性列自动 | 无显式列宽，靠内容撑开 | **跟**：列宽漂移是「读不进去」的主因 |
| **排序** | 表头可点，箭头指示当前方向 | 多数列表无排序 | **跟**（服务端排序，`?sort=x&order=desc`） |
| **空态** | 表格内嵌 `Empty`，**保留表头** | 部分页面表格整个不渲染（`administrators`/`departments`） | **跟**：表头是认知锚点，删了用户不知道这列表有什么列 |
| **加载态** | 骨架屏 / 局部 spinner | 无（HTMX 请求期间无反馈） | **跟**（用 `WBUI.busy` 覆盖按钮 + 行级骨架） |
| **固定操作列** | `fixed: 'right'` | 靠 `col-actions` 右对齐，横向滚动时会滚走 | **可选**：列多时才有价值 |
| **行选择** | `rowSelection` 首列勾选 + 表头全选 | 已有（`data-check-all` + `data-check-item`） | 已具备 |
| **批量操作条** | 选中后表格上方浮出操作条 | 已有 `.bulk-bar` | 已具备 |
| **分页** | 右下角，可切页长 | `partials/pagination.html`（服务端） | 已具备，但**7 个列表页缺**（见 `02-I` §5） |

**改造要点**：本项目缺的是**排序、固定列宽、空态保留表头、HTMX 加载反馈**这四项。

## 2. 表单（Form）—— 本项目差距最大的一块

### 2.1 布局（antd 的核心约定，可完整迁移）

```
vertical（默认，推荐用于 ≥3 字段或含长字段）
┌─────────────────┐
│ Label           │   label 在上、控件在下
│ [Input........] │   label 与控件间距紧凑
└─────────────────┘
字段之间间距明显 > label 与控件的间距  ← 这条是「表单能不能读」的关键

horizontal（短字段密集场景）
Label（右对齐，固定宽）  [Input]
```

**本项目现状的问题**：`.form-group` 的 label 在上是跟了 antd，但**字段间距与 label 间距没有拉开层次** ——
读起来所有元素等距，眼睛分不出「这是一组」还是「这是两组」。

**可迁移的具体数值**（antd 的间距节奏，转成本项目 token）：

| 位置 | antd | 本项目应改成 |
|---|---|---|
| label 与控件 | 8px | `--sp-sm`（已是 8px）✅ |
| 字段与字段 | 24px | `--sp-xl`（已是 24px）✅ |
| 分组与分组 | 32px | `--sp-xxl`（已是 32px）✅ |

> 注：本项目 token 值本身就与 antd 对齐。问题不在 token，而在**用了哪些**——
> 大量页面还在用 `.form-group` 而不是 `.form-field` / `.form-grid`（`02-H` §1.2 记录过
> `.form-field` 曾长期没有规则）。改造时要逐页核对实际用的类。

### 2.2 校验与错误（antd 的 `Form.Item` 契约）

| 约定 | antd 行为 | 本项目应做 |
|---|---|---|
| 必填标记 | label 前红色 `*` | 已有（`.form-label.req`）✅ |
| 校验时机 | 失焦校验 + 提交时全量 | 现在只有 HTML5 `required`（提交才报） | 
| 错误展示 | **紧贴控件下方**，红字，不改变布局（预留高度） | 本项目多为页顶统一 `?err=` 回带 —— 用户要自己找哪个字段错了 |
| 错误态 | 控件边框转红 | 无 |

**这是本项目后台最值得跟的一项**：字段级错误就地提示，而不是页顶一句笼统的失败消息。
（`theme_settings`、商品编辑页这类长表单，页顶错误几乎等于没提示。）

### 2.3 提交按钮位置

antd 的 `Drawer` 范式（官方示例）：

```tsx
<Drawer
  title="Create a new account"
  extra={<Space><Button>取消</Button><Button type="primary">提交</Button></Space>}
>
  <Form layout="vertical">…</Form>
</Drawer>
```

**约定**：抽屉/弹窗的提交按钮在**头部右侧**（`extra`），表单滚动时按钮始终可见。

本项目现状：按钮在表单底部 `.form-actions`（滚动到底才看得到）。**长表单建议跟 antd** ——
把取消/保存在抽屉头部固定。

## 3. 数据录入控件的选型（antd 的组件分层）

| 数据形态 | antd 组件 | 本项目对应 | 备注 |
|---|---|---|---|
| 短文本 | `Input` | `.form-input` | ✅ |
| 长文本（多段） | `Input.TextArea`（`autoSize`） | `textarea` | 本项目部分页面误用单行（`product_brands` SEO 描述） |
| 数字/金额 | `InputNumber`（带步进、格式） | `type="number"` | 缺格式与边界提示 |
| 日期 | `DatePicker`（范围选择器） | `<input type="date">` × 2 | **跟**：范围应是一个控件（antd `RangePicker`），本项目用两个独立 date |
| 日期+时间 | `DatePicker showTime` | 少见 | — |
| 枚举（≤7 项） | `Radio.Group` / `Segmented` | `<select>` | **跟**：少量互斥选项用 Segmented（分段控件）比下拉快 |
| 枚举（>7 项） | `Select`（可搜索、可分组） | `<select>` → `WBUI.select` | **跟**：超过 ~10 项要支持搜索；本项目 `blocks` 的 16 项类型下拉需分组 |
| 多选枚举 | `Select mode="multiple"` | 多选 checkbox 组 | — |
| 布尔 | `Switch` / `Checkbox` | `checkbox` | ✅ |
| 开关式（启用/停用） | `Switch`（行内即时提交） | 表单里的 select | **跟**：状态类字段用 Switch 即时生效，不必进表单保存 |
| 富文本 | —（第三方） | Trix | ✅ |
| 标签输入 | `Select mode="tags"` | 无 | 可考虑 |
| 树选择 | `TreeSelect` | 原生 `<select>` 平铺 | **跟**：层级数据（分类、部门）用树选择 |

**最高价值的三项**：日期范围控件、可搜索下拉、树选择 —— 本项目这三处都在用最原始的原生控件。

## 4. 反馈与状态（antd 的 message/notification 分层）

| 场景 | antd | 本项目 |
|---|---|---|
| 操作成功（非阻塞） | `message.success()` 顶部浮出，2s 消失 | `WBUI.toast` ✅ |
| 操作失败（需确认） | `message.error()` | 多为页顶 `?err=` badge |
| 危险操作确认 | `Modal.confirm()` | `data-confirm` + `data-confirm-danger` ✅ |
| 加载中 | `Spin` / 按钮 loading | `WBUI.busy`（但**未被普遍使用**） |
| 空数据 | `Empty`（图示 + 描述 + 操作） | `.empty-state`（但 7 页执行不一致） |
| 长任务进度 | `Progress` | 无 |

**改造要点**：① HTMX 请求期间给按钮加 loading（`WBUI.busy` 已有，推广使用）；
② 失败反馈从「页顶一句」改成「贴近出错位置」。

## 5. 导航与容器

| antd | 本项目 | 建议 |
|---|---|---|
| `Tabs`（同一数据多视图） | `.tabs`（`theme.css` §13 已有） | **推广使用**：`analytics` 5 表、`masterdata_changes` 2 表都应改 tabs |
| `Collapse`（折叠面板） | `.section-fold` ✅ | 已具备 |
| `Descriptions`（键值详情） | `.kv` ✅ | 已具备 |
| `Drawer` | `data-drawer-*` ✅ | 已具备 |
| `Modal` | `.wb-modal` ✅ | 已具备 |
| `Breadcrumb` | 无 | 深层页面（编辑页、详情页）可加 |
| `Steps`（流程） | 无 | `returns`（审核→入库→退款）、订单状态机可用 |
| `Result`（结果页） | 无 | 用于「操作完成」页 |

## 6. 值得整体跟进的 antd 设计价值观

antd 官方四条设计价值（<https://ant-design.antgroup.com/docs/spec/introduce-cn>）：

1. **确定性（Certain）** —— 同样的操作在任何页面结果一致；用词统一。
   → 本项目对标 `admin-ui-logic` §4「同一概念全站同名」；现状违反：`products` 状态列输出英文
   `published`、同页「影响面」一词两义。
2. **意义感（Meaningful）** —— 每个元素都服务于用户目标，不做装饰性设计。
   → 对标 `admin-ui-logic` §2.1「页面正文零说明文字」。
3. **生长性（Growing）** —— 内容增长时布局不崩。
   → 对标本项目缺的**分页/筛选**（7 页 filterBar:0）、以及 **N 行 = N 份抽屉** 的体积膨胀。
4. **自然（Natural）** —— 遵循用户既有心智，不要求学习。
   → 对标「全站列表页长同一个样子」。

**这四条与本项目的评审判据高度重合** —— 说明 `admin-ui-logic` 的判据方向是对的，
差距在**执行**（哪些页面还没收到）与**控件成熟度**（原生控件 vs 成熟组件）。

## 7. 改造优先级（按「用户受伤程度 ÷ 改造成本」）

| 序 | 项 | 依据 | 成本 |
|---|---|---|---|
| 1 | 字段级错误提示（替代页顶笼统错误） | §2.2 | 中（要改 handler 回带 + 模板） |
| 2 | 日期范围控件（一个控件替代两个 date） | §3 | 低（一个 JS 组件 + CSS） |
| 3 | 可搜索下拉（>10 项枚举） | §3 | 低（扩展现有 `WBUI.select`） |
| 4 | 表格空态保留表头 | §1 | 低（改模板分支） |
| 5 | 抽屉头部固定提交按钮（长表单） | §2.3 | 低 |
| 6 | HTMX 请求期间按钮 loading | §4 | 低（推广 `WBUI.busy`） |
| 7 | 树选择（分类 / 部门 / 菜单父级） | §3 | 中 |
| 8 | Tabs 收口多表页面 | §5 | 中 |
| 9 | 表格排序 + 固定列宽 | §1 | 中（要服务端支持） |
| 10 | Steps 用于流程页 | §5 | 中 |

## 8. 改造时必须守的本项目约束

- **不引入 React 或任何前端框架运行时** —— 本项目是服务端渲染 + HTMX；新控件写在
  `static/js/ui/` 基座里（见 `AGENTS.md`「交互方式」）。
- **控件必须渐进增强**：原生控件留在 DOM 里（视觉隐藏），表单提交、`name/value`、
  `label[for]` 全部照旧（这是 `WBUI.select` 的既有契约，新控件照此）。
- **必须多端适配**：桌面 / 平板 / 手机 × 鼠标 / 触屏 / 键盘
  （见 `AGENTS.md`「所有组件必须适配多端」；`AddHover` 规则要包在 `@media (hover: hover)`）。
- **颜色走 `--sky-c-*`**，禁止写死十六进制（深色主题下会退化）。
- **HTMX 局部替换后必须能初始化新控件** —— 挂进 `WBUI.controls`，由 `WBUI.scan` 统一扫描；
  不得因「初始化时没找到元素」提前 return（字段常随抽屉才进 DOM）。
