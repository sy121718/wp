# 组件渲染 Jet 模板化迁移计划

> 目标：把 `internal/builder/components/*` 组件渲染中的 Go 字符串拼接 HTML，
> 迁移为「每组件一个 Jet 模板（HTML 进 .jet 文件，IDE 可校验）+ Go 负责 props 解码 / CSS 生成 / 校验」。
> 理由：裸字符串拼接不利于开源维护、IDE 无法校验 HTML、转义靠手写易漏。
> 同时本计划对齐 CLAUDE.md 既有约定——「构建期模板 Jet v6：发布阶段把受限 Fragment 与
> BuildContext 渲染为最终 HTML」，组件渲染本就该用 Jet，当前 Go 拼字符串是实现偏离。

> **✅ 已完成（2025-09）**：18 个组件已全部 Jet 化——每个组件目录均有配套 `jet.go`（BuildView / View 导出），
> `internal/templates/components/*.jet` 每组件一个模板（另有 `_placeholder.jet`），`builder.Compile` / `RenderDocument`
> 经 `nodeViewOf` + Jet 渲染（`builder/jetview.go`），旧 render 字符串拼接死代码已删除，转义交 Jet 默认 HTML 转义
> （`unsafe` 仅用于 sanitize 后富文本与 Go 拼好的安全属性串），`go test` 全绿。
> 以下迁移计划内容保留作为历史记录。

## 零、Jet v6 能力结论（技术依据，已实测源码 v6.3.2）

| 能力 | 结论 | 证据 |
|---|---|---|
| include 是否运行时动态 | **是**，模板名运行时求值，可为变量/字符串 | `eval.go:executeInclude` 用 `evalPrimaryExpressionGroup` 求值模板名 |
| 是否支持递归 include | **支持**，自引用模板可递归 | `eval_test.go:844` 有 `recursive_incl_1 → recursive_incl_2 → recursive_incl_1` 自引用测试 |
| 递归深度保护 | 100000 上限 | `eval.go:605` `includeDepth >= 100_000` 报错 |
| include 上下文传递 | **支持**，`{{ include "child.jet" .Value }}` 后子模板 `.` 即传入值 | `eval.go` 处理 `node.Context` 并切换 `st.context` |
| 模板继承/组合 | `extends` / `block` / `import` / `include` | README 特性清单 |
| 自动转义 | `{{ .Text }}` 默认 HTML 转义，`unsafe` 输出原始 | README「auto-escaping」 |

**结论**：组件树的递归渲染可以**完全在 Jet 模板内完成**，不需要 Go 预渲染 children 字符串。

## 一、现状盘点（检测数据）

| 项 | 数值 |
|---|---|
| 组件文件 | 18 个 |
| `WriteString` 拼接点 | ≈372 处 |
| 手写 `html.EscapeString` | ≈47 处（漏一处即 XSS 风险） |
| 结构型组件（递归 children） | 6 个：container / slider / marquee / tabs / accordion / globalref |
| 富文本场景 | text（sanitizeRichHTML + 原样输出） |
| 特殊分支（iframe/按钮化/占位） | video（iframe↔video）、infobox（按钮化提前 return）、globalref（占位↔展开） |
| CSS 生成 | 各组件 `compileCSS` + `core/css.go` 序列化（**必须留 Go**） |
| 客户端脚本 | `builder/enhance.js`（144 行，保留） |
| 页面骨架 | `builder.RenderDocument`（20 处拼接，可选迁移） |

**结论**：迁移范围 = 组件 render 的 HTML 输出段；CSS 生成、校验、增强脚本**不迁**。

## 二、迁移架构

### 组件 = Jet 模板（HTML + 递归）+ Go（props 解码 + CSS 生成 + 校验）

```
render(node) 流程（改造后）：
  1. Go 解码 props → 计算 classes / 布尔量（IsEmbed、HasBtn 等）→ 组装节点视图
     nodeView{ Type, Template, Classes, Props..., Children:[nodeView] }
  2. Go 递归把整棵 Node 树转成 nodeView 树（每节点带 Template 名）
  3. set.GetTemplate("components/node.jet").Execute(buf, nil, rootNodeView)
     —— 模板内部 {{ range _, child := .Children }}{{ include child.Template child }}{{ end }} 递归
  4. Go 遍历树，对每节点调用 compileCSS(node.ID, props, ctx.CSS)  —— CSS 与 HTML 分离
```

**关键点**：递归由 **Jet include 完成**（模板内递归），Go 只做「解码 props → 算模板名 → 组 view 树」。这样：
- 每个组件一个独立 `.jet` 模板（单一职责、好开源阅读）
- 递归不写死在 Go，模板结构自然表达「容器 → children → 再递归」

### 模板文件结构

叶子组件（heading / image / button / video / list / infobox / social / counter / spacer / divider）：

```html
{# components/heading.jet #}
<{{ .Tag }} class="{{ .Classes }}"{{ if .CustomID }} id="{{ .CustomID }}"{{ end }}>
  {{ if .HighlightColor }}<span class="wp-heading-highlight">{{ .Text }}</span>{{ else }}{{ .Text }}{{ end }}
</{{ .Tag }}>
```

结构型组件（container / slider / tabs / accordion / marquee）——模板内递归 include：

```html
{# components/container.jet #}
<{{ .Tag }} class="{{ .Classes }}"{{ .Attrs | unsafe }}>
  {{ range _, child := .Children }}
    {{ include child.Template child }}  {* 运行时动态模板名 + 递归 *}
  {{ end }}
</{{ .Tag }}>
```

globalref——占位/展开分支，展开时模板内递归引用块 root：

```html
{# components/globalref.jet #}
{{ if .Resolved }}
  {{ range _, child := .ResolvedChildren }}
    {{ include child.Template child }}
  {{ end }}
{{ else }}
<div class="{{ .Classes }} wp-globalref-missing"><span>{{ .PlaceholderText }}</span></div>
{{ end }}
```

### 转义策略（核心收益）

- 普通文本 `{{ .Text }}` → Jet **默认 HTML 转义**（替代手写 EscapeString，消除 47 处漏转义风险）
- 富文本/已清洗 HTML → `{{ unsafe(.SanitizedContent) }}`（text 组件 sanitize 后原样输出）
- 动态 attrs → `{{ .Attrs | unsafe }}`（Go 拼好的安全属性串）

### 不变的部分（不迁）

| 模块 | 原因 |
|---|---|
| `compileCSS` 各组件 | CSS 规则按 props 计算 + 断点分桶，是代码不是模板 |
| `core/css.go` 序列化 | CSS 输出，非 HTML |
| `core/atom.go` 的 CSS 部分 | 同上 |
| `enhance.js` | 客户端增强脚本 |
| 校验（Validate） | Go 类型与白名单 |
| nodeView 组装（解码/算模板名） | Go 类型安全 + 复用 core.Lookup 注册表 |

## 三、分阶段实施

### Phase 0：样板验证（等价性地基）✅ 已完成
1. 建 `internal/templates/components/` 目录 + `node.jet` 递归入口 + 各组件模板
2. 建 `nodeView` 结构 + `nodeViewOf(node, ctx)` 转换器（复用 core.Lookup 派发模板名）
3. 组件 Jet 模板加载器挂进构建路径（**非 dev 模式**缓存模板）
4. 选 **button（叶子）+ container（结构型，含递归）** 两个样板迁移
5. **字节等价测试**：同一 Document，旧 render 输出 vs 新 Jet 输出，断言完全一致
6. 验证：确定性不破坏、IDE 高亮、递归深度正常、转义正确

- [x] **验收（已完成）**：button + container 新旧输出字节相同，递归 children 正确展开，`go test` 全过。

### Phase 1：叶子组件（无递归，最简单）✅ 已完成
heading / text（含富文本 unsafe）/ image / divider / spacer / list / infobox / socialbuttons / video / counter

- 每个组件：写 `.jet` 模板 + nodeView 挂接 + render 改「组装 view + compileCSS」
- text 富文本：模板 `{{ unsafe(.SanitizedContent) }}`，sanitize 仍在 Go
- video：模板 `{{ if .IsEmbed }}iframe{{ else }}video{{ end }}`，Go 只算 IsEmbed
- infobox：按钮化提前 return 改为模板 if/else 嵌套

- [x] **验收（已完成）**：10 个叶子组件迁移完，字节等价测试全过。

### Phase 2：结构型组件（含递归）✅ 已完成
slider / tabs / accordion / marquee / globalref

- 模板内 `{{ range _, child := .Children }}{{ include child.Template child }}{{ end }}` 递归
- marquee：双份内容循环在模板内（两份 track 各 range 一次 children）
- globalref：占位↔展开分支，展开时模板内递归引用块 root（Go 预解析块 root → nodeView）

- [x] **验收（已完成）**：6 个结构型组件迁移完，递归等价测试全过。

### Phase 3：收尾 ✅ 已完成
1. 删除组件 render 里残留的 WriteString/EscapeString（转义全交 Jet）
2. 可选：`builder.RenderDocument` 页面骨架也模板化（`page.jet`）
3. 全量 `go test -count=1` + 现有页面构建冒烟（字节与迁移前一致）
4. 文档更新（CLAUDE.md 组件渲染说明）

## 四、每个组件的特殊场景清单

| 组件 | 特殊场景 | 处理 |
|---|---|---|
| text | 富文本 sanitize + 原样输出 | Go sanitize，模板 `unsafe` |
| image | Atom 基座 + `media.RenderImageHTML` 返回整段 | 改用模板（HTML 段进 image.jet，variant 解析留 Go） |
| video | iframe ↔ video 双形态 | 模板 if/else，Go 算 IsEmbed |
| infobox | 按钮化提前 return | 模板嵌套 if/else |
| globalref | 占位 ↔ 递归展开 | Go 预解析块 root → view，模板 if/else + 递归 include |
| slider | data-* 属性 + 轨道/箭头/圆点外壳 | 属性进模板，children 递归 include |
| marquee | 双份内容无缝滚动 | 模板内两份 track 各 range children |
| container | attrs 动态拼接（drawer/group/自定义属性） | Go 拼好 attrs 字符串，模板 `unsafe` 输出 |

## 五、风险与回滚

| 风险 | 应对 |
|---|---|
| 字节不一致（确定性破坏） | 每组件迁移时做字节等价测试，等价才提交 |
| Jet 递归失控（深度/死循环） | 天然有 100000 深度保护；view 树先转好再执行，不产生自引用 |
| Jet 开发模式误入构建路径 | 构建路径强制非 dev 模式（缓存模板），加测试断言 |
| 转义语义差异（unsafe 误用） | 仅 sanitize 后内容 + 动态 attrs 用 unsafe，其余走默认转义；code review 把关 |
| include 动态模板名未命中 | nodeView 组装时用 core.Lookup 保证模板名合法，加 fallback 占位 |
| 迁移量大（18 组件） | 分 Phase 逐批提交，每批独立可回滚（git） |

## 六、验收标准 ✅ 已完成（当前代码满足以下全部验收项）

- [x] 1. `go test -count=1` 全过（含新增字节等价测试）
- [x] 2. 现有已发布页面构建产物字节与迁移前**完全一致**（确定性不破坏）
- [x] 3. 组件 HTML 全部在 `.jet` 文件，IDE 可高亮/校验
- [x] 4. 手写 `EscapeString` 清零（转义全由 Jet 承担）
- [x] 5. 递归 children 由 Jet include 完成（结构型组件模板内可见）
- [x] 6. 构建性能无感知变化（非 dev 模式缓存模板）

## 七、工作量估算

| Phase | 组件数 | 估时 |
|---|---|---|
| 0 样板 | 2 | 半日 |
| 1 叶子 | 10 | 1~1.5 日 |
| 2 结构型 | 6 | 1 日 |
| 3 收尾 | — | 半日 |
| **合计** | 18 | **约 3 日** |

## 八、待补充（下一步规划）

- Go 侧库位面规划（哪些 builder/core 能力可抽成独立库、依赖边界），待单独评估
- Jet 模板的 IDE 高亮/校验插件选型（VS Code Jet 扩展 或 按 HTML 关联）
