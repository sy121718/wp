# 02-G · 组件库整合（纯 Go，不引入 Node）

> 状态：**方案**（2026-09）。目标是把「一个组件散在 4 处」收敛为「一个组件一个自包含目录」。
> 不引入 Node / 打包器：现有构建链一个都不动。

## 1. 问题：一个组件散在四处

以 `form` 组件为例：

```
internal/builder/components/form/form.go              props + 样式（Go 字符串数组）
internal/templates/components/form.jet                模板（另一个顶层目录）
internal/templates/static/js/enhance.js               行为（538 行，所有组件挤一个文件）
internal/builder/core/effects.go + keyframes_animate.go  动效（495 行，全局词汇表）
```

改一个组件要开 3~4 个文件、跨 3 个顶层目录。实测规模：

| 项 | 数量 | 位置 |
|---|---|---|
| 组件目录 | 37 | `internal/builder/components/<name>/` |
| 组件模板 | 38 | `internal/templates/components/*.jet`（`//go:embed components/*.jet`） |
| 组件样式 | **425 处 `b.Add(...)`** | 散在各组件 `.go` 里，是 Go 字符串数组 |
| 组件行为 | **538 行** | `enhance.js` 一个 IIFE，按 `data-*` 分块 |
| 动效 | **495 行** | `core/effects.go` + `keyframes_animate.go` |

写样式没有补全、没有 lint、不能预览 —— 这是「分散写太麻烦」的实际来源。

## 2. 目标形态：一个组件 = 一个自包含目录

```
internal/builder/components/form/
  form.go        props 定义 + Register（不变）
  form.jet       模板（就近，//go:embed form.jet）
  form.css       样式（真 CSS：有补全 / lint / 格式化）
  form.js        行为（可选；构建期按特征内联）
  form_test.go   测试
```

改一个组件只进一个目录。**仍然没有任何 Node 参与**。

## 3. 四个机制（都要保留现有约束）

### 3.1 模板就近

现在：`internal/templates/components_embed.go` 用 `//go:embed components/*.jet` 嵌一个全局 FS。
改为：每个组件包自己 `//go:embed form.jet`，注册时把 FS 交给注册表；注册表**兼容两处查找**，
存量组件不动也能继续工作。

### 3.2 样式：真 CSS 文件 + 作用域替换

Go 读 `form.css`，把顶层 `&` 替换成该 node 的作用域选择器，写进 `CSSBuckets`。

```css
/* form.css —— 直接写 CSS */
& .sky-form-field { display: grid; gap: 6px; }
& .sky-form-field label { font-size: 13px; }
& .sky-form-submit:hover { filter: brightness(.95); }   /* 自动进 hover 桶 */
```

桶的选择由**选择器特征**推断，不需要额外标注：

| CSS 写法 | 进哪个桶 |
|---|---|
| 普通规则 | `BreakpointDesktop` |
| `&:hover` / `&:focus-visible` | `hover`（构建期包 `@media (hover: hover)`） |
| `&:active` | `active`（**不**包媒体查询 —— 触屏唯一可靠的按下反馈） |
| `@media (max-width: …)` | `mobile` / `tablet`（按断点归桶） |
| `@keyframes` | `b.AddKeyframes`（同名只输出一次） |

### 3.3 行为：按组件拆，构建期按特征内联

`enhance.js` 的 538 行按 `data-*` 特征拆到各组件目录的 `form.js`。
构建期已有同款机制（`internal/builder/ui_script.go` 的 `uiBlocks`：特征命中才注入），
照搬即可 —— **纯内容页不为没用的增强付流量**这条约束继续保持。

### 3.4 组件清单

`builder.go` 里现在有 37 行 `_ "go_wp/internal/builder/components/xxx"` blank import。
把它收敛为一份**注册表**（组件名 / 目录 / 特征 / props 类型），用来：

- 生成组件目录文档（像组件库的「目录页」）；
- 构建期挑选与版本指纹（`RegistryVersion`）；
- 检查「登记了却没模板 / 没样式」的漂移。

## 4. 必须保留的约束（一条都不能破）

| 约束 | 为什么不能破 |
|---|---|
| **访问面零运行时依赖** | 静态产物不执行模板、不查库；组件库只能是**编译期**的 |
| **确定性构建** | 同输入同字节；解析器不能引入顺序不确定性 |
| **按 node id 作用域** | 同一组件在页面上多实例不能互相污染 |
| **样式白名单安全模型** | 检查器不放开任意 `animation-*` 字符串；**动效词汇表白名单必须留在 Go** |
| **两个投递目标分离** | 后台常驻加载、产物按需注入；**不能合成同一份 CSS 产物** |
| **无 JS 降级** | HTML 初始态必须可见，JS 只做增强 |

## 5. 分期

| 阶段 | 内容 | 风险 |
|---|---|---|
| **P1** | 基础设施：组件目录可放 `.jet` / `.css` / `.js`，加载机制兼容两处查找 | 中（动加载链，但可增量） |
| **P2** | **试点 `form`**（最小，12 处 `b.Add`）：模板就近 + CSS 迁出，跑通全链路 | 低（单组件） |
| P3 | 动效 `keyframes` 迁 CSS 文件；**词汇表白名单留在 Go** | 中 |
| P4 | `enhance.js` 按组件拆，构建期按特征内联 | 中 |
| P5 | 组件清单 + 文档自动生成 | 低 |
| P6 | 令牌统一（`--c-*` 与 `--sky-c-*`）+ 原子层共享（后台控件与组件共用基础控件 CSS） | 低 |

**每阶段的验收**：全量 `go test ./... -count=1` 通过 + 浏览器比对产物 CSS 与运行时行为**不变** +
中文提交。P2 之后停下来复盘一次，再决定是否推全量。

## 6. 风险

1. **解析器是新的失败面**：`&` 替换、桶推断、`@keyframes` 提取都要稳。对策：先只支持
   现有 425 处实际用到的写法（列举出来做成白名单），遇到未支持的写法**构建期直接报错**而不是静默漏掉；
2. **模板加载改动影响全部 37 个组件**：对策是 P1 保持向后兼容（两处都能找），存量不动；
3. **样式迁移期间两套并存**：与控件迁移同一个教训 —— 分批、每批验证、不要一次全改；
4. **动效最容易迁错**：`effects.go` 的词汇表不只是 CSS，还承担白名单与无障碍（`prefers-reduced-motion`）
   职责，只迁 keyframes、不迁判定逻辑。

## 7. 明确不做

- ❌ 引入 Node / bundler / 打包器（现在没有，也别加）；
- ❌ 运行时组件框架（React/Vue 等）—— 与「访问面不执行模板」直接冲突；
- ❌ 把后台与产物的 CSS 合成同一份产物 —— 两个投递目标必须分离；
- ❌ 一次性迁移全部 37 个组件。
