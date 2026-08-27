// Package image 实现 core.image 图片与矢量图组件（规范《02-C3 图片与矢量图组件规范》）。
// 基座 core.Atom 吸收公共样板；本文件为业务本体：媒体源（媒体库 assetId / 外部 URL）、
// SVG 内联、变体规格、尺寸比例与适应模式、CSS 滤镜与悬浮微动、
// 懒加载策略、图注（figure/figcaption）、点击动作（链接 / 零 JS 灯箱）、
// CMS 图片字段绑定与占位图兜底。
package image

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/media"
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

// Binding CMS 图片字段绑定：解析结果为 assetId 或 URL；为空回退 Fallback。
type Binding struct {
	Field    string `json:"field,omitempty"`
	Fallback string `json:"fallback,omitempty"`
}

// Props 图片组件属性：媒体源/规格 + 尺寸排版 + 视觉（滤镜/悬浮）+ 交互 + 绑定 + Advanced。
type Props struct {
	// AssetID 媒体资产稳定标识（与 Src 二选一，AssetID 优先）。
	AssetID string `json:"assetId,omitempty" ct:"regex,sec=content" ctRegex:"^[A-Za-z0-9_-]{4,64}$"`
	// Src 外部绝对 URL（第三方 CDN / 站外图片；与 AssetID 二选一）。
	Src string `json:"src,omitempty" ct:"url,sec=content"`
	// Variant 媒体库变体规格：original / large / medium / thumbnail（默认 original）。
	Variant string `json:"variant,omitempty" ct:"select,original,large,medium,thumbnail,default=original,sec=content"`
	// InlineSVG 矢量内联开关：开启时以 SVG 源码内联输出（CSS 可控制 fill/stroke）。
	InlineSVG bool `json:"inlineSvg,omitempty" ct:"bool,sec=style"`
	// Alt 局部替代文本（默认继承媒体库全局 Alt）。
	Alt string `json:"alt,omitempty" ct:"text,maxlen=500,sec=content"`
	// Title 局部标题。
	Title string `json:"title,omitempty" ct:"text,maxlen=500,sec=content"`
	// Caption 图注（非空时输出 <figure>/<figcaption>）。
	Caption string `json:"caption,omitempty" ct:"text,maxlen=500,sec=content"`
	// Sizes srcset 的 sizes 提示。
	Sizes string `json:"sizes,omitempty" ct:"safe,maxlen=200,sec=content"`

	// --- 尺寸、排版与对齐 ---
	AspectRatio      string `json:"aspectRatio,omitempty" ct:"select,original,1:1,4:3,16:9,21:9,3:4,custom,sec=style"`
	AspectRatioValue string `json:"aspectRatioValue,omitempty" ct:"safe,maxlen=20,sec=style"` // custom 的 w/h 值，如 3 / 2
	ObjectFit        string `json:"objectFit,omitempty" ct:"select,cover,contain,fill,default=cover,sec=style"`
	Align            Align  `json:"align,omitempty"` // 三端对齐：left/center/right
	Width            string `json:"width,omitempty" ct:"safe,maxlen=30,sec=style"`     // auto / 百分比 / px / rem
	MaxWidth         string `json:"maxWidth,omitempty" ct:"safe,maxlen=30,sec=style"` // 如 480px

	// --- CSS 滤镜与悬浮 ---
	Filters Filters `json:"filters,omitempty"`
	Hover   Hover   `json:"hover,omitempty"`

	// --- 性能与交互 ---
	Loading       string `json:"loading,omitempty" ct:"select,lazy,eager,default=lazy,sec=content"` // 默认 lazy
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,high,auto,sec=content"`
	ClickAction   string `json:"clickAction,omitempty" ct:"select,none,link,lightbox,default=none,sec=content"`
	Link          string `json:"link,omitempty" ct:"url,sec=content"`
	LinkTarget    string `json:"linkTarget,omitempty" ct:"select,blank,self,default=self,sec=content"`
	LinkRel       string `json:"linkRel,omitempty" ct:"select,nofollow,none,default=none,sec=content"`

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
		Render:        render,
	},
}

// validateExtra 关系性校验：媒体源互斥、比例取值、滤镜/缩放值域、绑定路径。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.AssetID == "" && p.Src == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("必须提供媒体库 assetId、外部 URL 或 CMS 绑定")
	}
	if p.AssetID != "" && p.Src != "" {
		return fmt.Errorf("assetId 与外部 URL 只能二选一")
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
		name   string
		value  int
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

// render 媒体源解析（绑定 → assetId → MediaResolver；或外部 URL）→
// SVG 内联 / 标准响应式 HTML → 图注 / 链接 / 零 JS 灯箱包裹。
func render(node *core.Node, p *Props, h *core.AtomRender) (string, error) {
	if p.Variant == "" {
		p.Variant = media.VariantOriginal
	}

	// CMS 绑定优先：解析结果视为 assetId 或 URL。
	assetID, src := p.AssetID, p.Src
	if p.Binding != nil && p.Binding.Field != "" {
		if h.Content == nil {
			return "", fmt.Errorf("编译上下文缺少内容解析器，无法解析绑定 %q", p.Binding.Field)
		}
		v, err := h.Content.ResolveString(p.Binding.Field)
		if err != nil {
			return "", fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
		}
		if v != "" {
			assetID, src = v, "" // 绑定值优先视为 assetId（MediaResolver 失败再当 URL 用）
		} else if p.Binding.Fallback != "" {
			assetID = p.Binding.Fallback
		}
	}

	// 解析媒体元数据（宽高/srcset/源码），或构造纯 URL 元数据。
	var meta *core.MediaMeta
	if assetID != "" {
		if h.Media == nil {
			return "", fmt.Errorf("编译上下文缺少媒体解析器，无法解析 assetId")
		}
		m, err := h.Media.ResolveMedia(assetID, p.Variant)
		if err != nil {
			// 绑定值为外部 URL 的场景：解析失败回退为 URL 直载。
			if src == "" && p.Binding != nil && p.Binding.Field != "" && looksLikeURL(assetID) {
				src = assetID
			} else {
				return "", err
			}
		}
		meta = m
	}
	if src != "" {
		if meta != nil {
			meta.URL = src
		} else {
			meta = &core.MediaMeta{Type: core.MediaTypeImage, URL: src, Alt: p.Alt, Title: p.Title}
		}
	}

	// 组装 img / 内联 SVG。
	var imgHTML string
	if meta.SrcHTML != "" && p.InlineSVG {
		imgHTML = `<span class="` + h.Classes + `">` + meta.SrcHTML + `</span>`
	} else {
		var err error
		imgHTML, err = media.RenderImageHTML(meta, p.Alt, p.Title, media.ImageHTMLOptions{
			Class: h.Classes, Sizes: p.Sizes,
			Loading: p.Loading, FetchPriority: p.FetchPriority,
		})
		if err != nil {
			return "", err
		}
	}
	if meta.Caption != "" && p.Caption == "" {
		p.Caption = meta.Caption
	}

	// 点击动作：lightbox 零 JS 实现（CSS :target 浮层）。
	if p.ClickAction == "lightbox" {
		imgHTML = `<a href="#wp-lb-` + node.ID + `">` + imgHTML + `</a>` +
			lightboxHTML(node.ID, meta)
	} else if p.Link != "" || p.ClickAction == "link" {
		linkAttrs := `href="` + html.EscapeString(p.Link) + `"`
		if p.LinkTarget == "blank" {
			linkAttrs += ` target="_blank"`
		}
		if p.LinkRel == "nofollow" {
			linkAttrs += ` rel="nofollow"`
		}
		if h.CustomID != "" {
			linkAttrs += ` id="` + h.CustomID + `"`
		}
		imgHTML = `<a ` + linkAttrs + `>` + imgHTML + `</a>`
	} else if h.CustomID != "" {
		imgHTML = strings.Replace(imgHTML, "<img ", "<img id=\""+h.CustomID+"\" ", 1)
	}

	// 图注包裹。
	if p.Caption != "" {
		tmp := imgHTML
		imgHTML = `<figure>` + tmp + `<figcaption>` + html.EscapeString(p.Caption) + `</figcaption></figure>`
	}

	compileCSS(node.ID, p, h.CSS)
	return imgHTML, nil
}

// looksLikeURL 粗判是否为外部 URL（回退直载用）。
func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "/")
}

// lightboxHTML 零 JS 灯箱浮层：CSS :target 显隐（docs/02-C6 说明约束内实现）。
func lightboxHTML(nodeID string, meta *core.MediaMeta) string {
	if meta == nil {
		return ""
	}
	return `<div id="wp-lb-` + nodeID + `" class="wp-lightbox"><a href="#` + nodeID + `" class="wp-lightbox-close">×</a><img src="` +
		html.EscapeString(meta.URL) + `" alt=""></div>`
}

// compileCSS 图片样式：比例/适应/对齐/尺寸/滤镜/悬浮过渡/灯箱浮层。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	var desktop, tablet, mobile []string
	if p.ObjectFit != "" && p.ObjectFit != "cover" {
		desktop = append(desktop, "object-fit: "+p.ObjectFit)
	}
	if ar, ok := presetRatios[p.AspectRatio]; ok && ar != "" {
		desktop = append(desktop, "aspect-ratio: "+ar)
	} else if p.AspectRatio == "custom" {
		desktop = append(desktop, "aspect-ratio: "+p.AspectRatioValue)
	}
	if p.Width != "" {
		desktop = append(desktop, "width: "+p.Width)
	}
	if p.MaxWidth != "" {
		desktop = append(desktop, "max-width: "+p.MaxWidth)
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
		b.Add(core.BreakpointDesktop, sel, []string{"transition: " + strings.Join(transition, ", ")})

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
	return "filter: " + strings.Join(parts, " ")
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