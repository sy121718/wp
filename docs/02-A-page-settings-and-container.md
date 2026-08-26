# 02-A · 页面设置与容器规范 (Page Settings & Container Spec)

本文档规范 go_wp 的页面元配置（Page Settings）与标准容器组件（core.container）的设计逻辑与能力描述。

## 1. 架构定位：页面全局与组件树彻底分离

- **页面设置（Page Settings）**：属于页面实例自身的全局环境配置，不是可视化 DOM 节点。由独立的"页面设置面板"维护，在编译期直接作用于 `<head>` 与 `<body>`。
- **标准容器（core.container）**：组件树（AST）的唯一结构载体，既作为页面第一层顶级 Section，也可无限自由嵌套。编译期直出单层原生 HTML 标签，不产生冗余 Wrapper。

## 2. 页面设置能力描述 (Page Settings)

- **页面版心控制**：配置页面内容是"定宽居中"还是"全宽铺满"，定义定宽模式下的最大内容宽度，并设置桌面/平板/手机三端的最小安全左右留白以防小屏贴边。
- **整页基底样式**：配置注入 `<body>` 的背景颜色、整页平铺/固定背景图。
- **SEO 与全局元信息**：定义页面标题（Title）、描述（Meta Description），以及允许注入 `<body>` 的自定义 class（用于整站或局部特殊样式覆盖）。

## 3. 标准容器组件能力描述 (core.container)

### 布局与排版能力

- **原生语义标签**：支持自由指定为 `div`、`section`、`article`、`aside`、`nav`、`header` 或 `footer`，保证 SEO 和可访问性。
- **双排版引擎切换**：
  - **Flexbox 模式**：控制子元素横向或纵向排列、主轴对齐方式（居中/两端对齐等）、交叉轴对齐、是否允许自动换行，以及子元素间距（Gap）。
  - **Grid 栅格模式**：设置栅格列数（如 1~12 列），支持桌面/平板/手机三端响应式降级（如桌面 4 列、平板 2 列、手机 1 列），并控制行列间距。

### 盒模型与外观样式

- **盒模型尺寸**：支持桌面/平板/手机三端独立的内边距（Padding）、外边距（Margin，含 auto 居中），支持设置最大/最小高度，以及内容溢出处理（隐藏/滚动）。
- **视觉装饰**：支持背景设置（纯色/渐变/背景图）、边框（线型/粗细/颜色）、圆角弧度以及阴影投影效果。

### 交互状态与动画

- **滚动吸顶定位**：可开启固定/吸顶定位（Sticky），用于制作随页面滚动的导航栏或侧边栏。
- **悬浮卡片反馈**：当容器用作产品/文章卡片时，支持鼠标悬停（Hover）触发轻微上浮（Translate Y）与阴影加深反馈。
- **入场微动（默认关闭）**：仅支持纯 CSS 实现的轻量入场动效（如淡入、向上滑入），默认关闭以保护首屏性能和避免布局偏移（CLS）。

## 4. 编译与性能约束

- **编译期静态转译**：所有响应式断点、布局、外观及动画参数均由 Go 编译器直接编译为纯净 CSS，浏览器端零 JavaScript 布局计算。
- **单层干净结构**：每个容器节点编译后仅输出一个对应的 HTML 语义标签，坚决杜绝多层嵌套。

## 5. 实现映射（代码位置）

目录组织：**一个组件一个目录**，组件实现 `core.Component` 接口（Type / Validate / Render）并自注册到 Registry，编译器按节点 type 分发。

```text
internal/builder/
├── builder.go                      # 编译入口：Page → CompiledPage（HTML+CSS）、完整文档组装
├── settings.go                     # PageSettings 数据结构、校验与 CSS 编译
├── core/                           # 编译内核
│   ├── component.go                #   Node 结构、Component 接口、Registry、渲染分发
│   └── css.go                      #   CSSBuckets 三端规则收集、断点媒体查询、动效关键帧
└── components/
    └── container/                  # core.container（后续组件各占一目录：heading/、text/…）
        └── container.go
```

| 规范条目 | 实现 |
|---|---|
| Page Settings 数据结构与校验 | `internal/builder/settings.go` |
| core.container 数据结构与校验 | `internal/builder/components/container/container.go` |
| 组件接口与注册（组件扩展点） | `internal/builder/core/component.go` |
| 编译器（HTML + 响应式 CSS 输出） | `internal/builder/builder.go`、`internal/builder/core/css.go` |
| 单元测试 | `public/test/builder/unit/builder_test.go` |

断点约定（desktop-first）：桌面端为默认样式（无媒体查询）；平板 `@media (max-width: 1024px)`；手机 `@media (max-width: 767px)`。

新增组件步骤：在 `components/` 下新建目录，实现 `core.Component` 接口（定义自己的 Props、校验、HTML 渲染与 CSS 编译），包 `init()` 中 `core.Register(...)`，使用方 blank import 即可被编译器识别。