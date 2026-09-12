// Package shapedivider 实现 core.shapedivider 形状分隔线组件
// （区块过渡装饰，对标 Elementor/Divi Section Divider 与 GrapesJS shape divider 生态）。
//
// 与 container 内置单层形状白名单（03-A §3.1 Tab2）互补：本组件为独立叶子节点，
// 可放置于任意两区块之间，支持多层景深（wave/curve 1~3 层）、三端高度、
// 水平/垂直镜像与 CSS-only 层漂移动画。全部 path 由整数运算参数化生成，
// 零客户端 JS、确定性构建（同 props 同字节）。
package shapedivider

import (
	_ "embed" // shapedivider.css 经 //go:embed 打进二进制
	"fmt"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.shapedivider"

// 形状取值。
const (
	ShapeWave     = "wave"     // 正弦波浪（支持多层与漂移）
	ShapeCurve    = "curve"    // 单拱弧线（支持多层）
	ShapeSlope    = "slope"    // 直线斜坡
	ShapeTilt     = "tilt"     // 微弯倾斜
	ShapeTriangle = "triangle" // 三角峰
	ShapeRound    = "round"    // 半椭圆弧
)

// 层动画取值。
const (
	AnimNone  = "none"
	AnimDrift = "drift"
)

// Height 三端高度（各有缺省：桌面 120px / 平板 90px / 手机 64px）。
type Height struct {
	Desktop string `json:"desktop,omitempty" ct:"dimension,maxlen=20,sec=style,label=桌面端高度"`
	Tablet  string `json:"tablet,omitempty" ct:"dimension,maxlen=20,sec=style,label=平板端高度"`
	Mobile  string `json:"mobile,omitempty" ct:"dimension,maxlen=20,sec=style,label=手机端高度"`
}

// Props shapedivider 属性。
type Props struct {
	// Shape 形状：wave / curve / slope / tilt / triangle / round（默认 wave）。
	Shape string `json:"shape,omitempty" ct:"select,wave=波浪,curve=弧线,slope=斜坡,tilt=倾斜,triangle=三角,round=圆弧,default=wave,sec=content,label=形状"`
	// Layers 层数（0 视作 1）：仅 wave / curve 支持 2~3 层（多层自动降透明度做景深）。
	Layers int `json:"layers,omitempty" ct:"int,min=0,max=3,sec=content,label=层数（波浪/弧线）"`
	// FlipX / FlipY 水平/垂直镜像（CSS transform，path 不变）。
	FlipX bool `json:"flipX,omitempty" ct:"bool,sec=transform,label=水平翻转"`
	FlipY bool `json:"flipY,omitempty" ct:"bool,sec=transform,label=垂直翻转"`
	// Height 三端高度。
	Height Height `json:"height,omitempty"`
	// Color 前景层颜色（主题 Token 或色值；空 = currentColor）。
	Color string `json:"color,omitempty" ct:"color,maxlen=200,sec=style,label=前景颜色"`
	// ColorBack 背景层颜色（层数≥2 时生效；空 = 前景色 + 递减 fill-opacity）。
	ColorBack string `json:"colorBack,omitempty" ct:"color,maxlen=200,sec=style,label=背景层颜色"`
	// Animate 层漂移动画（CSS-only）：仅 wave 且层数≥2 时可用。
	// 注意量纲：本组件漂移作用于 SVG 内部 path（-60 = viewBox 单位 ≈ 4% 宽度），
	// 与通用循环动效 Interaction.LoopEffect=drift（HTML 元素量纲 sky-loop-drift，
	// 见 core/css.go）名称相近但用途不同——前者层错位流动，后者元素微漂。
	Animate string `json:"animate,omitempty" ct:"select,none=无,drift=层漂移,default=none,sec=style,label=动画"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// 无作者文本字段，Translatable 保持 nil（链接/色值/枚举永不翻译）。
	},
}

// effectiveShapes 多层/漂移能力白名单。
func multiLayerShape(shape string) bool {
	return shape == ShapeWave || shape == ShapeCurve
}

// effectiveShape 空值缺省 wave。
func effectiveShape(p *Props) string {
	if p.Shape == "" {
		return ShapeWave
	}
	return p.Shape
}

// effectiveLayers 层数缺省（<1 → 1，上限 3）。
func effectiveLayers(p *Props) int {
	if p.Layers < 1 {
		return 1
	}
	if p.Layers > 3 {
		return 3
	}
	return p.Layers
}

// validateExtra 关系性校验：形状白名单、多层组合、漂移组合、CSS 安全值。
func validateExtra(p *Props, nodeID string) (err error) {
	shape := effectiveShape(p)
	switch shape {
	case ShapeWave, ShapeCurve, ShapeSlope, ShapeTilt, ShapeTriangle, ShapeRound:
	default:
		return fmt.Errorf("无效的形状: %q", p.Shape)
	}
	if p.Layers < 0 || p.Layers > 3 {
		return fmt.Errorf("层数越界: %d（1~3）", p.Layers)
	}
	layers := effectiveLayers(p)
	if layers > 1 && !multiLayerShape(shape) {
		return fmt.Errorf("形状 %q 仅支持单层（多层仅 wave/curve）", shape)
	}
	switch p.Animate {
	case "", AnimNone:
	case AnimDrift:
		if shape != ShapeWave {
			return fmt.Errorf("层漂移动画仅 wave 形状支持（当前 %q）", shape)
		}
		if layers < 2 {
			return fmt.Errorf("层漂移动画需要 2 层以上（当前 %d 层）", layers)
		}
	default:
		return fmt.Errorf("无效的动画取值: %q", p.Animate)
	}
	for _, f := range []struct {
		name string
		v    string
	}{{"桌面端高度", p.Height.Desktop}, {"平板端高度", p.Height.Tablet}, {"手机端高度", p.Height.Mobile}} {
		if f.v != "" && !core.IsSafeCSSValue(f.v) {
			return fmt.Errorf("无效的%s: %q", f.name, f.v)
		}
	}
	if err := checkFill("前景颜色", p.Color); err != nil {
		return err
	}
	if err := checkFill("背景层颜色", p.ColorBack); err != nil {
		return err
	}
	return nil
}

// checkFill 填充色校验：CSS 安全值 + 禁 url() 引用（防 SVG 外部资源引用）。
func checkFill(name, v string) error {
	if v == "" {
		return nil
	}
	if !core.IsSafeCSSValue(v) || strings.Contains(v, "url(") {
		return fmt.Errorf("无效的%s: %q", name, v)
	}
	return nil
}

// shapedividerCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed shapedivider.css
var shapedividerCSS string

// compileCSS 容器/SVG 三端高度/镜像/漂移动画样式。
//
// 三端高度的缺省值与镜像的四种组合仍在 Go 算好（那是取值逻辑），
// 样式源负责把结果摆到正确的位置与桶里。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// SVG 高度：三端各有缺省（120/90/64px），显式设置覆盖。
	hd, ht, hm := p.Height.Desktop, p.Height.Tablet, p.Height.Mobile
	if hd == "" {
		hd = "120px"
	}
	if ht == "" {
		ht = "90px"
	}
	if hm == "" {
		hm = "64px"
	}

	vars := map[string]string{
		"flip":      strings.Join(flipDecls(p), "; "),
		"h_desktop": hd,
		"h_tablet":  ht,
		"h_mobile":  hm,
		"drift":     boolVar(p.Animate == AnimDrift),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, shapedividerCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("shapedivider 组件样式解析失败: %v", err))
	}
}

// boolVar 条件段变量的真值形态（非空即真）。
func boolVar(v bool) string {
	if v {
		return "1"
	}
	return ""
}

// flipDecls 镜像声明（CSS transform，作用于 svg 元素）。
func flipDecls(p *Props) []string {
	switch {
	case p.FlipX && p.FlipY:
		return []string{"transform: scaleX(-1) scaleY(-1)"}
	case p.FlipX:
		return []string{"transform: scaleX(-1)"}
	case p.FlipY:
		return []string{"transform: scaleY(-1)"}
	default:
		return nil
	}
}

// init 注册形状分隔线组件。
func init() {
	core.Register(Widget)
}
