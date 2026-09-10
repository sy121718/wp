# 02-C10 · core.cardstack（卡片堆叠）

> 速记版：**选哪个模式 + 参数怎么填**。实现原理（几何推导、收敛公式、降级策略、
> 零 JS 取舍）见 `docs/02-F-motion-animation.md` §4.6，本文不重复。

## 1. 一句话

**卡片 = 容器 + 数据槽位**：卡内可以放任意组件（跟 `core.container` 一样），
差别只在内容来源 —— 容器要手工摆，卡片能从内容集合自动出 N 张。

## 2. 选型：先挑 trigger

| 想要的效果 | `trigger` | 配套参数 | 卡片数量由 |
|---|---|---|---|
| 一摞卡叠着，悬停散开 | `hover`（缺省） | `shape`/`direction`/`spreadAngle`/`spreadDistance` | 内容决定 |
| 一屏内的小卡，滚动时逐张叠住 | `scroll` | `spacing`/`stickyTop` | 内容决定 |
| 卡片成环，拖着转 360° | `drag` | `dragRadius` | 内容决定 |
| 一屏一张，翻页看 | `slide` | `slideHeight` | 内容决定 |
| 主卡居中，两侧叠开，滑动切换 | `deck` | `deckOffset`/`deckRotate`/`deckScaleStep`/`deckLoop` | 内容决定 |

`hover` 再挑两个轴：

```text
shape: fan  弧线扇形（卡片带角度，展示效果好、文字略歪）
shape: line 直接排开（**卡片不带任何角度**）
       └ direction: horizontal 横排一行 / vertical 竖排一列
```

> 信息类卡片（商品、文章）优先 `line` 或 `deck`：扇形展开后文字是斜的，不好读。

### `slide` 与 `deck(vertical)` 别混（最容易搞错的一对）

| | `slide` | `deck` + `deckDirection=vertical` |
|---|---|---|
| 卡片排布 | **纵向平铺**，每张一屏高 | **叠在中心**，只露边 |
| 切换方式 | 滚动吸附（滚轮 / PageDown） | 上下拖动 / ↑↓ 键 / 点侧卡 |
| 首屏看到 | 第一张（其余在下面） | 第一张（其余压在下面） |
| 适合 | 一页一屏的落地页、故事页 | 卡片式翻牌、选项切换 |

看到「几屏内容纵向排开」是 `slide` 的**正常形态**，不是堆叠失效；要"叠着"用 `deck`。

## 3. 内容来源：三种，按需求挑

| 来源 | 怎么触发 | 适合 |
|---|---|---|
| **静态卡** | 拖子节点进去，每个子节点 = 一张卡 | 手工维护的固定几张 |
| **集合 + 字段映射** | 选「内容集合」+ 配 5 个字段，**不拖子节点** | 结构固定的列表（配一行就出卡、自带详情链接） |
| **集合 + 子节点模板** | 选「内容集合」+ **拖子节点**（子节点 = 每张卡的模板） | 结构自定义（图/名/价/描述/按钮随你摆） |

字段映射那一行的 5 个字段留空即不渲染该元素：

```text
图片字段  images / featuredImage / image      数组字段自动取首元素
标题字段  name / title
正文字段  description / excerpt
附注字段  price（一行次要信息，主色加粗）
链接字段  slug（配合「链接前缀」拼成完整地址）
```

子节点模板模式下，子节点里的组件用 **`item.<字段>`** 取当前卡片项
（Inspector 里是下拉，不是输入框；写错字段名构建期会报错并列出可用字段）。

## 4. 参数速查

### 内容（`sec=content`）

| 参数 | 类型 | 缺省 | 说明 |
|---|---|---|---|
| `trigger` | 下拉 | `hover` | 见 §2 |
| `shape` | 下拉 | `fan` | `fan` 弧线 / `line` 直线（仅 hover） |
| `direction` | 下拉 | `horizontal` | 横排 / 竖排（仅 `line`） |
| `count` | 滑块 | 9 | 占位卡数量（有子节点或集合时忽略；`slide` 缺省 3） |
| `width` / `height` | 尺寸 | 按模式 | `slide` 强制占满宽度、高度交给 `slideHeight` |

### 内容集合（`sec=collection`）

| 参数 | 缺省 | 说明 |
|---|---|---|
| `collectionSource` | 不使用 | `content:article` / `content:product` / `content:category` |
| `collectionLimit` | 6 | 取前几条（1~24） |
| `cardImageField` 等 5 项 | 空 | 字段映射，见 §3 |
| `cardLinkPrefix` | 空 | 与链接字段拼成 href，如 `/article/` |
| `cardLinkText` | 查看详情 | 详情链接文案（可配，多语言不用改代码） |
| `collectionEmpty` | 显示占位 | 无内容时：显示占位文案 / 隐藏整个组件 |
| `collectionEmptyText` | 暂无内容 | 占位文案 |

### 运动（`sec=motion`）

| 参数 | 缺省 | 适用 |
|---|---|---|
| `hueStep` | 50 | 全部（数字卡的位置派生色相） |
| `hoverEffect` | 无 | hover：悬停时的循环效果（发光 / 闪烁） |
| `deckHighlight` | 无 | deck：主卡的循环高亮（发光 / 闪烁） |
| `spreadAngle` | 5 | hover + fan（每张卡的角度增量，总角 = (张数−1)×增量） |
| `spreadDistance` | 120 | hover（每张卡的位移增量，同时是「铺满」开关） |
| `dragRadius` | 0 = 自动 | drag（0 时按「相邻卡片不重叠」自动算） |
| `deckDirection` | horizontal | deck：横向切换（左右） / **纵向切换（上下）** |
| `deckOffset` / `deckRotate` / `deckScaleStep` | 54 / 4 / 6 | deck（纵向时偏移缺省 12、倾斜减半） |
| `deckLoop` | 关 | deck（滑到头绕回另一端） |
| `deckTransition` / `deckDuration` | 平滑 / 450ms | deck 的切换曲线（回弹 = `cubic-bezier(.34,1.56,.64,1)`）与时长 |

### 布局 / 样式

| 参数 | 缺省 | 说明 |
|---|---|---|
| `spacing` | 26vh | scroll 模式的卡片间距 |
| `stickyTop` | 50% | scroll 模式的粘住位置 |
| `slideHeight` | 100dvh | slide 模式的每屏高度 |
| `slideFit` | 页面内滚动区 | slide 的贴合方式：`viewport` = 铺满视口（整页分页，不受父容器内边距影响） |
| `slideDirection` | vertical | slide 的滚动方向：纵向 / 横向 |
| `slideStack` | 关 | slide 的**堆叠翻页**：卡片粘在同一位置，下一张滑上来盖住前一张 |
| `slideEffect` | 无 | slide 的**切换动画**：淡入 / 缩放 / 翻转 / 弹入 / 回弹 / 旋转 / 光速 / 滚入 / 弹出（方向随 `slideDirection` 自适应） |
| `slideHighlight` | 无 | slide 的**当前屏高亮**：滚到视口中段的那一屏发光 / 闪烁（与切换动画并存） |

> `slide` 的右下角自带页码角标（**第几屏 / 共几屏**），由 CSS `counter` + 编译期写入的
> `attr(data-total)` 生成 —— 零 JS、滚动中自动更新。卡片背景设深色时记得文字也要跟浅色
> （默认文字色是深色，深底深字会看不清）。

**`slide` 有四个形态**（方向 × 排布）：

| | 平铺（缺省） | 堆叠（`slideStack`） |
|---|---|---|
| **纵向** | 上滑把前一张**推走**（原有形态） | 前一张**留在原地被盖住** |
| **横向** | 左滑把前一张推走 | 前一张留在原地被盖住 |

堆叠用 `position: sticky` + 递增 `z-index` 实现（后一张盖住前一张）——**零 JS、不依赖
scroll-driven**，老浏览器一样能跑。

`slideEffect` 与 `slideHighlight` **可以同时开** —— 它们并列成两条 `animation`，各自带
自己的 `animation-timeline` / `animation-range`（CSS 多动画语法），互不覆盖，因此不需要
把高亮挪到伪元素上：

```css
animation: sky-flip-in-x linear both, sky-loop-glow 2s ease-in-out infinite;
animation-timeline: view(), view();
animation-range: entry 0% entry 70%, cover 25% cover 75%;
```

两者都 **不新增关键帧**，直接复用通用动效词汇（`internal/builder/core/keyframes_animate.go`
那 33 条 `sky-*`）里适合翻页的 18 条，由 `NeedKeyframes` 标记后构建期统一注入（同名只注入一次、
**按需注入**：横向页面不会带上纵向的变体）。**方向不用选** —— 纵向自动走 `*-up`/`*-left`、
横向自动走 `*-right`/`*-y`。

`deck` 的动画走**过渡曲线**而不是入场动画：deck 的卡片始终在视口内，入场动画会和它的位置
变换抢 `transform`。回弹预设是 `cubic-bezier(.34,1.56,.64,1)`（轻微过冲）。

`hoverEffect` / `deckHighlight` 也**只挑了不抢 `transform` 的两条循环词汇**：`sky-loop-glow`
（`filter: drop-shadow`）与 `sky-loop-flash`（`opacity`）。其余 `sky-loop-*`（swing / wobble /
tada / pulse / bounce / rubber-band / spin）都改 `transform`，挂在卡片上会把展开位移或层级缩放
顶掉 —— 这是这套词汇用在「已在运动的元素」上时的硬约束。
播放时机用 `animation-timeline: view()`：卡片进入滚动容器视口时播；不支持 `view()` 的浏览器
把未知属性丢弃，动画按 0s 播完并由 `fill-mode: both` 停在终态 —— 视觉等于没有动画，位置与内容都正常。横向模式额外需要轨道 `display: flex` + 卡片
`flex: 0 0 100%`：块级卡片默认纵向堆叠，宽度不溢出，滚动条根本出不来。
| `cardLayout` | column | 卡内排列方向 |
| `cardGap` | 10px | 卡内间距 |
| `cardJustify` / `cardAlign` | center | 主轴 / 交叉轴对齐 |
| `cardBackground` | 主题面 | 卡片背景 |
| `cardPadding` / `cardRadius` / `cardBorder` | 24px / 16px / 10px | 内边距 / 圆角 / 边框宽度 |
| `zoom` | 开 | 点击放大到屏幕中央（`slide` 自动关） |

## 5. 常见配方

```text
商品多变体（结构自定义）
  trigger=line + direction=horizontal + 集合=content:product + 子节点模板
  子节点：image(item.images) → heading(item.name) → text(item.price) → text(item.description) → button

商品列表（结构固定）
  trigger=line + direction=horizontal + 集合=content:product + 字段映射
  图片=images 标题=name 附注=price 正文=description

文章列表
  trigger=line + direction=vertical + 集合=content:article + 字段映射
  图片=featuredImage 标题=title 正文=excerpt 链接=slug + 前缀 /article/

产品轮播
  trigger=deck + deckLoop=开 + 集合（或子节点模板）

品牌故事页 / 落地页（整页分页）
  trigger=slide + slideFit=viewport + 子节点模板（每屏一个 container）
  → 容器脱离文档流铺满视口，父容器的内边距不影响它，也不必手动归零

整页分页但保留页面其余内容（首屏分页 + 后面接正文）
  trigger=slide + slideHeight=100dvh + 父容器 padding=0（inline 贴合，留在文档流里）
  —— 这种情况不要用 slideFit=viewport：它会与同级内容重叠

手工卡片墙
  trigger=hover + shape=fan + 拖 9 张内容卡
```

## 6. 边界与注意事项

- **一屏内容放得下**：`slide` 用 `min-height`，内容超一屏卡片会自己长高、吸附对不齐 ——
  该拆成两张卡，不是调组件参数；
- **`slide` 的两种贴合**：`inline` 留在文档流里，父容器内边距会把它框成「页面里的一块
  滚动窗口」（要整页分页就加 `slideFit=viewport`，或手动把父容器内边距归零）；
  `viewport` 脱离文档流铺满视口，**页面上不能有别的同级内容**（会重叠）；
- **扇形展开的文字是斜的**：`fan` 好看但不好读，信息类内容用 `line` / `deck`；
- **卡片外观是组件级**：一套样式出 N 张卡。要每张卡不同外观，用静态卡模式在卡里套一层容器；
- **种子字段**：集合项自带 `id / slug / revision`，链接字段填 `slug` 即可（`items` 这类数组字段自动取首元素）；
- **键盘可达**：`drag` / `deck` 容器可 Tab 聚焦，方向键等价于拖拽；`slide` 用原生滚动，
  PageDown / 空格 / 屏幕阅读器照旧可用；
- **无脚本降级**：`drag` 静态成环、`deck` 按序号摊开、`scroll` 静态层叠、`slide` 原生滚动，
  四种模式都不会因为脚本没加载而白屏。

## 7. 相关

- 实现原理与取舍：`docs/02-F-motion-animation.md` §4.6
- 组件源码：`internal/builder/components/cardstack/`
- 回归测试：`internal/builder/components/cardstack/cardstack_test.go`、
  `public/test/builder/unit/cardstack_collection_test.go`
