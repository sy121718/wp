# 02-C8 · 按钮与 CTA 组件实现说明

> 基于用户规范《02-C5 · 按钮与行动召唤组件规范》的实现映射与设计决策。
> 用户文档编号 02-C5 与既有《02-C5 泛型组件基座》重号，按实现批次编号为 02-C8。

## 1. 规范覆盖清单

| 规范条目 | 实现 |
|---|---|
| 单层语义化标签 | 动作类型驱动：modal → `<button type="button" data-modal-target="...">`（无 wrapper）；其余 → 单层 `<a>` |
| 统一链接协议 | `action`：internal（站内路径白名单）/ external / anchor（元素 ID 白名单）/ native（tel:/mailto: 协议白名单）/ modal / link（动态绑定） |
| 外部链接打开方式 | `target`：self / blank（自动 rel="noopener noreferrer"） |
| SEO 策略 | `rel`：nofollow / sponsored（与 noopener noreferrer 正确合并） |
| 锚点滚动 | `#<element-id>` href 直出（平滑滚动由全局 theme CSS 提供） |
| 动态 CMS 链接绑定 | `binding.field`（仅 link 动作；绑定空值无 fallback 时编译报错） |
| 按钮文本 | 静态 `text`（html 转义；绑定场景按规范定位为链接绑定，文本仍静态） |
| 字号/字重/字距/大小写 | `fontSize`/`fontWeight`(400~800 select)/`letterSpacing`/`transform` |
| 图标集成 | `icon`：builtin 白名单 SVG（8 个箭头/对勾/电话/邮件，24 viewBox stroke=currentColor）/ media 库图标以 `<img>` 直引 URL 输出；prefix/suffix 位置；spacing 间距；hoverShift 悬停位移（`translateX` 过渡） |
| 尺寸预设 | `size`：xs/sm/md/lg/xl（padding+font-size 预设表） |
| 块级铺满 | `block` 三端独立（display:flex + width:100% 分断点） |
| 变体 | `variant`：solid（填充）/ outline（描边，悬停填充）/ ghost（透明文本） |
| 双态外观 | `normal`/`hover` State（bg/color/border/shadow Token）+ hoverLift（translateY） |
| 圆角 | `radius` select：0/6/8/9999（胶囊） |
| Advanced 层 | 基座（margin/align-self/显隐/class+ID） |

## 2. 关键设计决策

- **标签智能选择即 CTA 范式核心**：跳转/原生协议/锚点 → `<a>`；弹窗/表单触发 → `<button type="button">`。
  后续卡片/横幅复合组件复用本组件的 `action` 协议字段。
- **rel 合并规则**：blank 自动包含 noopener noreferrer；nofollow/sponsored 追加于同一 `rel` 属性。
- **绑定语义**：link 动作绑定的是**链接**（如 post.permalink），非文本；
  文本绑定（如 "立即购买 $${price}"）属 02-C1 ContentResolver 体系的字段模板场景，body 后续扩展。
- **内置图标库为白名单常量表**（确定性、零外部依赖）；媒体库图标由 `button.go:renderIcon` 对 `source=media` 直接输出
  `<img class="bt-icon" src="...">`（URL 直引、构建期零解析、不内联 SVG 源码；fill/stroke 主题化受限但引用稳定）。

## 3. 实现位置

| 文件 | 内容 |
|---|---|
| `components/button/button.go` | 组件本体（协议校验/标签选择/图标/双态/尺寸预设/块级） |
| `components/button/jet_test.go` | 8 组测试（外链+图标+双态/弹窗 button/三动作/动态绑定/媒体图标/块级/8 项校验拒绝/确定性） |