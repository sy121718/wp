# 02-C6 · 图片与矢量图组件实现说明

> 基于用户规范《02-C3 · 图片与矢量图组件规范》的实现映射与设计决策。
> 用户文档编号 02-C3 与既有《02-C3 组件 Controls 规范》重号，本文件按实现批次编号为 02-C6。

## 1. 规范覆盖清单

| 规范条目 | 实现 |
|---|---|
| 媒体库绑定 assetId | 保留（MediaResolver 构建期解析，02-B 体系） |
| 外部绝对 URL | `src` 字段（`url` 控件协议白名单） |
| 变体规格选择 | `variant`（original/large/medium/thumbnail，srcset 自动输出） |
| SVG 智能处理 | `inlineSvg` 开关：`MediaMeta.SrcHTML` 内联载体，识别 svg 类型；开启后源码内联（CSS 可控 fill/stroke），默认标准 `<img>` |
| 固定比例 | `aspectRatio`（original/1:1/4:3/16:9/21:9/3:4/custom + `aspectRatioValue`）→ `aspect-ratio` |
| Object Fit | `objectFit`（cover/contain/fill） |
| 对齐 | `align` 三端（left/center/right，块级 margin 控制） |
| 自定义尺寸 | `width` / `maxWidth`（safe 白名单：auto/百分比/px/rem） |
| 圆角/边框/阴影 | Advanced 层（02-C0 已有四角圆角/边框/阴影 Token） |
| CSS 滤镜 | `filters` 五值（brightness/contrast/saturate/grayscale/blur）；语义：亮/对比/饱和 100=无调整省略，grayscale 100=纯黑白有效，blur 0=无模糊省略 |
| 悬浮微动 | `hover`（scale 缩放 + restoreColor 灰阶恢复彩色 + duration），`:hover` 规则 + transition |
| 防抖懒加载 | width/height 必注入（防 CLS）；`loading` 默认 lazy，`eager` 可切；`fetchPriority` high 可选；`decoding="async"` 恒定 |
| SEO 元数据 | alt/title 继承媒体库全局值 + 局部覆盖（alt 覆盖测试可见） |
| 图注 | `caption`（可读媒体库全局 Caption 或手动输入）→ `<figure>/<figcaption>` |
| 点击动作 | `clickAction`：none / link（含 `linkTarget` blank/self、`linkRel` nofollow）/ **lightbox（零 JS：CSS `:target` 浮层，符合零客户端 JS 约束）** |
| CMS 绑定 | `binding.field`（camelCase 字段路径）+ `fallback` 占位图（绑定空值回退） |
| 媒体契约扩展 | `MediaMeta.Caption`、`MediaMeta.SrcHTML`（core/render.go + media/resolve.go） |

## 2. 关键设计决策

- **零 JS 灯箱**：规范要求全屏预览，但架构不变量约束“浏览器端零 JavaScript 布局计算”。
  采用 CSS `:target` 伪类实现：点击图 `<a href="#wp-lb-<id>">`，浮层 `#wp-lb-<id>:target { display: flex }`，关闭链接回退锚点。
  无脚本、确定性输出，且浮层样式属于静态 CSS 而非布局计算（无 JS 执行）。
- **绑定路径白名单放宽**：`entity.camelCaseField`（规范示例 `post.featuredImage`），
  统一放宽三个组件（heading/text/image）的 fieldPathRe 第二段允许大写字母。
- **滤镜值语义**：grayscale(100%) 是“默认黑白”有效语义，不按“无调整”省略。
- **SVG 内联载体**：内存 Store 以 `variant.URL` 承载源码字符串；生产实现由媒体存储读取 SVG 文件内容填充 `SrcHTML`。

## 3. 实现位置

| 文件 | 内容 |
|---|---|
| `core/render.go` | `MediaMeta` 增加 `Caption` / `SrcHTML` 字段 |
| `media/resolve.go` | SVG 类型解析内联载体 |
| `media/render.go` | `ImageHTMLOptions` 增加 `Loading`/`FetchPriority`（eager/fetchpriority/decoding 恒 async） |
| `components/image/image.go` | 组件本体：媒体源双通道、全尺寸排版、滤镜/悬浮、图注、三种点击动作、CMS 绑定兜底 |
| `public/test/builder/unit/image_spec_test.go` | 规范能力测试（SVG 内联/照片全流程/外链/灯箱/绑定/6 项校验拒绝） |