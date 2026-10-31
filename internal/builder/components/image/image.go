package image

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.image"

var presetRatios = map[string]string{
	"original": "",
	"1:1":      "1 / 1",
	"4:3":      "4 / 3",
	"16:9":     "16 / 9",
	"21:9":     "21 / 9",
	"3:4":      "3 / 4",
}

type Responsive struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

type Align struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

type Filters struct {
	Brightness int `json:"brightness,omitempty"`
	Contrast   int `json:"contrast,omitempty"`
	Saturation int `json:"saturation,omitempty"`
	Grayscale  int `json:"grayscale,omitempty"`
	Blur       int `json:"blur,omitempty"`
}

type Hover struct {
	Scale string `json:"scale,omitempty"`

	RestoreColor bool `json:"restoreColor,omitempty"`

	Duration string `json:"duration,omitempty"`
}

type Binding struct {
	Field    string `json:"field,omitempty" ct:"bindingfield,maxlen=60,sec=content,label=内容字段"`
	Fallback string `json:"fallback,omitempty"`
}

type AltBinding struct {
	Field    string `json:"field,omitempty" ct:"bindingfield,maxlen=60,sec=content,label=alt 字段"`
	Fallback string `json:"fallback,omitempty"`
}

type Props struct {
	Src string `json:"src,omitempty" ct:"media,sec=content,label=图片地址"`

	Alt string `json:"alt,omitempty" ct:"string,maxlen=500,sec=content,label=替代文字"`

	Title string `json:"title,omitempty" ct:"string,maxlen=500,sec=content,label=标题"`

	Caption string `json:"caption,omitempty" ct:"text,maxlen=500,sec=content,label=图注"`

	AspectRatio      string `json:"aspectRatio,omitempty" ct:"select,original=原图,1:1=1:1,4:3=4:3,16:9=16:9,21:9=21:9,3:4=3:4,custom=自定义,sec=style,label=宽高比"`
	AspectRatioValue string `json:"aspectRatioValue,omitempty" ct:"safe,maxlen=20,sec=style,label=自定义宽高比"`
	ObjectFit        string `json:"objectFit,omitempty" ct:"select,cover=铺满裁剪,contain=完整包含,fill=拉伸,default=cover,sec=style,label=填充方式"`

	ObjectPosition string `json:"objectPosition,omitempty" ct:"safe,maxlen=40,sec=style,label=对象定位"`
	Align          Align  `json:"align,omitempty" ct:"rtext,sec=layout,label=对齐"`
	Width          string `json:"width,omitempty" ct:"safe,maxlen=30,sec=style,label=宽度"`
	MaxWidth       string `json:"maxWidth,omitempty" ct:"safe,maxlen=30,sec=style,label=最大宽度"`

	Height Responsive `json:"height,omitempty" ct:"rtext,sec=layout,label=高度"`

	BorderRadius string `json:"borderRadius,omitempty" ct:"safe,maxlen=30,sec=style,label=圆角"`

	Filters Filters `json:"filters,omitempty"`
	Hover   Hover   `json:"hover,omitempty"`

	Loading       string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=加载优先级"`
	ClickAction   string `json:"clickAction,omitempty" ct:"select,none=无,link=打开链接,lightbox=灯箱放大,default=none,sec=content,label=点击动作"`
	Link          string `json:"link,omitempty" ct:"url,sec=content,label=链接地址"`
	LinkTarget    string `json:"linkTarget,omitempty" ct:"select,blank=新窗口,self=当前窗口,default=self,sec=content,label=打开方式"`
	LinkRel       string `json:"linkRel,omitempty" ct:"select,nofollow=加 nofollow,none=默认,default=none,sec=content,label=链接关系"`

	Binding *Binding `json:"binding,omitempty" sec:"content"`

	AltBinding *AltBinding `json:"altBinding,omitempty" sec:"content"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "图片",
		Hint:            "外部图片",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"src":       "https://placehold.co/1200x800/png",
			"alt":       "图片占位符",
			"objectFit": "cover",
			"width":     "100%",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"alt", "title", "caption"},
	},
}

func validateExtra(p *Props, nodeID string) (err error) {
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
	if strings.TrimSpace(p.Src) == "" && (p.Binding == nil || p.Binding.Field == "") {
		return fmt.Errorf("%w: 必须提供图片地址或 CMS 绑定", core.ErrIncompleteNode)
	}
	return nil
}

var fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)

//go:embed image.css
var imageCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	heightOf := func(h string) string {
		if h != "" && core.IsSafeCSSValue(h) {
			return h
		}
		return ""
	}

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

		panic(fmt.Sprintf("image 组件样式解析失败: %v", err))
	}
}

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

func (h Hover) DurationOr(d string) string {
	if h.Duration == "" {
		return d
	}
	return h.Duration
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("image", imageTemplate)
}

//go:embed image.jet
var imageTemplate string
