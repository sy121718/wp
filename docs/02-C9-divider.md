# 02-C9 · 分割线组件实现说明

> 基于用户规范《02-C6 · 分割线组件规范》的实现映射。用户文档编号 02-C6 与既有《02-C6 图片与矢量图实现说明》重号，按实现批次编号为 02-C9。

## 1. 规范覆盖清单

| 规范条目 | 实现 |
|---|---|
| 线条类型 | `style`：solid（默认）/ dashed / dotted / double（`border-top` 绘制；double 最小 3px 保辨识度） |
| 粗细 | `weight`（px） |
| 总宽度三端 | `width.desktop/tablet/mobile`（百分比或固定值） |
| 对齐 | `align`：left / center（默认）/ right（margin 控制） |
| 线条颜色 | `color`（自定义或主题 Token） |
| 嵌入类型 | `inset.kind`：none（默认）/ text / icon（内置白名单 star/diamond/dot） |
| 嵌入文本样式 | `inset.fontSize/fontWeight/color` |
| 嵌入位置 | `inset.position`：center（等分线）/ left（左短右长 0.5:1.5）/ right（1.5:0.5） |
| 嵌入间距 | `inset.spacing`（元素两侧 padding） |
| Advanced 层 | 基座（margin 三端/显隐/class+ID） |

## 2. 编译输出结构（零 JS）

- **无嵌入**：单层原生 `<hr>`（最简语义化输出，对齐元素标准能力）。
- **有嵌入**：`div.dt > span.dt-line + span.dt-inset[+svg] + span.dt-line` 三段 Flex 结构；
  线段用 `border-top` 绘制（高度 0 + border，不产生额外盒子），元素 `white-space: nowrap` 防折行。

## 3. 实现位置

| 文件 | 内容 |
|---|---|
| `components/divider/divider.go` | 组件本体（线型/宽度三端/对齐/嵌入三态/位置比例） |
| `public/test/builder/unit/divider_test.go` | 5 组测试（纯线 hr/文本嵌入/图标嵌入/4 项校验拒绝/确定性） |

## 4. 首批组件定稿状态

用户规范清单 10 项全部落地：02-A 容器、02-B 媒体中心、02-C0 通用基础、02-C1 标题、
02-C2 正文、02-C3 图片与矢量图、02-C4 图集画廊、02-C5 按钮与链接协议、02-C6 分割线。
下一规范：03-A 可视化编辑器全景与大纲树。