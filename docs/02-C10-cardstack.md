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
| `spreadAngle` | 5 | hover + fan（每张卡的角度增量，总角 = (张数−1)×增量） |
| `spreadDistance` | 120 | hover（每张卡的位移增量，同时是「铺满」开关） |
| `dragRadius` | 0 = 自动 | drag（0 时按「相邻卡片不重叠」自动算） |
| `deckOffset` / `deckRotate` / `deckScaleStep` | 54 / 4 / 6 | deck |
| `deckLoop` | 关 | deck（滑到头绕回另一端） |

### 布局 / 样式

| 参数 | 缺省 | 说明 |
|---|---|---|
| `spacing` | 26vh | scroll 模式的卡片间距 |
| `stickyTop` | 50% | scroll 模式的粘住位置 |
| `slideHeight` | 100dvh | slide 模式的每屏高度 |
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

品牌故事页 / 落地页
  trigger=slide + 子节点模板（每屏一个 container，一屏一件事）

手工卡片墙
  trigger=hover + shape=fan + 拖 9 张内容卡
```

## 6. 边界与注意事项

- **一屏内容放得下**：`slide` 用 `min-height`，内容超一屏卡片会自己长高、吸附对不齐 ——
  该拆成两张卡，不是调组件参数；
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
