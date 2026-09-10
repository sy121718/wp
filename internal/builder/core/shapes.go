package core

import "strconv"

// 形状分隔线通用素材库：区块过渡装饰的参数化 path 生成器（整数运算，确定性输出）。
//
// 定位：core 层「效果基本库」的形状部分——任何组件/插件需要区块过渡形状时
// 统一从这里取，单一定义、按需编译。当前消费方：components/shapedivider。
//
// 量纲说明：本库统一 viewBox 1440×120；container 组件的内置单层装饰
// （StyleEx.ShapeDivider）为历史行为，viewBox 1440×64、字节冻结，不经过本库。
const (
	ShapeVBW      = 1440 // viewBox 宽
	ShapeVBH      = 120  // viewBox 高
	ShapeWaveHalf = 360  // 波浪半周期宽（px）：整宽 2 个完整周期
)

// 标准形状取值（与 shapedivider 组件词汇表一致）。
const (
	ShapeKindWave     = "wave"     // 正弦波浪（支持多层与漂移扩宽）
	ShapeKindCurve    = "curve"    // 单拱弧线（支持多层）
	ShapeKindSlope    = "slope"    // 直线斜坡
	ShapeKindTilt     = "tilt"     // 微弯倾斜
	ShapeKindTriangle = "triangle" // 三角峰
	ShapeKindRound    = "round"    // 半椭圆弧
)

// ShapePath 返回指定形状与层变体（0=前景 / 1=中层 / 2=背景层）的 path d。
// driftPad 为漂移动画外扩波形（左右各一个周期），保证平移不露边。
// 仅 wave/curve 传入 variant>0 有意义（多层景深），其余形状恒用变体 0。
func ShapePath(shape string, variant int, driftPad bool) string {
	switch shape {
	case ShapeKindWave:
		return shapeWavePath(variant, driftPad)
	case ShapeKindCurve:
		switch variant {
		case 1:
			return "M0 120C280 44 1160 44 1440 120H0z"
		case 2:
			return "M0 120C220 78 1220 78 1440 120H0z"
		default:
			return "M0 120C360 0 1080 0 1440 120H0z"
		}
	case ShapeKindSlope:
		return "M0 120L1440 0v120H0z"
	case ShapeKindTilt:
		return "M0 120C360 92 1080 28 1440 0v120H0z"
	case ShapeKindTriangle:
		return "M0 120L720 0l720 120H0z"
	case ShapeKindRound:
		return "M0 120A720 120 0 0 1 1440 120H0z"
	default:
		// 调用方白名单校验；此处兜底返回波浪。
		return shapeWavePath(0, driftPad)
	}
}

// shapeWavePath 生成正弦波浪 path。
//
// 波形：以 y=60 为中线的整数近似正弦，每半周期一段三次贝塞尔
// （首段 c 保持水平切线，后续 s 镜像平滑），峰谷 y 差为 2×amp：
// 变体 0 amp=44（峰 16 / 谷 104），变体 1 amp=30，变体 2 amp=18，
// 多层由大到小形成景深。driftPad 时左右各外扩一个周期（x -720..1800）。
func shapeWavePath(variant int, driftPad bool) string {
	var dy, y0 int
	switch variant {
	case 1:
		dy, y0 = 60, 30
	case 2:
		dy, y0 = 36, 42
	default:
		dy, y0 = 88, 16
	}
	segments := 4 // 0..1440
	start := 0
	if driftPad {
		// -720 与 0 相位一致（整周期），波形无缝延续。
		start = -2 * ShapeWaveHalf
		segments = 6
	}
	d := "M" + strconv.Itoa(start) + " " + strconv.Itoa(y0) +
		" c130 0 230 " + strconv.Itoa(dy) + " " + strconv.Itoa(ShapeWaveHalf) + " " + strconv.Itoa(dy)
	alt := -dy
	for i := 1; i < segments; i++ {
		d += " s240 " + strconv.Itoa(alt) + " " + strconv.Itoa(ShapeWaveHalf) + " " + strconv.Itoa(alt)
		alt = -alt
	}
	return d + "V" + strconv.Itoa(ShapeVBH) + "H0z"
}
