// Package image 实现 core.image 图片与矢量图组件（规范《02-C3 图片与矢量图组件规范》）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：媒体源（媒体库/外链统一 URL）、
// 尺寸比例与适应模式、CSS 滤镜与悬浮微动、
// 懒加载策略、图注（figure/figcaption）、点击动作（链接 / 零 JS 灯箱）、
// CMS 图片字段绑定与占位图兜底。
package image

import (
	"fmt"
	"html"
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
	Field    string `json:"field,omitempty"`
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
	Align          Align  `json:"align,omitempty"`                                             // 三端对齐：left/center/right
	Width          string `json:"width,omitempty" ct:"safe,maxlen=30,sec=style,label=宽度"`      // auto / 百分比 / px / rem
	MaxWidth       string `json:"maxWidth,omitempty" ct:"safe,maxlen=30,sec=style,label=最大宽度"` // 如 480px
	// Height 固定高度（三端独立；设置后配合 object-fit 控制裁切）。
	Height Responsive `json:"height,omitempty"`
	// BorderRadius 圆角（CSS 简写，如 "12px" 或 "12px 0"）。
	BorderRadius string `json:"borderRadius,omitempty" ct:"safe,maxlen=30,sec=style,label=圆角"`

	// --- CSS 滤镜与悬浮 ---
	Filters Filters `json:"filters,omitempty"`
	Hover   Hover   `json:"hover,omitempty"`

	// --- 性能与交互 ---
	Loading       string `json:"loading,omitempty" ct:"select,lazy=懒加载,eager=立即加载,default=lazy,sec=content,label=加载策略"` // 默认 lazy
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,high=高,auto=自动,sec=content,label=加载优先级"`
	ClickAction   string `json:"clickAction,omitempty" ct:"select,none=无,link=打开链接,lightbox=灯箱放大,default=none,sec=content,label=点击动作"`
	Link          string `json:"link,omitempty" ct:"url,sec=content,label=链接地址"`
	LinkTarget    string `json:"linkTarget,omitempty" ct:"select,blank=新窗口,self=当前窗口,default=self,sec=content,label=打开方式"`
	LinkRel       string `json:"linkRel,omitempty" ct:"select,nofollow=加 nofollow,none=默认,default=none,sec=content,label=链接关系"`

	// --- CMS 绑定 ---
	Binding *Binding `json:"binding,omitempty" sec:"content"`

	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
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
	if p.Binding != nil && p.Binding.Field != "" && !fieldPathRe.MatchString(p.Binding.Field) {
		return fmt.Errorf("无效的绑定字段路径: %q", p.Binding.Field)
	}
	return nil
}

// fieldPathRe 绑定路径白名单。
var fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)

// lightboxHTML 零 JS 灯箱浮层：CSS :target 显隐（docs/02-C6 说明约束内实现）。
func lightboxHTML(nodeID, src string) string {
	return `<div id="wp-lb-` + nodeID + `" class="wp-lightbox"><a href="#` + nodeID + `" class="wp-lightbox-close">×</a><img src="` +
		html.EscapeString(src) + `" alt=""></div>`
}

// compileCSS 图片样式：比例/适应/对齐/尺寸/滤镜/悬浮过渡/灯箱浮层。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string
	if p.ObjectFit != "" && p.ObjectFit != "cover" {
		desktop = append(desktop, core.CSSDecl("object-fit", p.ObjectFit))
	}
	if p.ObjectPosition != "" {
		desktop = append(desktop, core.CSSDecl("object-position", p.ObjectPosition))
	}
	if ar, ok := presetRatios[p.AspectRatio]; ok && ar != "" {
		desktop = append(desktop, core.CSSDecl("aspect-ratio", ar))
	} else if p.AspectRatio == "custom" {
		desktop = append(desktop, core.CSSDecl("aspect-ratio", p.AspectRatioValue))
	}
	if p.Width != "" {
		desktop = append(desktop, core.CSSDecl("width", p.Width))
	}
	if p.MaxWidth != "" {
		desktop = append(desktop, core.CSSDecl("max-width", p.MaxWidth))
	}
	// 固定高度（三端独立）。
	appendHeight := func(target *[]string, h string) {
		if h != "" {
			*target = append(*target, core.CSSDecl("height", h))
		}
	}
	appendHeight(&desktop, p.Height.Desktop)
	appendHeight(&tablet, p.Height.Tablet)
	appendHeight(&mobile, p.Height.Mobile)
	if p.BorderRadius != "" {
		desktop = append(desktop, core.CSSDecl("border-radius", p.BorderRadius))
	}

	// 对齐：块级 margin 控制。
	appendAlign := func(target *[]string, a string) {
		switch a {
		case "left":
			*target = append(*target, "display: block", "margin-left: 0", "margin-right: auto")
		case "center":
			*target = append(*target, "display: block", "margin-left: auto", "margin-right: auto")
		case "right":
			*target = append(*target, "display: block", "margin-left: auto", "margin-right: 0")
		}
	}
	appendAlign(&desktop, p.Align.Desktop)
	appendAlign(&tablet, p.Align.Tablet)
	appendAlign(&mobile, p.Align.Mobile)

	// CSS 滤镜。
	if f := filterDecls(p.Filters); f != "" {
		desktop = append(desktop, f)
	}

	b.Add(core.BreakpointDesktop, sel, desktop)
	b.Add(core.BreakpointTablet, sel, tablet)
	b.Add(core.BreakpointMobile, sel, mobile)

	// 悬浮微动：过渡 + :hover 规则。
	if p.Hover.Scale != "" || p.Hover.RestoreColor {
		var transition []string
		if p.Hover.Scale != "" {
			transition = append(transition, "transform "+p.Hover.DurationOr("300ms")+" ease")
		}
		if p.Hover.RestoreColor {
			transition = append(transition, "filter "+p.Hover.DurationOr("300ms")+" ease")
		}
		b.Add(core.BreakpointDesktop, sel, []string{core.CSSDecl("transition", strings.Join(transition, ", "))})

		var hoverDecls []string
		if p.Hover.Scale != "" {
			hoverDecls = append(hoverDecls, "transform: scale("+p.Hover.Scale+")")
		}
		if p.Hover.RestoreColor {
			hoverDecls = append(hoverDecls, "filter: none")
		}
		b.Add(core.BreakpointDesktop, sel+":hover", hoverDecls)
	}

	// 灯箱浮层样式（零 JS :target 显隐）。
	b.Add(core.BreakpointDesktop, ".wp-lightbox", []string{
		"display: none",
		"position: fixed",
		"inset: 0",
		"background: rgba(0,0,0,0.85)",
		"z-index: 1000",
		"align-items: center",
		"justify-content: center",
	})
	b.Add(core.BreakpointDesktop, ".wp-lightbox:target", []string{"display: flex"})
	b.Add(core.BreakpointDesktop, ".wp-lightbox img", []string{
		"max-width: 90vw", "max-height: 90vh",
	})
	b.Add(core.BreakpointDesktop, ".wp-lightbox-close", []string{
		"position: absolute",
		"top: 16px",
		"right: 24px",
		"color: #fff",
		"font-size: 2rem",
		"line-height: 1",
		"text-decoration: none",
	})
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
}
