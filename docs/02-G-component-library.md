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

### 3.1 模板就近（P1 已完成）

组件包自己 `//go:embed form.jet`，经 `core.RegisterTemplate(name, source)` 登记；加载器
**兼容两处查找**（组件目录优先，`internal/templates/components/*.jet` 兜底），存量组件不动也能继续工作。

影子发现：模板漏注册由 `//go:embed` 编译期兜住（缺文件即 `[setup failed]`），行为漏注册由
`TestComponentAssetsConsistent` 兜住 —— 两条链都不需要额外看护。

### 3.2 样式：真 CSS 文件 + 作用域替换

Go 读组件同目录的 `.css`，把顶层 `&` 替换成该 node 的作用域选择器，交给 `CSSBuckets`。

```css
& .sky-form-field { display: grid; gap: 6px; }
& .sky-form-field label { font-size: 13px; }
@hover & .sky-form-submit { filter: brightness(.95); }   /* 进 hover 桶 */
```

桶的划分走**显式标记**，不做选择器特征推断 —— 推断要在 `:is(...)::before:hover` 这类组合上判对，
而写清楚只多几个字符：

| CSS 写法 | 进哪个桶 |
|---|---|
| `& { … }`（普通规则） | `BreakpointDesktop` |
| `@hover & { … }` | `hover`（构建期包 `@media (hover: hover)`，触屏根本不输出） |
| `@active & { … }` | `active`（**不**包媒体查询 —— 触屏唯一可靠的按下反馈） |
| `@media (max-width: 1024px / 767px)` | `tablet` / `mobile`（只认这两档，自造断点构建期报错） |
| `@keyframes …` | `AddKeyframes`（同名只输出一次） |
| `@focus-ring;` / `@focus-transition;` | 展开成 Go 侧的计算声明（效果基本库保持一份实现） |

**属性驱动的样式走值变量**：把「值从哪来」留在 Go、「属性怎么组合」写进 CSS。

```css
& .sky-progress-bar {
  width: {{width}};        /* Go 侧按 value/max 算出的百分比 */
  background: {{color}};   /* 色值或主题 Token */
}
```

配套规则：**任一变量为空 → 整条声明省略**。这一条吸收了迁移前 Go 里所有的
`if p.X != "" { decls = append(...) }` —— 属性没设，声明自然不产出。
变量**双向严格校验**：样式源引用了未提供的变量、或提供的变量没被样式源消费，都构建期报错
（Go 与 `.css` 各写一半，拼写错只会表现为「样式悄悄少了」）。

**结构性分支走声明块内的条件段**：

```css
& {
  display: inline-flex;
  @if outline
  color: {{color}};
  border: 1px solid {{color}};
  @endif
}
```

条件是「同一选择器在不同模式下是**不同的声明组**」（badge 三种外观、divider 有无嵌入），
值替换表达不了。写在**声明块内**（而非包住整条规则）是关键：命中的分支与同块其余声明合并进
**同一条规则**，于是产物与迁移前「Go 里按条件拼一个切片、只 `Add` 一次」逐字节一致。
真值：空串 / `0` / `false` / `no` / `off` 为假（大小写不敏感）。

解析器**只支持上表列出的写法**，遇到不认识的写法返回 error 而不是静默跳过 ——
静默漏掉的样式在页面上只表现为「有点不对劲」，比构建失败难查得多。

### 3.3 行为：按组件拆，构建期按特征内联（P4 已完成）

`enhance.js` 的 539 行按 `data-*` 特征拆到各组件目录（8 个块：`counter` / `slider` /
`gallery` ×2 / `countdown` / `cardstack` ×3），主文件只剩 `onReady` 框架 30 行。
构建期沿用既有机制（`internal/builder/ui_script.go` 的 `uiBlocks`：特征命中才注入），
**纯内容页不为没用的增强付流量**这条约束继续保持。

拼接点是 `splitEnhance`，它**按行切**（`LastIndex(src[:idx], "\n")`）而不是按命中下标切 ——
后者会让块多缩进一级、或让无块时丢掉缩进，产物字节随之漂移。

### 3.4 组件清单（P5 已完成）

`builder.go` 的 37 行 blank import 仍在，它是组件 `init()` 顺序的唯一来源——
而 `init()` 顺序决定行为块的内联顺序，所以不做「收敛成一张表」的改动。
实际落地的是 **`TestComponentAssetsConsistent`**（漂移检查）与 **`TestComponentManifest`**
（打印清单）：每个 `.jet` 必须在模板注册表里、每个 `enhance*.js` 必须能被某个注册块内容匹配，
任一不满足即测试失败。

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
| **P6** | 产物令牌修复：`ui.css` 里 45 处 `var(--c-*)` 补 fallback（见下） | 低 |
| **P7** | **存量样式迁移**（按批）：原子 → 布局文本 → 交互重组件 | 低（每批独立验证） |

P6 的结论推翻了本文件原先的设想：`--c-*`（后台 `theme.css` 的 22 个，含派生色）与 `--sky-c-*`
（产物由 `ThemeVarsCSS` 按主题生成的 11 个）**不是同一个集合**，改名统一收益低风险高，不做；
「原子层共享」也不做 —— 两个投递目标必须分离。真正修掉的是既有缺陷：`ui.css` 里 45 处
`var(--c-*)` 没有 fallback，而产物只定义 `--sky-c-*`，整条声明失效。

**存量迁移进度（P7）**：已迁 **6 / 37** —— `form`（P2 试点）、`rating` / `badge` / `progress` /
`spacer` / `divider`（原子批）。每批做法固定：先 dump 迁移前后产物要求**逐字节一致**
（有 golden 的组件另由 `TestJetViewByteEquivalent` 整页字节网兜底），再补该组件的
`xxx_css_test.go` 契约测试。剩余 31 个组件、约 400 处 `b.Add`。

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
