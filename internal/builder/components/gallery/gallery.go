// Package gallery 实现 core.gallery 图集与画廊组件（规范《02-C4 图集与画廊组件规范》）。
//
// 数据源：静态图集（URL 列表 + 单图 alt/caption/link 覆盖）或 CMS 图集字段绑定
// （兼容字符串数组/对象数组/逗号分隔三种形态，空时隐藏或占位图兜底）。
//
// 展示模式：
//   - 网格（Grid）：BuildStatic——纯 CSS Grid 编译直出，零客户端 JS；
//   - 轮播（Carousel）：ClientEnhance——编译期输出语义静态骨架 + data-carousel 增强属性，
//     客户端按受控脚本协议挂载滑动交互；无脚本时仍可点击查看原图。
//
// 全局统一样式：比例/适配/圆角边框/悬浮反馈作用于全部子图。
package gallery

import (
	_ "embed" // enhance.js 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// 组件行为源。与 .go / .css / .jet 同目录：改交互不必再去 enhance.js 里找。
//
//go:embed enhance-carousel.js
var enhanceCarouselJS string

//
//go:embed enhance-lightbox.js
var enhanceLightboxJS string

// Type 组件类型标识。
const Type = "core.gallery"

// 展示模式常量。
const (
	LayoutGrid     = "grid"
	LayoutCarousel = "carousel"
)

// 图注显示方式。
const (
	CaptionNone  = "none"
	CaptionBelow = "below"
	CaptionHover = "hover"
)

// 点击动作。
const (
	ClickLightbox = "lightbox"
	ClickLink     = "link"
	ClickNone     = "none"
)

// presetRatios 统一比例预设（original 不输出 aspect-ratio）。
var presetRatios = map[string]string{
	"original": "", "1:1": "1 / 1", "4:3": "4 / 3",
	"16:9": "16 / 9", "3:2": "3 / 2", "3:4": "3 / 4",
}

// Item 单图项：URL + 单图覆盖元数据。
type Item struct {
	URL     string `json:"url"`
	Alt     string `json:"alt,omitempty"`
	Caption string `json:"caption,omitempty"`
	Link    string `json:"link,omitempty"`
	// Loading 单图加载策略三态：空=继承组件级 loading（再继承主题）/ on / off。
	Loading string `json:"loading,omitempty" ct:"select,=继承组件,on=开启懒加载,off=关闭懒加载,default=,sec=content,label=单图加载"`
	// FetchPriority 单图资源提示：空=auto（不输出）/ high=首屏优先 / low=次要。
	FetchPriority string `json:"fetchPriority,omitempty" ct:"select,=自动,high=高优先,low=低优先,default=,sec=content,label=单图优先级"`
}

// Columns 三端栅格列数。
type Columns struct {
	Desktop int `json:"desktop,omitempty"`
	Tablet  int `json:"tablet,omitempty"`
	Mobile  int `json:"mobile,omitempty"`
}

// SlidesPerView 三端单屏张数（支持小数）。
type SlidesPerView struct {
	Desktop float64 `json:"desktop,omitempty"`
	Tablet  float64 `json:"tablet,omitempty"`
	Mobile  float64 `json:"mobile,omitempty"`
}

// Carousel 轮播配置。
type Carousel struct {
	Autoplay      bool          `json:"autoplay,omitempty"`
	Interval      int           `json:"interval,omitempty"`     // ms，默认 4000
	Infinite      bool          `json:"infinite,omitempty"`     // 无限循环
	PauseOnHover  bool          `json:"pauseOnHover,omitempty"` // 悬停暂停
	SlidesPerView SlidesPerView `json:"slidesPerView,omitempty"`
	Arrows        bool          `json:"arrows,omitempty"`
	Dots          bool          `json:"dots,omitempty"`
	// FirstEagerOff 关闭「首图优先加载」（默认开启）：轮播首图立即加载并置
	// fetchpriority=high 以压 LCP；关闭后首图跟随组件级/主题的懒加载设置。
	FirstEagerOff bool `json:"firstEagerOff,omitempty" ct:"bool,sec=content,label=关闭首图优先加载"`
}

// Grid 栅格配置。
type Grid struct {
	Columns   Columns `json:"columns,omitempty"`
	ColumnGap string  `json:"columnGap,omitempty"`
	RowGap    string  `json:"rowGap,omitempty"`
}

// Hover 统一悬浮反馈。
type Hover struct {
	Scale    string `json:"scale,omitempty"`    // 如 "1.05"
	Overlay  string `json:"overlay,omitempty"`  // dark / light；空=无遮罩
	Deepen   bool   `json:"deepen,omitempty"`   // 阴影加深
	Duration string `json:"duration,omitempty"` // 如 "300ms"
}

// Binding CMS 图集字段绑定。
type Binding struct {
	Field       string `json:"field,omitempty"`
	Fallback    bool   `json:"fallback,omitempty"`    // 空时隐藏组件（默认）
	Placeholder string `json:"placeholder,omitempty"` // fallback=false 时使用的占位图 URL
}

// Props gallery 属性：数据源 + 展示模式 + 统一样式 + 交互 + Advanced。
type Props struct {
	// Items 静态图集（与 Binding 二选一）。
	Items []Item `json:"items,omitempty"`
	// Binding CMS 图集字段绑定。
	Binding *Binding `json:"binding,omitempty"`
	// Mode 展示模式：grid / carousel（默认 grid）。
	Mode string `json:"mode,omitempty" ct:"select,grid=网格,carousel=轮播,default=grid,sec=content,label=展示模式"`
	// Grid 栅格配置。
	Grid Grid `json:"grid,omitempty"`
	// Carousel 轮播配置。
	Carousel Carousel `json:"carousel,omitempty"`

	// --- 全局统一样式 ---
	AspectRatio string `json:"aspectRatio,omitempty" ct:"select,original=原图,1:1=1:1,4:3=4:3,16:9=16:9,3:2=3:2,3:4=3:4,default=original,sec=style,label=宽高比"`
	ObjectFit   string `json:"objectFit,omitempty" ct:"select,cover=铺满裁剪,contain=完整包含,default=cover,sec=style,label=填充方式"`
	Radius      string `json:"radius,omitempty" ct:"dimension,maxlen=30,sec=style"`
	BorderWidth string `json:"borderWidth,omitempty" ct:"dimension,maxlen=20,sec=style"`
	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=100,sec=style"`
	Hover       Hover  `json:"hover,omitempty"`

	// --- 点击动作与图注 ---
	ClickAction string `json:"clickAction,omitempty" ct:"select,lightbox=灯箱放大,link=打开链接,none=无,default=lightbox,sec=content,label=点击动作"`
	DefaultLink string `json:"defaultLink,omitempty" ct:"url,sec=content"`
	CaptionMode string `json:"captionMode,omitempty" ct:"select,none=不显示,below=图下方,hover=悬停显示,default=none,sec=style,label=说明方式"`

	// Loading 图片加载策略三态（组件级，作用于图集内全部图片）：
	// 空=默认（继承主题「图片管理」）/ on=开启懒加载 / off=关闭。
	Loading string `json:"loading,omitempty" ct:"select,=默认（继承主题）,on=开启懒加载,off=关闭懒加载,lazy=懒加载（旧）,eager=立即加载（旧）,default=,sec=content,label=图片加载"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
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
		// Translatable 可翻译字段白名单（多语言 P5b，docs/06-D §7.5 决策 F6）：
		// 只有这里列出的字段参与内容翻译，未声明字段永不翻译。
		Translatable: []string{"alt", "caption"},
	},
}

var (
	fieldPathRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-zA-Z][a-zA-Z0-9_]*$`)
)

// validateExtra 关系性校验。
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

// parseValues 解析绑定值：字符串数组 / 对象数组 / 逗号分隔。
func parseValues(v string) (items []Item, err error) {
	var raw []json.RawMessage
	if json.Unmarshal([]byte(v), &raw) != nil {
		// 逗号分隔兜底。
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

// 单图项渲染视图逻辑已迁移至 jet.go 的 buildItemView + gallery.jet 模板
//（HTML 结构下沉 .jet，Jet 默认转义），旧 renderItem 手拼 HTML 已删除。

// galleryCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组，
// 而作用域替换、三端分桶、容器查询分层与确定性输出仍由构建期负责。
//
//go:embed gallery.css
var galleryCSS string

// compileCSS 图集样式：Grid 三端/间距、Carousel 骨架、统一样式（比例/适配/圆角/边框/悬浮）、图注。
//
// 只做一件事：把属性翻译成样式源的变量。分支结构（哪种模式 / 哪端列数 / 有没有悬浮）
// 全部在 gallery.css 里用 @if 描述 —— 因此这里不再拼声明切片，也不必用 if 包住 b.Add。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 列数：0 视为「该端不单独设列数」（变量为空 → 整段规则不产出）；
	// 桌面端例外，兜底 4 列（与迁移前一致）。
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
	// 单屏宽度：slidesPerView 换算成 flex-basis 百分比（定长三位小数，确定性）。
	slideW := func(n float64) string {
		if n <= 0 {
			return ""
		}
		return fmt.Sprintf("%.3f%%", 100/n)
	}
	slideDesktop := slideW(p.Carousel.SlidesPerView.Desktop)
	if slideDesktop == "" {
		slideDesktop = "100.000%" // 默认单屏一张
	}
	// 裁剪方式只在非默认值时输出：cover 是默认值，写出来是冗余声明。
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

// slideNum slides 数值序列化（确定性，小数保留原值）。
func slideNum(n float64, def float64) string {
	if n <= 0 {
		n = def
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// isSafeURL 图片地址安全（仅 #//http(s)，有意排除 mailto/tel）。
// 比 core.IsSafeURL（允许 mailto/tel）更严：图集 URL 语义是图片 src 或点击链接，
// 不涉及邮件/电话协议。静态 Items 路径使用本函数；绑定解析路径（jet.go）按统一
// 协议白名单走 core.IsSafeURL。
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

// defaultDur 悬浮覆盖层的默认时长。
func defaultDur(d string) string {
	if d == "" {
		return "300ms"
	}
	return d
}

// init 注册画廊组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("gallery", galleryTemplate)
	// 每个增强块独立注册：命中任一特征只注入它自己 —— 合并注册会让「用了轮播」的页面白背灯箱代码。
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCarousels"},
		Feats:  []string{"data-carousel"},
		Source: enhanceCarouselJS,
	})
	// 每个增强块独立注册：命中任一特征只注入它自己 —— 合并注册会让「用了轮播」的页面白背灯箱代码。
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initLightboxes"},
		Feats:  []string{"data-lightbox"},
		Source: enhanceLightboxJS,
	})
}

// galleryTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed gallery.jet
var galleryTemplate string
