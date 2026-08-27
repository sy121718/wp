# 02-C7 · 图集与画廊组件实现说明

> 基于用户规范《02-C4 · 图集与画廊组件规范》的实现映射与设计决策。
> 用户文档编号 02-C4 与既有《02-C4 共享控件组》重号，按实现批次编号为 02-C7。

## 1. 规范覆盖清单

| 规范条目 | 实现 |
|---|---|
| 静态多图（媒体库批量 assetId 列表） | `items[]`（含单图 alt/caption/link 覆盖） |
| CMS 图集字段绑定 | `binding.field`，兼容三种绑定值形态：JSON 字符串数组 / 对象数组 / 逗号分隔 |
| 空状态兜底 | 无 placeholder → 组件隐藏（默认）；有 placeholder → 占位图单项输出 |
| 网格模式（BuildStatic 零 JS） | 三端列数（1~8）+ columnGap/rowGap，纯 CSS Grid 直出 |
| 轮播模式（ClientEnhance） | 语义静态骨架（track/slide/arrows/dots）+ `data-carousel` JSON 增强属性（autoplay/interval/infinite/pauseOnHover/slidesPerView，支持 2.5 小数），客户端按受控脚本协议挂载；无脚本时点击仍可看原图 |
| 统一比例/适配 | `aspectRatio`（六预设）+ `objectFit`（cover/contain）作用于全部子图 |
| 统一圆角边框 | `radius` + `borderWidth/BorderColor`（统一施加 .gi） |
| 统一悬浮反馈 | `hover`：scale 缩放 / dark-light 遮罩（::after 纯 CSS）/ 阴影加深 + 过渡时长 |
| 点击动作 | lightbox（默认，锚点打开原图，脚本增强为相册）/ link（单图链接优先，缺省走 DefaultLink）/ none |
| 图注展示 | `captionMode`：none / below（figure-figcaption 常显）/ hover（CSS 滑出） |
| Advanced 通用层 | 容器级 margin/padding/align-self/显隐/class+ID（基座） |

## 2. 关键设计决策

- **零 JS 约束下的轮播**：编译期只输出语义骨架与 `data-carousel` 属性（增强协议），
  滑动/自动播放由后续 Client Enhance 受控脚本通过属性驱动；无脚本时浏览器原生横向滚动
  仍然可用（overflow-x + scroll-snap），首屏与降级路径完整。
- **灯箱默认行为**：点击 `<a href="<原图URL>">`——无脚本时浏览器直开原图；
  有脚本时接管为相册（滑动/双击/ESC/键盘），骨架结构（`data-lightbox` 标记）为增强预留。
- **绑定值多形态兼容**：编辑历史/CMS 输出可能为对象数组或逗号串，`parseValues` 统一消化。
- **默认模式语义**：`mode` 空=grid（校验与渲染统一按 grid 处理，避免了默认可变性的坑）。

## 3. 实现位置

| 文件 | 内容 |
|---|---|
| `components/gallery/gallery.go` | 组件本体（Props/校验/双模式渲染/统一样式编译） |
| `public/test/builder/unit/gallery_test.go` | 6 组测试：网格全流程/轮播骨架/绑定三分支/悬浮/校验拒绝/确定性 |