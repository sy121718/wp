// Package image 实现 core.image 图片与矢量图组件（规范《02-C3 图片与矢量图组件规范》）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：媒体源（媒体库/外链统一 URL）、
// 尺寸比例与适应模式、CSS 滤镜与悬浮微动、
// 懒加载策略、图注（figure/figcaption）、点击动作（链接 / 零 JS 灯箱）、
// CMS 图片字段绑定与占位图兜底。
package image

import (
	_ "embed" // image.css 经 //go:embed 打进二进制
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.image"

// 预置比例表（aspect-ratio CSS 值）。
var presetRatios = map[string]string{
	"original": "", // 原图比例：不输出 aspect-ratio（浏览器按 width/height 计算）
	"1:1":      "1 / 1",
	"4:3":      "4 / 3",
	"16:9":     "16 / 9",
	"21:9":     "21 / 9",
	"3:4":      "3 / 4",
}

// Responsive 三端值（桌面/平板/手机，空值沿用上一档）。
type Responsive struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// Align 组件对齐（块级对齐用 margin；左对齐=右 auto）。
type Align struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// Filters CSS 滤镜五值（0~100，100=无调整）。
type Filters struct {
	Brightness int `json:"brightness,omitempty"`
	Contrast   int `json:"contrast,omitempty"`
	Saturation int `json:"saturation,omitempty"`
	Grayscale  int `json:"grayscale,omitempty"`
	Blur       int `json:"blur,omitempty"`
}

// Hover 悬浮微动：缩放比例（如 1.05）+ 滤镜退化值 + 过渡时长。
type Hover struct {
	// Scale 缩放比例（如 "1.05"）。
	Scale string `json:"scale,omitempty"`
	// RestoreColor 默认灰阶，悬停恢复彩色。
	RestoreColor bool `json:"restoreColor,omitempty"`
	// Duration 过渡时长（如 "300ms"）。
	Duration string `json:"duration,omitempty"`
}

// Binding CMS 图片字段绑定：解析结果为图片 URL；为空回退 Fallback（同为 URL）。
type Binding struct {
	Field    string `json:"field,omitempty" ct:"bindingfield,maxlen=60,sec=content,label=内容字段"`
	Fallback string `json:"fallback,omitempty"`
}

// Props 图片组件属性：媒体源（媒体库/外链统一 URL）+ 尺寸排版 + 视觉（滤镜/悬浮）+ 交互 + 绑定 + Advanced。
type Props struct {
	// Src 图片地址：媒体库选择回填 URL 或外部绝对 URL（媒体库/外链统一，构建期直出）。
	Src string `json:"src,omitempty" ct:"media,sec=content,label=图片地址"`
	// Alt 局部替代文本。
	Alt string `json:"alt,omitempty" ct:"text,maxlen=500,sec=content,label=替代文字"`
	// Title 局部标题。
	Title string `json:"title,omitempty" ct:"text,maxlen=500,sec=content,label=标题"`
	// Caption 图注（非空时输出 <figure>/<figcaption>）。
	Caption string `json:"caption,omitempty" ct:"text,maxlen=500,sec=content,label=图注"`

	// --- 尺寸、排版与对齐 ---
	AspectRatio      string `json:"aspectRatio,omitempty" ct:"select,original=原图,1:1=1:1,4:3=4:3,16:9=16:9,21:9=21:9,3:4=3:4,custom=自定义,sec=style,label=宽高比"`
	AspectRatioValue string `json:"aspectRatioValue,omitempty" ct:"safe,maxlen=20,sec=style,label=自定义宽高比"` // custom 的 w/h 值，如 3 / 2
	ObjectFit        string `json:"objectFit,omitempty" ct:"select,cover=铺满裁剪,contain=完整包含,fill=拉伸,default=cover,sec=style,label=填充方式"`
	// ObjectPosition 对象定位（object-fit 裁剪基准点），如 "center center" / "50% 20%"。
	ObjectPosition string `json:"objectPosition,omitempty" ct:"safe,maxlen=40,sec=style,label=对象定位"`
	Align          Align  `json:"align,omitempty" ct:"rtext,sec=layout,label=对齐"`              // 三端对齐：left/center/right
	Width          string `json:"width,omitempty" ct:"safe,maxlen=30,sec=style,label=宽度"`      // auto / 百分比 / px / rem
	MaxWidth       string `json:"maxWidth,omitempty" ct:"safe,maxlen=30,sec=style,label=最大宽度"` // 如 480px
	// Height 固定高度（三端独立；设置后配合 object-fit 控制裁切）。
	Height Responsive `json:"height,omitempty" ct:"rtext,sec=layout,label=高度"`
	// BorderRadius 圆角（CSS 简写，如 "12px" 或 "12px 0"）。
	BorderRadius string `json:"borderRadius,omitempty" ct:"safe,maxlen=30,sec=style,label=圆角"`

	// --- CSS 滤镜与悬浮 ---
	Filters Filters `json:"filters,omitempty"`
	Hover   Hover   `json:"hover,omitempty"`

	// --- 性能与交互 ---
	// Loading 图片加载策略三态：空=默认（继承主题「图片管理」设置）/ on=开启懒加载 / off=关闭。
	// 旧值 lazy/eager 在编译期兼容映射（历史文档）。
	// 保留旧值 lazy/eager 选项：历史页面文档仍在用，校验放行后由编译期映射到三态。
	Loading       string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`
	ClickAction   string `json:"clickAction,omitempty" ct:"select,none=无,link=打开链接,lightbox=灯箱放大,default=none,sec=content,label=点击动作"`
	Link          string `json:"link,omitempty" ct:"url,sec=content,label=链接地址"`
	LinkTarget    string `json:"linkTarget,omitempty" ct:"select,blank=新窗口,self=当前窗口,default=self,sec=content,label=打开方式"`
	LinkRel       string `json:"linkRel,omitempty" ct:"select,nofollow=加 nofollow,none=默认,default=none,sec=content,label=链接关系"`

	// --- CMS 绑定 ---
	Binding *Binding `json:"binding,omitempty" sec:"content"`

	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"alt", "title", "caption"},
	},
}

// validateExtra 关系性校验：媒体源互斥、比例取值、滤镜/缩放值域、绑定路径。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.Src == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("必须提供图片地址或 CMS 绑定")
	}
	if p.AspectRatio == "custom" && p.AspectRatioValue == "" {
		return fmt.Errorf("自定义比例必须提供 w / h 值")
	}
	for bp, a := range map[string]string{"desktop": p.Align.Desktop, "tablet": p.Align.Tablet, "mobile": p.Align.Mobile} {
		if a != "" && a != "left" && a != "center" && a != "right" {
			return fmt.Errorf("无效的 %s 端对齐: %q", bp, a)
		}
	}
	for _, f := range []struct {
		name  string
		value int
	}{
		{"亮度", p.Filters.Brightness}, {"对比度", p.Filters.Contrast},
		{"饱和度", p.Filters.Saturation}, {"灰阶", p.Filters.Grayscale}, {"模糊", p.Filters.Blur},
	} {
		if f.value != 0 && f.value < 1 || f.value > 100 {
			return fmt.Errorf("%s必须在 1~100 之间: %d", f.name, f.value)
		}
	}
	if p.Hover.Scale != "" && !core.IsSafeCSSValue(p.Hover.Scale) {
		return fmt.Errorf("无效的缩放值: %q", p.Hover.Scale)
	}
	if p.Hover.Duration != "" && !core.IsSafeCSSValue(p.Hover.Duration) {
		return fmt.Errorf("无效的过渡时长: %q", p.Hover.Duration)
	}
	for bp, h := range map[string]string{"desktop": p.Height.Desktop, "tablet": p.Height.Tablet, "mobile": p.Height.Mobile} {
		if h != "" && !core.IsSafeCSSValue(h) {
			return fmt.Errorf("无效的 %s 端固定高度: %q", bp, h)
		}
	}
	if p.Binding != nil && p.Binding.Field != "" && !fieldPathRe.MatchString(p.Binding.Field) {
		return fmt.Errorf("无效的绑定字段路径: %q", p.Binding.Field)
	}
	return nil
}

// fieldPathRe 绑定路径白名单。
var fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)

// 灯箱浮层结构已迁移至 image.jet 模板（HTML 下沉 .jet，Jet 默认转义），
// 旧 lightboxHTML 手拼 HTML 已删除。

// imageCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed image.css
var imageCSS string

// compileCSS 图片样式：比例/适应/对齐/尺寸/滤镜/悬浮过渡/灯箱浮层。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 固定高度（三端独立）。编译期容错：非法值跳过声明（对齐「降级不阻断编译」，
	// validateExtra 已在校验阶段拦截，此处为防御性兜底）。
	heightOf := func(h string) string {
		if h != "" && core.IsSafeCSSValue(h) {
			return h
		}
		return ""
	}
	// 对齐：块级用 margin 控制。返回的是**两条声明**（值变量允许承载多条，展开后逐条收录）。
	alignOf := func(a string) string {
		switch a {
		case "left":
			return "margin-left: 0; margin-right: auto"
		case "center":
			return "margin-left: auto; margin-right: auto"
		case "right":
			return "margin-left: auto; margin-right: 0"
		}
		return ""
	}

	objectFit := ""
	if p.ObjectFit != "" && p.ObjectFit != "cover" {
		objectFit = p.ObjectFit
	}
	aspectRatio := ""
	if ar, ok := presetRatios[p.AspectRatio]; ok && ar != "" {
		aspectRatio = ar
	} else if p.AspectRatio == "custom" {
		aspectRatio = p.AspectRatioValue
	}

	// 悬浮微动：过渡与 :hover 形态成对出现（有过渡必有形态，反之亦然）。
	hoverOn := p.Hover.Scale != "" || p.Hover.RestoreColor
	var transition, hoverScale, hoverFilter string
	if p.Hover.Scale != "" {
		transition = "transform " + p.Hover.DurationOr("300ms") + " ease"
		hoverScale = "scale(" + p.Hover.Scale + ")"
	}
	if p.Hover.RestoreColor {
		if transition != "" {
			transition += ", "
		}
		transition += "filter " + p.Hover.DurationOr("300ms") + " ease"
		hoverFilter = "none"
	}

	vars := map[string]string{
		"object_fit":      objectFit,
		"object_position": p.ObjectPosition,
		"aspect_ratio":    aspectRatio,
		"width":           p.Width,
		"max_width":       p.MaxWidth,
		"h_desktop":       heightOf(p.Height.Desktop),
		"h_tablet":        heightOf(p.Height.Tablet),
		"h_mobile":        heightOf(p.Height.Mobile),
		"align_desktop":   alignOf(p.Align.Desktop),
		"align_tablet":    alignOf(p.Align.Tablet),
		"align_mobile":    alignOf(p.Align.Mobile),
		"border_radius":   p.BorderRadius,
		"filter":          filterDecls(p.Filters),
		"hover_on":        core.BoolVar(hoverOn),
		"transition":      transition,
		"hover_scale":     hoverScale,
		"hover_filter":    hoverFilter,
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, imageCSS, vars); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("image 组件样式解析失败: %v", err))
	}
}

// filterDecls 滤镜五值 → CSS 声明。
// 语义：brightness/contrast/saturate 的 100=无调整（省略）；
// grayscale 的 100=纯黑白（有效值，需输出）；blur 的 0=无模糊（省略）。
func filterDecls(f Filters) string {
	fv := func(name string, v int, zeroIsNone bool) string {
		if v == 0 || (v == 100 && zeroIsNone) {
			return ""
		}
		if name == "blur" {
			return fmt.Sprintf("blur(%dpx)", v/10)
		}
		return fmt.Sprintf("%s(%d%%)", name, v)
	}
	var parts []string
	for _, decl := range []string{
		fv("brightness", f.Brightness, true),
		fv("contrast", f.Contrast, true),
		fv("saturate", f.Saturation, true),
		fv("grayscale", f.Grayscale, false),
		fv("blur", f.Blur, true),
	} {
		if decl != "" {
			parts = append(parts, decl)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return core.CSSDecl("filter", strings.Join(parts, " "))
}

// DurationOr 悬浮过渡默认值。
func (h Hover) DurationOr(d string) string {
	if h.Duration == "" {
		return d
	}
	return h.Duration
}

// init 注册图片组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("image", imageTemplate)
}

// imageTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed image.jet
var imageTemplate string
