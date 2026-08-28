# Elementor/WD 控件形态研究与 go_wp 检查器实施规范

> 数据来源:uaevape.store(Elementor 3.x + Woodmart/XTemos)
> 原始注册表:elementor_controls_research.json / wd_atomic_controls_research.json
> 用户决策:**同类型合并**——Elementor 把响应式控件复制三份(desktop/tablet/mobile 各一个),
> 我们合并为「一个控件 + 设备切换器」,不照抄它的三份形态。

## 一、Elementor 控件类型清单(实测)

| 控件类型 | 用途 | 我们的对应 |
|---|---|---|
| choose | 图标分段选择(方向/对齐/背景类型),≤4 项 | 分段按钮 wb-seg(≤6 项) |
| select | 长列表下拉(字重/定位/附着方式) | 保留 select |
| slider | 数值+单位(px/%/em/rem/vw/vh/custom)+min/max | 数值输入+单位选择(后续加滑块) |
| dimensions | 四向(上右下左)+链接联动+单位 | 后端升级四向后启用;暂单值紧凑控件 |
| gaps | 间距(单值,flex gap) | 现有 gap 字段 |
| color | 颜色选择 | color input |
| switcher | 开关 | checkbox |
| popover_toggle | 弹出式子面板(排版/盒阴影/滚动效果) | 二期:折叠子面板 |
| media | 媒体选择 | 媒体库按钮(已实现) |
| repeater | 列表编辑(幻灯片/列表项) | 二期:repeater |
| font | 字体系列下拉 | 二期 |
| wd_buttons | WD 自定义按钮组(文本对齐) | 分段按钮 |
| number/text/raw_html | 数字/文本/说明 | input/textarea |

## 二、响应式模式(Elementor 的做法 vs 我们的合并)

Elementor:
- 每个响应式控件生成 3 份:width / width_tablet / width_mobile
- 控件标题旁有设备切换器(桌面/平板/手机图标),点击切换**显示对应的控件副本**
- 数据同值时副本隐藏(继承)

我们的合并形态:
- 一个控件 + 右侧三设备小图标(桌面/平板/手机),点击切换控件绑定的值域
- AST 存储:props.box.padding = { desktop: "32px", tablet: "", mobile: "" }(已有 Responsive 结构 ✓)
- 控件仅在当前设备值与桌面值不同时显示「已自定义」标记

## 三、分组与折叠(Elementor)

- 面板 tab:布局 / 样式 / 高级设置
- tab 内 section 折叠组(默认展开第一个)
- popover_toggle:行内按钮弹层(排版、盒阴影),不占面板纵向空间

我们的检查器沿用:布局/样式/扩展 三页签(已有)+ 二期加折叠组与弹层。

## 四、排版组(popover 内完整子控件,WD 实测)

字体大小(px/em/rem/vw) · 字重(100~900/normal/bold) · 转换(大小写) ·
正斜(italic) · 装饰(underline/overline/line-through) · 行高(px/em/rem/lh) ·
字间距 · 词间距 · 字体系列

→ 全部响应式。我们的 TextStyle 已有 desktop/tablet/mobile 三端(对齐)。

## 五、背景系统(container 样式 tab)

背景类型 choose:经典/渐变/视频/幻灯片(4 种,分段按钮)
- 经典:颜色 + 图像 + 位置/重复/尺寸/附着
- 渐变:两色 + 角度 + 类型
- 视频/幻灯片:media + 播放设置
+ 背景覆盖层(popover)+ 背景滤镜(popover)

我们的 bg 面板一期已做(bgColor/bgGradient/bgImage),二期补覆盖层与滤镜。

## 六、落地清单(检查器改造)

1. ✅ 短枚举(≤6)→ 分段按钮 wb-seg(本轮)
2. ✅ 选项中文映射 OPTION_LABELS(本轮)
3. ⬜ 数值字段:输入框+单位下拉(px/%/em/rem/vh)+三端设备切换(绑 Responsive)
4. ⬜ dimensions 四向:等后端 BoxProps 升级四向后启用联动控件
5. ⬜ popover 子面板:排版组/盒阴影
6. ⬜ repeater:幻灯片/列表项/表单字段
7. ⬜ 颜色选择器统一(color input + 主题 Token 提示)
