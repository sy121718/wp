# 02-C3 · 声明式组件 Controls 规范 (Component Controls Spec)

本文档规范 go_wp 组件开发中的**声明式控件描述符**体系，对标 WordPress register_controls 的声明式 API，
将组件开发从“手写 Props 解码 + 白名单校验 + 面板 schema”约 150 行样板收敛为“结构体 tag 声明 + 少量关系性校验”。

## 1. 目标

组件作者只需：

1. 定义 Props 结构体（字段加 `ct` tag 声明控件类型）；
2. 仅对**关系性规则**（二选一、互斥、依赖、子节点限制）手写校验；
3. 字段级校验、Inspector 面板 schema、默认值由 core 自动生成。

## 2. 控件类型与 tag 语法

```go
// ct:"kind,opt1,opt2,..."
// kind：string / bool / int / select / safe / text / regex
// 选项：default=...  min=...  max=...  maxlen=...
type Props struct {
    Text    string `json:"text"   ct:"text,maxlen=500"`                    // 富文本/长文本
    Tag     string `json:"tag"    ct:"select,h1,h2,h3,h4,h5,h6,div,span,default=h2"`
    Shadow  string `json:"shadow" ct:"select,subtle,strong"`               // 选项列表
    Color   string `json:"color"  ct:"safe,maxlen=200"`                    // CSS 值白名单
    Clamp   int    `json:"clamp"  ct:"int,min=0,max=6"`                    // 零值=未设置，范围校验
    URL     string `json:"url"    ct:"regex" ctRegex:"^[a-z]{2,4}$"`       // 正则单独 tag（模式可含逗号）
}
```

| 控件 | 校验规则 |
|---|---|
| `string` | maxlen 上限 |
| `bool` | 无值域 |
| `int` | **零值=未设置放行**；非零校验 min/max |
| `select` | 值必须在选项中；default 供渲染层取默认 |
| `safe` | `core.IsSafeCSSValue` 白名单（防 CSS 注入） |
| `text` | 长度上限 maxlen（富文本/长文本，内容语义由组件负责） |
| `regex` | 模式来自独立 `ctRegex` tag（模式含逗号时不走 ct 分片） |

## 3. 核心 API（internal/builder/core/controls.go）

| API | 职责 |
|---|---|
| `ParseControls(props) ([]Control, error)` | 反射扫描 ct tag 生成描述符表（字段声明序，确定性） |
| `ValidateSpec(props, nodeID) error` | 按描述符逐字段校验（错误统一格式：`节点 X: 字段 Y ...`） |
| `SchemaJSON(props) ([]byte, error)` | Inspector 面板 schema（key/kind/default/min/max/maxLen/options） |
| `SpecProvider` 可选接口 | 组件实现 `PropsSpec() any` 后由校验流程自动调用 |

## 4. 组件开发流程（对齐后的样板）

```go
// components/heading/heading.go 的 Validate：
func (Heading) Validate(node *core.Node, ids map[string]bool) (err error) {
    // 1. 节点 ID / 子节点限制（手写，普遍规则）
    // 2. 关系性规则：文本或绑定二选一、Binding 字段路径、Advanced（手写）
    // 3. 字段级：core.ValidateSpec(&p, node.ID)   ← 声明式（tag → 校验）
}
func (Heading) PropsSpec() any { return &Props{} }  // 声明模板
```

渲染层（Render）不变：结构逻辑 + `core.CompileAdvanced`。

## 5. 已接入组件与样板缩减

- `core.heading`：Text/Tag/Weight/LetterSpacing/Transform/Color/LineClamp/TextShadow 共 8 个字段接入声明式，
  手写校验段减少约 60 行（保留内容互斥、Binding 路径、排版三端、Decor、字重 token 等关系性/复合校验）。
- `core.image` / `core.text`：下一步接入（方案一致：字段 tag + ValidateSpec）。

## 6. 实现映射

| 规范条目 | 实现 |
|---|---|
| 控件类型与 tag 语法 | `core/controls.go`（kind 常量 + parseControlTag + ctRegex 独立 tag） |
| 反射校验引擎 | `core.ValidateSpec`（零值放行、select/safe/regex/int 域、错误统一格式） |
| 面板 schema | `core.SchemaJSON`（确定性字段序输出） |
| 组件接入点 | `core.SpecProvider` 可选接口 + 组件 `PropsSpec()` |
| 单元测试 | `public/test/builder/unit/controls_test.go`（解析/校验/schema 确定性 + heading 集成） |

后续组件（button/divider/视频等）按 §4 流程开发，字段级校验不再手写。