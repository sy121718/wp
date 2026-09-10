package core

// effects.go — 视觉效果基本库（按前端基础控件分类的词汇目录 + 统一编译出口）。
//
// 分类总览 v2（12 类；组件按需取用，未引用零字节输出；✅=已实现 ◻️=按需立项）：
//
//	 1. 表面质感 surface ✅ glass 玻璃拟态 / liquid 液态玻璃
//	    → AdvancedProps.Surface；CompileSurface。
//	 2. 边框 border ✅ Gradient 渐变边框 + Flow 流动（@property 角度旋转）
//	    → AdvancedProps.Border；CompileAdvanced 边框段。
//	 3. 背景 background ✅ 渐变流动 BackgroundFlowDecls（container 已接）；
//	    ✅ 图案背景 BackgroundPatternDecls（dots 点阵 / grid 网格）；◻️ 噪点、极光。
//	 4. 焦点 focus ✅ FocusRingDecls 光晕 + FocusTransitionDecl 过渡 → form 已取用；
//	    表单校验错误态用原生 :has(:user-invalid)（零 JS，form 已接）。
//	 5. 文本 text ✅ 渐变文字 TextGradientDecls（AdvancedProps.TextGradient）；
//	    ✅ 描边 TextStrokeDecls；◻️ 逐字入场（组件级）。
//	 6. 动效 motion ✅ 入场×40（含 Animate.css 拆解 24 词，keyframes_animate.go）/
//	    循环×17（含拆解 7 词）/ 悬浮×8（触屏治理 hover:hover）/ 滚动触发 / 吸顶 / ReducedMotion 无障碍
//	    → InteractionProps + css.go/keyframes_animate.go keyframes 通用表。
//	 7. 图片 image ✅ img-zoom 悬停缩放 / img-gray 灰度→彩色 / img-hue 色相流动（HoverEffect）；
//	    ✅ HueRotate 静态色相偏移（AdvancedProps，整体调色 / 多元素色相轮转）；◻️ 模糊过渡。
//	 8. 按钮 button ✅ shine 光泽扫过（HoverEffect）+ lift/scale/glow 复用；
//	    ✅ 按压反馈 ActiveEffect（press/sink/pop/glow，:active，触屏同样生效）。
//	 9. 表格 table ✅ RowHover 行悬停高亮（AddHover 触屏治理）；◻️ 悬停展开。
//	10. 加载 loader ✅ core.loader 组件（spinner/dots/bars/pulse 四形态，零 JS）。
//	11. 阴影 shadow ✅ 预设 sm/md/lg/xl/neon（霓虹发光）。
//	12. 形状 shape ✅ ShapePath 素材库（shapes.go，6 形状参数化）。
//	13. 视口适配 viewport ✅ H5/平板：dimension 字段直写 100dvh（动态视口高，
//	    地址栏伸缩不溢出）与 clamp(最小,首选vw,最大)（流体字号，免三端手调）
//	    ——白名单已兼容值直透；SafeAreaDecls 安全区垫高（container 已接）；
//	    AddHover 悬浮触屏治理（@media hover:hover 全局包裹）；
//	    ◻️ 断点变量化（tablet 1024 / mobile 767 可配）。
//
// 原则：所有效果 = core 词汇表条目 + 统一编译函数；组件只声明词汇，不自带实现；
// CSS 变量驱动主题化（--wp-*），确定性输出（同 props 同字节）；
// 新增特效的标准动作 = 词汇常量 + 白名单 + 编译出口 + 表驱动测试（四件套）。

// 表面质感取值。
const (
	SurfaceGlass    = "glass"    // 玻璃拟态：backdrop 模糊 + 半透明面板 + 高光边框
	SurfaceLiquid   = "liquid"   // 液态玻璃：玻璃 + 高饱和 backdrop + 上下内阴影透镜感（Apple LIQUID GLASS 风格）
	SurfaceNeumorph = "neumorph" // 新拟态：与背景同色的双向柔和阴影，靠光影塑形（Neumorphism）
)

// allowedSurface 表面质感白名单。
var allowedSurface = map[string]bool{"": true, SurfaceGlass: true, SurfaceLiquid: true, SurfaceNeumorph: true}

// CompileSurface 表面质感 → 桌面端声明（Advanced 管线调用）。
// 背景/边框色经 CSS 变量暴露给主题覆写（--wp-glass-bg / --wp-glass-border）。
// 质感的 box-shadow 含内阴影（透镜感），会覆盖同元素的 Shadow 外阴影预设。
func CompileSurface(sel string, surface string, b *CSSBuckets) {
	switch surface {
	case SurfaceGlass:
		b.Add(BreakpointDesktop, sel, []string{
			"backdrop-filter: blur(12px)",
			"background: var(--wp-glass-bg, rgba(255,255,255,.55))",
			"border: 1px solid var(--wp-glass-border, rgba(255,255,255,.45))",
		})
	case SurfaceLiquid:
		b.Add(BreakpointDesktop, sel, []string{
			"backdrop-filter: blur(16px) saturate(1.6)",
			"background: var(--wp-glass-bg, rgba(255,255,255,.45))",
			"border: 1px solid var(--wp-glass-border, rgba(255,255,255,.55))",
			"box-shadow: inset 0 1px 1px rgba(255,255,255,.65), inset 0 -1px 2px rgba(0,0,0,.06), 0 8px 32px rgba(0,0,0,.12)",
		})
	case SurfaceNeumorph:
		// 新拟态：与背景同色 + 双向柔和阴影（左上高光 / 右下暗影）塑形。
		// 背景与阴影色经变量暴露，主题可整体替换（暗色主题改写 --wp-neu-*）。
		b.Add(BreakpointDesktop, sel, []string{
			"background: var(--wp-neu-bg, #e9edf2)",
			"box-shadow: 8px 8px 16px var(--wp-neu-dark, rgba(163,177,198,.6)), -8px -8px 16px var(--wp-neu-light, rgba(255,255,255,.9))",
			"border: 1px solid transparent",
		})
	}
	// 新拟态移动端降级：阴影半径减半（大半径软阴影在低端设备上合成开销更高）。
	if surface == SurfaceNeumorph {
		b.Add(BreakpointMobile, sel, []string{
			"box-shadow: 4px 4px 8px var(--wp-neu-dark, rgba(163,177,198,.6)), -4px -4px 8px var(--wp-neu-light, rgba(255,255,255,.9))",
		})
	}
	// 移动端降级（H5 性能）：backdrop-filter 是低端安卓最贵的合成操作，
	// 手机断点降低模糊半径、liquid 去 saturate，视觉近似但合成开销大幅下降。
	if surface == SurfaceLiquid {
		b.Add(BreakpointMobile, sel, []string{"backdrop-filter: blur(6px) saturate(1.3)"})
	} else if surface == SurfaceGlass {
		b.Add(BreakpointMobile, sel, []string{"backdrop-filter: blur(8px)"})
	}
}

// FocusRingDecls 焦点光晕声明（:focus 态使用；表单类组件取用）。
// 焦点环颜色经 --wp-focus-ring 主题覆写。
func FocusRingDecls() []string {
	return []string{
		"border-color: var(--wp-c-primary, #2563eb)",
		"box-shadow: 0 0 0 3px var(--wp-focus-ring, rgba(37,99,235,.15))",
		"outline: none",
	}
}

// FocusTransitionDecl 焦点过渡声明（控件基础态使用，保证 focus/blur 平滑）。
func FocusTransitionDecl() string {
	return "transition: border-color .2s ease, box-shadow .2s ease"
}

// BackgroundFlowDecls 流动渐变背景声明（配合 BgGradient 使用；背景类组件取用）。
// 前提：渐变 background-size 拉伸至 200%，位移动画往复循环。
func BackgroundFlowDecls() []string {
	return []string{
		"background-size: 200% 200%",
		"animation: wp-bg-flow 8s ease infinite",
	}
}

// BorderFlowAngleProperty 边框流动所需的 @property 角度注册块
// （自定义属性动画，现代浏览器全绿；旧浏览器忽略动画但保留静态渐变边框）。
const BorderFlowAngleProperty = "@property --wp-flow-angle {\n  syntax: \"<angle>\"\n  initial-value: 0deg\n  inherits: false\n}"

// TextGradientDecls 渐变文字声明组（标题/文本类组件取用）。
// gradient 为 CSS 渐变值（ct:safe 白名单校验后传入）；color 透明由裁剪文字显示渐变。
func TextGradientDecls(gradient string) []string {
	return []string{
		"background-image: " + gradient,
		"-webkit-background-clip: text",
		"background-clip: text",
		"color: transparent",
		"-webkit-text-fill-color: transparent",
	}
}

// BackgroundPatternDecls 图案背景（纯 CSS 平铺，零图片资产；容器/区块取用）。
//
// 图案清单（20 种，参考 pattern.css 的经典实现思路重写）：
//
//	dots 点阵 / grid 网格 / overlay 细网格 / diagonal-stripes 斜纹 / diagonal-lines 细斜线
//	vertical-stripes 竖纹 / horizontal-stripes 横纹 / zigzag 锯齿 / checkerboard 棋盘
//	triangles 三角 / diamond 菱形 / crosses 十字 / plus 加号 / squares 方块
//	circles 大圆点 / polka 交错波点 / ripple 同心波纹 / bricks 砖块 / rain 雨丝 / honeycomb 蜂窝
//
// color 为图案色（空或非法值回退 8% 黑）。
func BackgroundPatternDecls(kind, color string) []string {
	c := color
	if c == "" || !IsSafeCSSValue(c) {
		c = "rgba(0,0,0,.08)"
	}
	switch kind {
	case "grid":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 24px 24px",
		}
	case "overlay":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 8px 8px",
		}
	case "diagonal-stripes":
		return []string{
			"background-image: repeating-linear-gradient(45deg, " + c + " 0 2px, transparent 2px 10px)",
		}
	case "diagonal-lines":
		return []string{
			"background-image: repeating-linear-gradient(-45deg, " + c + " 0 1px, transparent 1px 12px)",
		}
	case "vertical-stripes":
		return []string{
			"background-image: repeating-linear-gradient(90deg, " + c + " 0 2px, transparent 2px 12px)",
		}
	case "horizontal-stripes":
		return []string{
			"background-image: repeating-linear-gradient(0deg, " + c + " 0 2px, transparent 2px 12px)",
		}
	case "zigzag":
		return []string{
			"background-image: linear-gradient(135deg, " + c + " 25%, transparent 25%), linear-gradient(225deg, " + c + " 25%, transparent 25%)",
			"background-size: 20px 20px",
			"background-position: 0 0, 10px 0",
		}
	case "checkerboard":
		return []string{
			"background-image: conic-gradient(" + c + " 25%, transparent 0 50%, " + c + " 0 75%, transparent 0)",
			"background-size: 20px 20px",
		}
	case "triangles":
		return []string{
			"background-image: linear-gradient(45deg, " + c + " 25%, transparent 25%), linear-gradient(-45deg, " + c + " 25%, transparent 25%)",
			"background-size: 24px 24px",
		}
	case "diamond":
		return []string{
			"background-image: conic-gradient(from 45deg, " + c + " 25%, transparent 0 50%, " + c + " 0 75%, transparent 0)",
			"background-size: 24px 24px",
		}
	case "crosses":
		return []string{
			"background-image: radial-gradient(" + c + " 2px, transparent 2px), radial-gradient(" + c + " 2px, transparent 2px)",
			"background-size: 24px 24px",
			"background-position: 0 0, 12px 12px",
		}
	case "plus":
		return []string{
			"background-image: linear-gradient(" + c + " 2px, transparent 2px), linear-gradient(90deg, " + c + " 2px, transparent 2px)",
			"background-size: 24px 24px",
			"background-position: 0 11px, 11px 0",
		}
	case "squares":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 16px 16px",
			"background-position: center",
		}
	case "circles":
		return []string{
			"background-image: radial-gradient(" + c + " 3px, transparent 3px)",
			"background-size: 24px 24px",
		}
	case "polka":
		return []string{
			"background-image: radial-gradient(" + c + " 3px, transparent 3px), radial-gradient(" + c + " 3px, transparent 3px)",
			"background-size: 28px 28px",
			"background-position: 0 0, 14px 14px",
		}
	case "ripple":
		return []string{
			"background-image: repeating-radial-gradient(circle at 0 0, " + c + " 0 2px, transparent 2px 16px)",
		}
	case "bricks":
		return []string{
			"background-image: linear-gradient(" + c + " 1px, transparent 1px), linear-gradient(90deg, " + c + " 1px, transparent 1px)",
			"background-size: 24px 12px",
			"background-position: 0 0, 12px 0",
		}
	case "rain":
		return []string{
			"background-image: repeating-linear-gradient(70deg, " + c + " 0 1px, transparent 1px 9px)",
		}
	case "honeycomb":
		return []string{
			"background-image: conic-gradient(from 30deg, " + c + " 0 60deg, transparent 60deg 120deg, " + c + " 120deg 180deg, transparent 180deg 240deg, " + c + " 240deg 300deg, transparent 300deg)",
			"background-size: 20px 20px",
		}
	default: // dots 点阵
		return []string{
			"background-image: radial-gradient(" + c + " 1px, transparent 1px)",
			"background-size: 16px 16px",
		}
	}
}

// TextStrokeDecls 文字描边（标题/文本类组件取用）。
// width 如 "1px"；paint-order 保证描边在填充之下（视觉更细腻）。
func TextStrokeDecls(width, color string) []string {
	return []string{
		"-webkit-text-stroke: " + width + " " + color,
		"paint-order: stroke fill",
	}
}

// —— 弹簧缓动（motion 类；Apple 式手感，linear() 采样曲线）——
//
// 原理：把真实弹簧物理（刚度/阻尼）的位置-时间曲线采样为 linear() 停止点，
// 纯 CSS 实现 iOS 式过冲回弹，无 keyframes、无 JS。浏览器兼容：Chrome 113+ /
// Safari 17.2+ / Firefox 112+；旧浏览器忽略该声明，回退 animation 简写内的
// ease（渐进增强，产物不坏）。采样值按手感等效重写（非逐字复制）。
const (
	// SpringStandardCurve 标准弹簧：默认入场缓动，5~8% 过冲一次回稳。
	SpringStandardCurve = "linear(0, 0.0088 2.86%, 0.0347 5.83%, 0.1413 12.1%, 0.2981 18.6%, 0.4629 24.9%, 0.7415 35%, 0.8683 41.4%, 0.9727 49.5%, 1.0072 53.7%, 1.0209 59.7%, 1.0161 66%, 0.9885 75.4%, 0.9829 80.9%, 0.9854 86.9%, 0.9967 95.1%, 1)"
	// SpringSoftCurve 柔和弹簧：高阻尼轻微过冲，适合大面积区块。
	SpringSoftCurve = "linear(0, 0.0183 3.18%, 0.0704 6.66%, 0.2247 13.7%, 0.4064 21.4%, 0.5811 29.8%, 0.7642 39.8%, 0.8794 48.9%, 0.9591 58.3%, 1.0015 66.1%, 1.0173 76.1%, 1.0126 84.3%, 1.0017 92.9%, 0.9995 97.1%, 1)"
	// SpringBouncyCurve 弹跳弹簧：低阻尼多次过冲，活泼强调场景。
	SpringBouncyCurve = "linear(0, 0.0272 4.03%, 0.1137 9.06%, 0.2601 14.5%, 0.4021 19.9%, 0.6259 28.8%, 0.7605 36.4%, 0.8379 44.4%, 0.9145 54.1%, 0.9569 62.9%, 0.9836 73.1%, 1.0039 84.4%, 1.0083 89.9%, 1.0011 97.4%, 1)"
)

// SafeAreaDecls 刘海屏/底部横条安全区垫高（H5 viewport 适配；container 等取用）。
// edge：bottom（底部横条，最常用）/ top（刘海）/ left / right。
// fallback 0px 兼容不支持 env() 的旧浏览器。
func SafeAreaDecls(edge string) []string {
	// edge 白名单：函数导出，防止未来传入外部值时拼出非法属性名（注入面）。
	switch edge {
	case "top", "bottom", "left", "right":
	default:
		edge = "bottom"
	}
	return []string{
		"padding-" + edge + ": env(safe-area-inset-" + edge + ", 0px)",
	}
}
