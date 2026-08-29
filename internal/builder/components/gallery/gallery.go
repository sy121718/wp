// Package gallery 实现 core.gallery 图集与画廊组件（规范《02-C4 图集与画廊组件规范》）。
//
// 数据源：静态图集（assetId 列表 + 单图 alt/caption/link 覆盖）或 CMS 图集字段绑定
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
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	"go_wp/internal/builder/media"
)

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

// Item 单图项：assetId + 单图覆盖元数据。
type Item struct {
	AssetID string `json:"assetId"`
	Alt     string `json:"alt,omitempty"`
	Caption string `json:"caption,omitempty"`
	Link    string `json:"link,omitempty"`
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
	Placeholder string `json:"placeholder,omitempty"` // fallback=false 时使用的占位图 assetId
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
	Radius      string `json:"radius,omitempty" ct:"safe,maxlen=30,sec=style"`
	BorderWidth string `json:"borderWidth,omitempty" ct:"safe,maxlen=20,sec=style"`
	BorderColor string `json:"borderColor,omitempty" ct:"safe,maxlen=100,sec=style"`
	Hover       Hover  `json:"hover,omitempty"`

	// --- 点击动作与图注 ---
	ClickAction string `json:"clickAction,omitempty" ct:"select,lightbox=灯箱放大,link=打开链接,none=无,default=lightbox,sec=content,label=点击动作"`
	DefaultLink string `json:"defaultLink,omitempty" ct:"url,sec=content"`
	CaptionMode string `json:"captionMode,omitempty" ct:"select,none=不显示,below=图下方,hover=悬停显示,default=none,sec=style,label=说明方式"`

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

var (
	assetIDRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{4,64}$`)
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
	if p.Binding != nil && p.Binding.Placeholder != "" && !assetIDRe.MatchString(p.Binding.Placeholder) {
		return fmt.Errorf("无效的占位图 assetId: %q", p.Binding.Placeholder)
	}

	for i := range p.Items {
		if !assetIDRe.MatchString(p.Items[i].AssetID) {
			return fmt.Errorf("第 %d 张图无效的 assetId: %q", i+1, p.Items[i].AssetID)
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

// resolvedItem 解析后的单图。
type resolvedItem struct {
	asset Item
	meta  *core.MediaMeta
}

// render 数据源解析 → 逐项媒体解析 → Grid/Carousel 组装。
func render(node *core.Node, p *Props, h *core.AtomRender) (string, error) {
	if p.Mode == "" {
		p.Mode = LayoutGrid
	}

	items, err := resolveSource(p, h)
	if err != nil {
		return "", err
	}
	if items == nil {
		return "", nil // 空图集 + 无占位：组件隐藏（Fallback 默认）
	}

	var resolved []resolvedItem
	for i := range items {
		if h.Media == nil {
			return "", fmt.Errorf("编译上下文缺少媒体解析器")
		}
		meta, err := h.Media.ResolveMedia(items[i].AssetID, media.VariantLarge)
		if err != nil {
			return "", fmt.Errorf("第 %d 张图解析失败: %w", i+1, err)
		}
		if meta.Type != core.MediaTypeImage && meta.Type != core.MediaTypeSVG {
			return "", fmt.Errorf("第 %d 张图不是图片类型: %s", i+1, meta.Type)
		}
		resolved = append(resolved, resolvedItem{asset: items[i], meta: meta})
	}

	var body strings.Builder
	switch p.Mode {
	case LayoutCarousel:
		body.WriteString(renderCarousel(node.ID, p, resolved))
	default:
		body.WriteString(renderGrid(p, resolved))
	}

	var out strings.Builder
	out.WriteString(`<div class="`)
	out.WriteString(h.Classes)
	out.WriteString(`"`)
	if h.CustomID != "" {
		out.WriteString(` id="`)
		out.WriteString(h.CustomID)
		out.WriteString(`"`)
	}
	out.WriteString(">")
	out.WriteString(body.String())
	out.WriteString("</div>")

	compileCSS(node.ID, p, h.CSS)
	return out.String(), nil
}

// resolveSource 图集数据源：绑定优先（JSON 数组/对象数组/逗号分隔），静态兜底；空+无占位返回 nil。
func resolveSource(p *Props, h *core.AtomRender) (items []Item, err error) {
	if p.Binding == nil || p.Binding.Field == "" {
		return p.Items, nil
	}
	if h.Content == nil {
		return nil, fmt.Errorf("编译上下文缺少内容解析器")
	}
	v, err := h.Content.ResolveString(p.Binding.Field)
	if err != nil {
		return nil, fmt.Errorf("解析绑定 %q 失败: %w", p.Binding.Field, err)
	}
	if v != "" {
		return parseValues(v)
	}
	if p.Binding.Placeholder != "" {
		return []Item{{AssetID: p.Binding.Placeholder}}, nil
	}
	return nil, nil // 隐藏组件
}

// parseValues 解析绑定值：字符串数组 / 对象数组 / 逗号分隔。
func parseValues(v string) (items []Item, err error) {
	var raw []json.RawMessage
	if json.Unmarshal([]byte(v), &raw) != nil {
		// 逗号分隔兜底。
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				items = append(items, Item{AssetID: s})
			}
		}
		return items, nil
	}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			items = append(items, Item{AssetID: s})
			continue
		}
		var it Item
		if json.Unmarshal(r, &it) == nil {
			items = append(items, it)
		}
	}
	return items, nil
}

// renderGrid 网格模式：纯 CSS Grid 直出。
func renderGrid(p *Props, list []resolvedItem) string {
	var sb strings.Builder
	for _, r := range list {
		sb.WriteString(renderItem(p, r))
	}
	return sb.String()
}

// renderCarousel 轮播骨架：语义静态结构 + data-carousel 增强属性。
func renderCarousel(galleryID string, p *Props, list []resolvedItem) string {
	c := p.Carousel
	interval := c.Interval
	if interval == 0 {
		interval = 4000
	}
	attr := fmt.Sprintf(`data-carousel='{"autoplay":%t,"interval":%d,"infinite":%t,"pauseOnHover":%t,"slidesPerView":{"desktop":%s,"tablet":%s,"mobile":%s}}'`,
		c.Autoplay, interval, c.Infinite, c.PauseOnHover,
		slideNum(c.SlidesPerView.Desktop, 1), slideNum(c.SlidesPerView.Tablet, 1), slideNum(c.SlidesPerView.Mobile, 1))

	var sb strings.Builder
	sb.WriteString(`<div class="gallery-carousel" `)
	sb.WriteString(attr)
	sb.WriteString(`>`)
	sb.WriteString(`<div class="gallery-track">`)
	for _, r := range list {
		sb.WriteString(`<div class="gallery-slide">`)
		sb.WriteString(renderItem(p, r))
		sb.WriteString(`</div>`)
	}
	sb.WriteString(`</div>`)
	if c.Arrows {
		sb.WriteString(`<button class="gallery-prev" aria-label="上一张" type="button">‹</button>`)
		sb.WriteString(`<button class="gallery-next" aria-label="下一张" type="button">›</button>`)
	}
	if c.Dots {
		sb.WriteString(`<div class="gallery-dots" aria-hidden="true"></div>`)
	}
	sb.WriteString(`</div>`)
	return sb.String()
}

// renderItem 单图项：img + 点击动作包裹 + 图注结构。
func renderItem(p *Props, r resolvedItem) string {
	imgHTML, err := media.RenderImageHTML(r.meta, r.asset.Alt, "", media.ImageHTMLOptions{
		Class: "gi",
		Sizes: "(max-width: 768px) 100vw, 300px",
	})
	if err != nil {
		return ""
	}

	href := r.asset.Link
	if href == "" {
		href = p.DefaultLink
	}
	var item string
	switch {
	case p.ClickAction == ClickLightbox && href == "":
		// 默认相册灯箱：点击打开原图（客户端增强脚本接管为滑动相册；无脚本时浏览器直开图片）。
		item = `<a href="` + html.EscapeString(r.meta.URL) + `" data-lightbox="wp-g-` + "" + `">` + imgHTML + `</a>`
	case (p.ClickAction == ClickLink || r.asset.Link != "") && href != "":
		item = `<a href="` + html.EscapeString(href) + `">` + imgHTML + `</a>`
	default:
		item = imgHTML
	}

	switch p.CaptionMode {
	case CaptionBelow, CaptionHover:
		caption := r.asset.Caption
		if caption == "" {
			caption = r.meta.Caption
		}
		if caption != "" {
			return `<figure>` + item + `<figcaption>` + html.EscapeString(caption) + `</figcaption></figure>`
		}
	}
	return item
}

// compileCSS 图集样式：Grid 三端/间距、Carousel 骨架、统一样式（比例/适配/圆角/边框/悬浮）、图注。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	// 统一子图样式。
	var base []string
	if ar, ok := presetRatios[p.AspectRatio]; ok && ar != "" {
		base = append(base, "aspect-ratio: "+ar)
	}
	if p.ObjectFit != "" && p.ObjectFit != "cover" {
		base = append(base, "object-fit: "+p.ObjectFit)
	}
	if p.Radius != "" {
		base = append(base, "border-radius: "+p.Radius)
	}
	if p.BorderWidth != "" && p.BorderColor != "" {
		base = append(base, "border: "+p.BorderWidth+" solid "+p.BorderColor)
	}
	b.Add(core.BreakpointDesktop, sel+" .gi", base)

	// Grid 模式：三端列数与间距。
	if p.Mode == LayoutGrid {
		c := p.Grid.Columns
		gridDecl := func(n int) string {
			if n <= 0 {
				n = 4 // 默认 4 列
			}
			return fmt.Sprintf("display: grid; grid-template-columns: repeat(%d, 1fr)", n)
		}
		gaps := func() []string {
			var g []string
			if p.Grid.ColumnGap != "" {
				g = append(g, "column-gap: "+p.Grid.ColumnGap)
			}
			if p.Grid.RowGap != "" {
				g = append(g, "row-gap: "+p.Grid.RowGap)
			}
			return g
		}
		b.Add(core.BreakpointDesktop, sel, append(splitDecls(gridDecl(c.Desktop)), gaps()...))
		if c.Tablet > 0 {
			b.Add(core.BreakpointTablet, sel, splitDecls(gridDecl(c.Tablet)))
		}
		if c.Mobile > 0 {
			b.Add(core.BreakpointMobile, sel, splitDecls(gridDecl(c.Mobile)))
		}
	}

	// Carousel 骨架：横向滚动 + 滚动吸附 + 单屏宽度（按 slidesPerView）。
	if p.Mode == LayoutCarousel {
		c := p.Carousel
		b.Add(core.BreakpointDesktop, sel+" .gallery-track", []string{
			"display: flex",
			"overflow-x: auto",
			"scroll-snap-type: x mandatory",
			"scrollbar-width: none",
		})
		if p.Grid.ColumnGap != "" {
			b.Add(core.BreakpointDesktop, sel+" .gallery-track", []string{"column-gap: " + p.Grid.ColumnGap, "gap: " + p.Grid.ColumnGap})
		}
		slideW := func(n float64) string {
			if n <= 0 {
				n = 1
			}
			// 基础宽度百分比（剩余给 gap 由浏览器按 flex 分配，用 basis 近似）。
			return fmt.Sprintf("flex: 0 0 %.3f%%", 100/n)
		}
		// 默认 desktop = slides 或 1。
		b.Add(core.BreakpointDesktop, sel+" .gallery-slide", []string{
			"scroll-snap-align: start",
			slideW(c.SlidesPerView.Desktop),
		})
		if c.SlidesPerView.Tablet > 0 {
			b.Add(core.BreakpointTablet, sel+" .gallery-slide", splitDecls(slideW(c.SlidesPerView.Tablet)))
		}
		if c.SlidesPerView.Mobile > 0 {
			b.Add(core.BreakpointMobile, sel+" .gallery-slide", splitDecls(slideW(c.SlidesPerView.Mobile)))
		}
		b.Add(core.BreakpointDesktop, sel+" .gallery-prev, "+sel+" .gallery-next", []string{
			"position: absolute", "top: 50%", "transform: translateY(-50%)",
			"z-index: 2", "width: 36px", "height: 36px",
			"border-radius: 50%", "border: none",
		})
		b.Add(core.BreakpointDesktop, sel+" .gallery-dots", []string{
			"display: flex", "justify-content: center", "gap: 6px", "margin-top: 8px",
		})
	}

	// 统一悬浮反馈：过渡 + hover 缩放/遮罩/阴影加深。
	if p.Hover.Scale != "" || p.Hover.Overlay != "" || p.Hover.Deepen {
		duration := p.Hover.Duration
		if duration == "" {
			duration = "300ms"
		}
		b.Add(core.BreakpointDesktop, sel+" .gi", []string{
			"transition: transform " + duration + " ease, box-shadow " + duration + " ease, filter " + duration + " ease",
		})
		var hoverDecls []string
		if p.Hover.Scale != "" {
			hoverDecls = append(hoverDecls, "transform: scale("+p.Hover.Scale+")")
		}
		if p.Hover.Deepen {
			hoverDecls = append(hoverDecls, "box-shadow: 0 10px 28px rgba(0,0,0,0.16)")
		}
		if len(hoverDecls) > 0 {
			b.Add(core.BreakpointDesktop, sel+" .gi:hover", hoverDecls)
		}
	}
	// 遮罩：::after 覆盖层（纯 CSS）。
	if p.Hover.Overlay != "" {
		var overlayColor string
		if p.Hover.Overlay == "dark" {
			overlayColor = "rgba(0,0,0,0.35)"
		} else {
			overlayColor = "rgba(255,255,255,0.35)"
		}
		b.Add(core.BreakpointDesktop, sel+" .gi", []string{"position: relative"})
		b.Add(core.BreakpointDesktop, sel+" .gi::after", []string{
			"content: \"\"",
			"position: absolute", "inset: 0",
			"background: transparent",
			"transition: background " + defaultDur(p.Hover.Duration) + " ease",
		})
		b.Add(core.BreakpointDesktop, sel+" .gi:hover::after", []string{"background: " + overlayColor})
	}

	// 图注 hover 模式：常显于底部滑出。
	if p.CaptionMode == CaptionHover {
		b.Add(core.BreakpointDesktop, sel+" figcaption", []string{
			"position: absolute", "left: 0", "right: 0", "bottom: 0",
			"padding: 8px", "background: rgba(0,0,0,0.55)", "color: #fff",
			"opacity: 0", "transition: opacity " + defaultDur(p.Hover.Duration) + " ease",
		})
		b.Add(core.BreakpointDesktop, sel+" figure", []string{"position: relative", "overflow: hidden"})
		b.Add(core.BreakpointDesktop, sel+" figure:hover figcaption", []string{"opacity: 1"})
	}
}

// splitDecls "a: b; c: d" → 声明列表。
func splitDecls(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, d := range strings.Split(s, ";") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// slideNum slides 数值序列化（确定性，小数保留原值）。
func slideNum(n float64, def float64) string {
	if n <= 0 {
		n = def
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// isSafeURL 链接安全（与 core url 控件同规则）。
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
}
