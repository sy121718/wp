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

### 3.1 模板就近（已完成：37 / 37）

组件包自己 `//go:embed <模板名>.jet`，经 `core.RegisterTemplate(name, source)` 登记。加载器
**兼容两处查找**（组件目录优先，`internal/templates/components/*.jet` 兜底）—— 逐批搬迁期间
靠它不断档，现在 `internal/templates/components/` 只剩一个不属于任何组件的共享模板
`_placeholder.jet` 走兜底路径。

三个组件的模板名与目录名不同：`productcard` → `product_card.jet`、`productlist` →
`product_list.jet`、`productselector` → `product_selector.jet`，注册名以**模板名**为准。

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
| `@hovernone & { … }` | `hover`（包 `@media (hover: none)`）—— 触屏没有悬停，任何依赖 `:hover` 的形态都必须在这里给等价形态 |
| `@active & { … }` | `active`（**不**包媒体查询 —— 触屏唯一可靠的按下反馈） |
| `@container (width >= 480px) & { … }` | 容器尺寸查询（`@layer sky-auto`）。条件写成 `(…)`、可含空格；选择器仍必须带 `&` |
| `@style <容器> <属性> <值> & { … }` | 容器样式查询（`@layer sky-local`）—— 作者 / 容器显式声明的语义开关 |
| `@theme <容器> <属性> <值> & { … }` | 容器样式查询（`@layer sky-theme`）—— 主题档位 |
| `@media (max-width: 1024px / 767px)` | `tablet` / `mobile`（只认这两档，自造断点构建期报错） |
| `@keyframes …` | `AddKeyframes`（同名只输出一次）。**名字与帧体都支持变量** —— 每实例一份帧名的组件（`marquee`）靠它，`{{id}}` 没展开就会多出一个谁都不引用的关键帧 |
| `@property <名> { … }` | `AddPropertyDecls` → **未分层顶层桶**（注册是全局的，放进 `@layer` 会让浏览器对「层内注册」产生实现差异）；块内每行一条声明，与 `@keyframes` 同约定 |
| `@global <sel> { … }` | **跨实例共享**的全局规则（如灯箱浮层：同页多个图片共用一份，重复登记由 `CSSBuckets` 去重）。不做 `&` 替换，但**仍然展开变量**；不含 `&` 又不带此标记的选择器一律拒绝 —— 漏写 `&` 会让样式静默泄漏到全站 |
| `@focus-ring;` / `@focus-transition;` | 展开成 Go 侧的计算声明（效果基本库保持一份实现） |
| `@need-keyframes <name>;` | 登记「本组件用到某个内建关键帧」，不产出声明。名字支持变量（效果名常由 props 算出）；**动效词汇表白名单留在 Go** —— 校验在那边做，这里只负责登记 |

**属性驱动的样式走值变量**：把「值从哪来」留在 Go、「属性怎么组合」写进 CSS。

```css
& .sky-progress-bar {
  width: {{width}};        /* Go 侧按 value/max 算出的百分比 */
  background: {{color}};   /* 色值或主题 Token */
}
```

配套规则：**任一变量为空 → 整条声明省略**。这一条吸收了迁移前 Go 里所有的
`if p.X != "" { decls = append(...) }` —— 属性没设，声明自然不产出。

变量的值还可以是**多条声明**（Go 侧用 `"; "` 连接）：排版组三端、描边库、块级对齐的
`margin` 对天生是「一组声明」，展开后按分号拆开逐条收录，顺序保持。
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

**规则级条件块**（`@if` 独占一行、位于规则之外）包住整条规则或指令，用来让一整段结构随变量
存废 —— `@keyframes` 不是声明，声明级条件段包不住它（shapedivider 的漂移帧就是这种情形）：

```css
@if drift
@keyframes sky-sd-drift {
  from { transform: translateX(0) }
  to { transform: translateX(-60px) }
}
@endif
```

两处 `@if` 同名但作用域不同：规则级在规则之外、声明级在规则块内，互不干扰。
真值：空串 / `0` / `false` / `no` / `off` 为假（大小写不敏感）。

**未命中的分支也要解析一遍**，只是把输出丢进一个用完即弃的临时桶：Go 侧总是提供全部业务
变量（它不该跟着样式源的分支结构走），若未命中就完全不解析，分支内的变量不会被标记为
「已消费」，反向校验会把它们误判成拼写错误。顺带让未命中分支里的语法错误也能在构建期暴露。

**数量随数据变化的规则走 `@each`**：列表由 Go 侧提供，块内用 `<循环变量>.<字段>` 取值。
值变量表达不了这类规则 —— 变量表是扁平的，而这里每条规则要取自己那一项的值：

```css
@each tab in tabs
&:has({{tab.radio}}:checked) .sky-tab-panel[data-index="{{tab.index}}"] {
  display: block;
}
@endfor
```

Go 侧经 `ApplyComponentCSSTmplLists` 传 `map[string][]map[string]string`。列表名与值变量
共用同一套反向校验（提供了没用到、引用了没提供都报错）；块内字段拼错会在**第一次循环**
就失败，不会静默少一条规则。`@if` 与 `@each` 的块深度**一起计** —— 两种块互相嵌套时
只数自己那一种，内层的结束标记会被当成外层的，块被提前截断，后半段规则凭空消失。

选择器里的变量同样会展开（`@each` 的循环项最常用在选择器里）。

**空列表的 `@each` 也会把内部试解析一遍**（输出丢弃）：理由与未命中的 `@if` 相同 ——
否则循环体内的变量不算「已消费」，数据为空时反向校验就会误报。此时循环项没有真实取值，
按占位处理。规则块内的注释支持跨行、也允许出现分号（先整块清注释再按分号切声明）。

最后三条容器类指令的层序 `sky-auto < sky-theme < sky-local` 就是优先级：自动适配要被主题档位
盖住，主题档位又要被作者显式声明盖住。层归属写错的表现是「主题调了没反应」—— 产物是一份
合法 CSS，浏览器不报任何错，所以 `card` 的契约测试专门钉住它。

这三条**不能写在 `@media` 块里**：容器查询与视口断点是两套正交档位，混在一处时「谁先赢」
取决于源顺序，属于会随编辑漂移的隐式规则，解析器直接报错。

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

**存量迁移进度（P7）**：样式源 **36 / 37**（余下的 `globalref` 的 `CompileCSS` 是空实现，
不参与迁移）、模板就近 **37 / 37**。至此「一个组件一个自包含目录」在样式与模板两类资产上
都已落地：每个组件目录里是 `<name>.go` / `<name>.jet` / `<name>.css`（`globalref` 无样式），
行为块（`enhance*.js`）也在其中。


| 批次 | 组件 |
|---|---|
| P2 试点 | `form` |
| 原子批 | `rating` `badge` `progress` `spacer` `divider` |
| 布局文本批 | `text` `quote` `image` `list` |
| 标题批 | `heading` |
| 媒体小组件批 | `icon` `video` `countdown` `languages` |
| 问答与形状批 | `faq` `shapedivider` |
| 卡片与图集批 | `card` `gallery` `productcard` |
| 表格与计数批 | `table` `counter` `marquee` |
| 轮播与折叠批 | `slider` `accordion` |
| 页签与社交批 | `tabs` `socialbuttons` |
| 信息框与导航批 | `infobox` `nav` |
| 加载器批 | `loader` |
| 收尾批 | `button` `cardstack` `container` `product` `productlist` `productselector` |

每批做法固定：先 dump 迁移前后产物要求**逐字节一致**（有 golden 的组件另由
`TestJetViewByteEquivalent` 整页字节网兜底），再补该组件的 `xxx_css_test.go` 契约测试。

**解析器的能力几乎全部是迁移过程中被真实组件逼出来的**，而不是事先设计好的：

| 能力 | 谁逼出来的 | 不说会怎样 |
|---|---|---|
| 值变量 / 声明级条件段 | `badge` `divider` | 可选属性要在 Go 里写 if 拼切片 |
| 规则级条件块 | `shapedivider` | `@keyframes` 不是声明，声明级条件段包不住它 |
| `@global` | `image` | 灯箱浮层每实例产出一份 |
| `@need-keyframes` | `heading` | 动效词汇表白名单被迫挪出 Go |
| 容器类四条桶 | `card` `gallery` | 主题档位与容器查询无从表达 |
| `@property` | `counter` | 零 JS 计数的注册块没有归宿 |
| 关键帧名与帧体的变量 | `marquee` | 帧名留着 `{{id}}`，动画不动 |
| 规则级 `@if` 未命中时也试解析 | `gallery` | 分支一多就误报「变量提供了没用到」 |
| `@each` 列表变量 | `tabs` `socialbuttons` | 「一个 switch 展开成 N 条规则」只能留在 Go |
| 空列表的 `@each` 也试解析 | `container` | 数据为空时才出现的构建失败 |
| 块内注释支持跨行与分号 | `cardstack` | 注释里的分号把一行劈成两半而报错 |

最后一行那两处（`container` 的空轮播、`cardstack` 的效果名）都是「**只在某种数据下才出现**」
的构建失败：前者只在幻灯片为空时触发，后者只在动效名由 props 算出时才显现。
这类缺陷的共同点是回归测试很容易漏 —— 它们的契约测试都是事后补的。

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
