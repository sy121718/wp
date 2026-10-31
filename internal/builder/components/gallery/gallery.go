package gallery

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

//go:embed enhance-carousel.js
var enhanceCarouselJS string

//go:embed enhance-lightbox.js
var enhanceLightboxJS string

const Type = "core.gallery"

const (
	LayoutGrid     = "grid"
	LayoutCarousel = "carousel"
)

const (
	CaptionNone  = "none"
	CaptionBelow = "below"
	CaptionHover = "hover"
)

const (
	ClickLightbox = "lightbox"
	ClickLink     = "link"
	ClickNone     = "none"
)

var presetRatios = map[string]string{
	"original": "", "1:1": "1 / 1", "4:3": "4 / 3",
	"16:9": "16 / 9", "3:2": "3 / 2", "3:4": "3 / 4",
}

type Item struct {
	URL     string `json:"url" ct:"media"`
	Alt     string `json:"alt,omitempty"`
	Caption string `json:"caption,omitempty"`
	Link    string `json:"link,omitempty"`

	Loading string `json:"loading,omitempty" ct:"select,=继承组件,on=开启懒加载,off=关闭懒加载,default=,sec=content,label=单图加载"`

	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=单图优先级"`
}

type Columns struct {
	Desktop int `json:"desktop,omitempty"`
	Tablet  int `json:"tablet,omitempty"`
	Mobile  int `json:"mobile,omitempty"`
}

type SlidesPerView struct {
	Desktop float64 `json:"desktop,omitempty"`
	Tablet  float64 `json:"tablet,omitempty"`
	Mobile  float64 `json:"mobile,omitempty"`
}

type Carousel struct {
	Autoplay      bool          `json:"autoplay,omitempty"`
	Interval      int           `json:"interval,omitempty"`
	Infinite      bool          `json:"infinite,omitempty"`
	PauseOnHover  bool          `json:"pauseOnHover,omitempty"`
	SlidesPerView SlidesPerView `json:"slidesPerView,omitempty"`
	Arrows        bool          `json:"arrows,omitempty"`
	Dots          bool          `json:"dots,omitempty"`

	FirstEagerOff bool `json:"firstEagerOff,omitempty" ct:"bool,sec=content,label=关闭首图优先加载"`
}

type Grid struct {
	Columns   Columns `json:"columns,omitempty"`
	ColumnGap string  `json:"columnGap,omitempty"`
	RowGap    string  `json:"rowGap,omitempty"`
}

type Hover struct {
	Scale    string `json:"scale,omitempty"`
	Overlay  string `json:"overlay,omitempty"`
	Deepen   bool   `json:"deepen,omitempty"`
	Duration string `json:"duration,omitempty"`
}

type Binding struct {
	Field       string `json:"field,omitempty"`
	Fallback    bool   `json:"fallback,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

type Props struct {
	Items []Item `json:"items,omitempty"`

	Binding *Binding `json:"binding,omitempty"`

	Mode string `json:"mode,omitempty" ct:"select,grid=网格,carousel=轮播,default=grid,sec=content,label=展示模式"`

	Grid Grid `json:"grid,omitempty"`

	Carousel Carousel `json:"carousel,omitempty"`

	AspectRatio string `json:"aspectRatio,omitempty" ct:"select,original=原图,1:1=1:1,4:3=4:3,16:9=16:9,3:2=3:2,3:4=3:4,default=original,sec=style,label=宽高比"`
	ObjectFit   string `json:"objectFit,omitempty" ct:"select,cover=铺满裁剪,contain=完整包含,default=cover,sec=style,label=填充方式"`
	Radius      string `json:"radius,omitempty" ct:"dimension,maxlen=30,sec=style"`
	BorderWidth string `json:"borderWidth,omitempty" ct:"dimension,maxlen=20,sec=style"`
	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=100,sec=style"`
	Hover       Hover  `json:"hover,omitempty"`

	ClickAction string `json:"clickAction,omitempty" ct:"select,lightbox=灯箱放大,link=打开链接,none=无,default=lightbox,sec=content,label=点击动作"`
	DefaultLink string `json:"defaultLink,omitempty" ct:"url,sec=content"`
	CaptionMode string `json:"captionMode,omitempty" ct:"select,none=不显示,below=图下方,hover=悬停显示,default=none,sec=style,label=说明方式"`

	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		DisplayName:     "图集",
		Hint:            "图片网格 / 轮播",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"mode": "grid",
			"items": []any{
				map[string]any{
					"url": "https://placehold.co/1200x800/png",
					"alt": "图集占位图",
				},
			},
			"grid": map[string]any{
				"columns": map[string]any{
					"desktop": 3,
				},
			},
			"aspectRatio": "16:9",
			"objectFit":   "cover",
			"radius":      "8px",
		},
		TypeName:      Type,
		ValidateExtra: validateExtra,

		Translatable: []string{"alt", "caption"},
	},
}

var (
	fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)
)

func validateExtra(p *Props, nodeID string) (err error) {
	hasStatic := len(p.Items) > 0
	hasBinding := p.Binding != nil && p.Binding.Field != ""
	if !hasStatic && !hasBinding {
		return fmt.Errorf("必须提供静态图集或 CMS 图集绑定")
	}
	if hasBinding && !fieldPathRe.MatchString(p.Binding.Field) {
		return fmt.Errorf("无效的绑定字段路径: %q", p.Binding.Field)
	}
	if p.Binding != nil && p.Binding.Placeholder != "" && !isSafeURL(p.Binding.Placeholder) {
		return fmt.Errorf("无效的占位图 URL: %q", p.Binding.Placeholder)
	}

	for i := range p.Items {
		if p.Items[i].URL == "" || !isSafeURL(p.Items[i].URL) {
			return fmt.Errorf("第 %d 张图无效的图片地址: %q", i+1, p.Items[i].URL)
		}
		if p.Items[i].Link != "" && !isSafeURL(p.Items[i].Link) {
			return fmt.Errorf("第 %d 张图无效的链接: %q", i+1, p.Items[i].Link)
		}
	}

	if p.Mode == "" || p.Mode == LayoutGrid {
		c := p.Grid.Columns
		if c.Desktop != 0 && (c.Desktop < 1 || c.Desktop > 8) {
			return fmt.Errorf("桌面端列数必须在 1~8 之间: %d", c.Desktop)
		}
		for bp, n := range map[string]int{"tablet": c.Tablet, "mobile": c.Mobile} {
			if n != 0 && (n < 1 || n > 8) {
				return fmt.Errorf("%s 端列数必须在 1~8 之间: %d", bp, n)
			}
		}
		for _, v := range []string{p.Grid.ColumnGap, p.Grid.RowGap} {
			if v != "" && !core.IsSafeCSSValue(v) {
				return fmt.Errorf("无效的网格间距: %q", v)
			}
		}
	}
	if p.Mode == LayoutCarousel {
		c := p.Carousel
		if c.Interval != 0 && (c.Interval < 1000 || c.Interval > 60000) {
			return fmt.Errorf("自动播放间隔必须在 1000~60000 ms 之间: %d", c.Interval)
		}
		for bp, n := range map[string]float64{
			"desktop": c.SlidesPerView.Desktop, "tablet": c.SlidesPerView.Tablet, "mobile": c.SlidesPerView.Mobile,
		} {
			if n != 0 && (n < 1 || n > 8) {
				return fmt.Errorf("%s 端单屏张数必须在 1~8 之间: %v", bp, n)
			}
		}
	}
	if p.Hover.Scale != "" && !core.IsSafeCSSValue(p.Hover.Scale) {
		return fmt.Errorf("无效的缩放值: %q", p.Hover.Scale)
	}
	if p.Hover.Overlay != "" && p.Hover.Overlay != "dark" && p.Hover.Overlay != "light" {
		return fmt.Errorf("无效的遮罩类型: %q（仅 dark/light）", p.Hover.Overlay)
	}
	if p.Hover.Duration != "" && !core.IsSafeCSSValue(p.Hover.Duration) {
		return fmt.Errorf("无效的过渡时长: %q", p.Hover.Duration)
	}
	if p.ClickAction == ClickLink && p.DefaultLink == "" {
		return fmt.Errorf("链接动作必须提供默认链接")
	}
	return nil
}

func parseValues(v string) (items []Item, err error) {
	var raw []json.RawMessage
	if json.Unmarshal([]byte(v), &raw) != nil {

		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				items = append(items, Item{URL: s})
			}
		}
		return items, nil
	}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			items = append(items, Item{URL: s})
			continue
		}
		var it Item
		if json.Unmarshal(r, &it) == nil {
			items = append(items, it)
		}
	}
	return items, nil
}

//go:embed gallery.css
var galleryCSS string

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	cols := func(n int) string {
		if n <= 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	colsDesktop := cols(p.Grid.Columns.Desktop)
	if colsDesktop == "" {
		colsDesktop = "4"
	}

	slideW := func(n float64) string {
		if n <= 0 {
			return ""
		}
		return fmt.Sprintf("%.3f%%", 100/n)
	}
	slideDesktop := slideW(p.Carousel.SlidesPerView.Desktop)
	if slideDesktop == "" {
		slideDesktop = "100.000%"
	}

	objectFit := ""
	if p.ObjectFit != "" && p.ObjectFit != "cover" {
		objectFit = p.ObjectFit
	}
	overlayColor := ""
	switch p.Hover.Overlay {
	case "dark":
		overlayColor = "rgba(0,0,0,0.35)"
	case "":
	default:
		overlayColor = "rgba(255,255,255,0.35)"
	}

	vars := map[string]string{
		"ratio":        presetRatios[p.AspectRatio],
		"objectFit":    objectFit,
		"radius":       p.Radius,
		"borderWidth":  p.BorderWidth,
		"borderColor":  p.BorderColor,
		"isGrid":       core.BoolVar(p.Mode == LayoutGrid),
		"colsDesktop":  colsDesktop,
		"columnGap":    p.Grid.ColumnGap,
		"rowGap":       p.Grid.RowGap,
		"colsTablet":   cols(p.Grid.Columns.Tablet),
		"colsMobile":   cols(p.Grid.Columns.Mobile),
		"isCarousel":   core.BoolVar(p.Mode == LayoutCarousel),
		"slideDesktop": slideDesktop,
		"slideTablet":  slideW(p.Carousel.SlidesPerView.Tablet),
		"slideMobile":  slideW(p.Carousel.SlidesPerView.Mobile),
		"hasHover":     core.BoolVar(p.Hover.Scale != "" || p.Hover.Overlay != "" || p.Hover.Deepen),
		"duration":     defaultDur(p.Hover.Duration),
		"scale":        p.Hover.Scale,
		"deepen":       core.BoolVar(p.Hover.Deepen),
		"hasOverlay":   core.BoolVar(p.Hover.Overlay != ""),
		"overlayColor": overlayColor,
		"captionHover": core.BoolVar(p.CaptionMode == CaptionHover),
	}
	if err := core.ApplyComponentCSSTmpl(b, sel, galleryCSS, vars); err != nil {
		panic(fmt.Sprintf("gallery 组件样式解析失败: %v", err))
	}
}

func slideNum(n float64, def float64) string {
	if n <= 0 {
		n = def
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func isSafeURL(s string) bool {
	if len(s) > 500 {
		return false
	}
	for _, r := range s {
		ok := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("./:?=&%~#+_@-", r)
		if !ok {
			return false
		}
	}
	if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return true
	}
	return false
}

func defaultDur(d string) string {
	if d == "" {
		return "300ms"
	}
	return d
}

func init() {
	core.Register(Widget)
	core.RegisterTemplate("gallery", galleryTemplate)

	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCarousels"},
		Feats:  []string{"data-carousel"},
		Source: enhanceCarouselJS,
	})

	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initLightboxes"},
		Feats:  []string{"data-lightbox"},
		Source: enhanceLightboxJS,
	})
}

//go:embed gallery.jet
var galleryTemplate string
