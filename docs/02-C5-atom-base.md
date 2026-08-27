# 02-C5 · 泛型组件基座规范 (Atom Base Spec)

本文档规范 `core.Atom` 泛型基座——go_wp 版的 Widget_Base，目标：**写一个新组件的成本对齐 WordPress**（Props + 渲染函数即完成），公共样板零重复。

## 1. 问题与方案

02-C0~C4 之前，每个原子组件手写一遍公共管线（约 60~80 行/组件）：

节点 ID 校验与查重、叶子约束、props JSON 解码、声明式字段校验调用、Advanced 校验与 CSS 编译、class 合并与自定义 ID 织入——六件事每个组件完全相同。

Go 泛型可以把它们吸收进基座：

```go
type Atom[P any] struct{ Spec AtomSpec[P] }   // 实现 core.Component 接口
```

## 2. 组件开发形态（对齐 WP 三件套）

写一个新组件 = 一个文件三段式，典型 50~80 行（见 `components/spacer/spacer.go` 实例）：

```go
// 1. Props 结构体：ct tag 声明控件（自动校验 + 面板 schema）
type Props struct {
    Height   Height             `json:"height,omitempty"`
    Advanced core.AdvancedProps `json:"advanced"`   // 基座约定字段，通用层白拿
}

// 2. 渲染函数：只写组件本体（class/id/Advanced CSS 已由基座处理好，经 h 传入）
func render(node *core.Node, p *Props, h *core.AtomRender) (string, error) {
    // h.Classes / h.CustomID / h.CSS
}

// 3. 基座装配 + 注册
var Widget = core.Atom[Props]{Spec: core.AtomSpec[Props]{
    TypeName: "core.spacer",
    ValidateExtra: func(p *Props, nodeID string) error { ... }, // 可选：关系性校验
    Render: render,
}}
func init() { core.Register(Widget) }
```

## 3. 基座职责边界

| 基座负责（组件作者免费获得） | 组件作者负责 |
|---|---|
| 节点 ID 白名单 + 全文档查重 | Props 定义（ct tag） |
| 叶子约束（原子组件无子节点） | Render 渲染函数（结构 + 自身 CSS） |
| props JSON 解码 | ValidateExtra（互斥/依赖等关系性规则，可选） |
| 声明式字段校验（ValidateSpec） | — |
| Advanced 校验 + CSS 编译 + class/id 织入 | — |

## 4. 对比

| | WP widget | 02-C3 手写组件 | Atom 基座组件 |
|---|---|---|---|
| 样板 | ~0（基类吸收） | ~80 行/组件 | ~0（基座吸收） |
| 安全校验 | 作者自觉 | 框架强制 | 框架强制（同左） |
| 面板 schema | 自动 | 自动 | 自动 |
| 渲染时机 | 运行时（每次请求） | 编译期（确定性） | 编译期（确定性） |

## 5. 实现映射

| 条目 | 实现 |
|---|---|
| 泛型基座 | `core/atom.go`（Atom[P] / AtomSpec[P] / AtomRender） |
| Advanced 反射接入 | `core.AdvancedOf[P]`（约定字段名 Advanced） |
| 节点 ID 公共校验 | `core.ValidateNodeID`（基座与容器共用） |
| 首个基座组件 | `components/spacer`（间隔，~75 行含注释） |
| 单元测试 | `public/test/builder/unit/spacer_test.go`（编译/校验管线/确定性） |

存量组件已全部迁移到基座（`core.heading` / `core.text` / `core.image`）：
基座吸收 ID 校验/叶子约束/解码/声明式校验/Advanced/class 织入；组件仅保留关系性校验
（ValidateExtra：文本二选一、绑定路径、模式限制、排版组校验）与业务渲染。

容器 `core.container` 为组件树唯一结构载体（递归子节点 + 布局引擎），不迁移到原子基座，
保持独立实现（docs/02-A）。