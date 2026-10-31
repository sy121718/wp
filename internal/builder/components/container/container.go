package container

import (
	_ "embed"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

const Type = "core.container"

const (
	EngineFlex = "flex"
	EngineGrid = "grid"
)

var (
	nodeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

var allowedContainerTags = map[string]bool{
	"div": true, "section": true, "article": true, "aside": true,
	"nav": true, "header": true, "footer": true, "main": true,
}

var justifyMap = map[string]string{

	"flex-start": "flex-start", "center": "center", "flex-end": "flex-end",
	"space-between": "space-between", "space-around": "space-around", "space-evenly": "space-evenly",

	"start": "flex-start", "end": "flex-end",
	"between": "space-between", "around": "space-around", "evenly": "space-evenly",
}

var alignMap = map[string]string{
	"stretch": "stretch", "center": "center", "baseline": "baseline",
	"flex-start": "flex-start", "flex-end": "flex-end",
	"start": "flex-start", "end": "flex-end",
}

var allowedFlexDirection = map[string]bool{
	"row": true, "row-reverse": true, "column": true, "column-reverse": true,
}

var allowedOverflow = map[string]bool{
	"visible": true, "hidden": true, "scroll": true, "auto": true,
}

var allowedBorderStyle = map[string]bool{
	"solid": true, "dashed": true, "dotted": true, "double": true,
}

var shadowLevels = map[string]string{
	"sm": "0 1px 3px rgba(0,0,0,0.12)",
	"md": "0 4px 12px rgba(0,0,0,0.12)",
	"lg": "0 10px 28px rgba(0,0,0,0.16)",
	"xl": "0 20px 48px rgba(0,0,0,0.2)",
}

type Responsive struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

type ResponsiveInt struct {
	Desktop int `json:"desktop,omitempty"`
	Tablet  int `json:"tablet,omitempty"`
	Mobile  int `json:"mobile,omitempty"`
}

type Props struct {
	Tag    string      `json:"tag"`
	Layout LayoutProps `json:"layout"`

	Box BoxProps `json:"box" ct:"group"`

	Visual      VisualProps           `json:"visual" ct:"group"`
	Interaction core.InteractionProps `json:"interaction"`

	Position PositionProps `json:"position,omitempty" ct:"group"`

	StyleEx StyleExProps `json:"styleEx,omitempty"`
}

type PositionProps struct {
	Type string `json:"type,omitempty" ct:"select,static=默认,relative=相对,absolute=绝对,sticky=粘性,drawer=抽屉,sec=layout,label=定位方式"`

	Top    string `json:"top,omitempty" ct:"dimension,maxlen=20,sec=layout,label=上偏移"`
	Right  string `json:"right,omitempty" ct:"dimension,maxlen=20,sec=layout,label=右偏移"`
	Bottom string `json:"bottom,omitempty" ct:"dimension,maxlen=20,sec=layout,label=下偏移"`
	Left   string `json:"left,omitempty" ct:"dimension,maxlen=20,sec=layout,label=左偏移"`

	DrawerSide string `json:"drawerSide,omitempty" ct:"select,left=左侧,right=右侧,bottom=底部,sec=layout,label=抽屉方向"`

	DrawerOverlay bool `json:"drawerOverlay,omitempty" ct:"bool,sec=layout,label=抽屉遮罩"`

	DrawerTriggerID string `json:"drawerTriggerId,omitempty" ct:"safe,maxlen=64,sec=layout,label=抽屉触发 ID"`
}

type StyleExProps struct {
	BackgroundHover string `json:"backgroundHover,omitempty"`

	Overlay string `json:"overlay,omitempty"`

	ShapeDivider string `json:"shapeDivider,omitempty"`

	ShapeDividerPosition string `json:"shapeDividerPosition,omitempty"`

	SafeAreaBottom bool `json:"safeAreaBottom,omitempty" ct:"bool,sec=background,label=底部安全区垫高"`

	Reveal string `json:"reveal,omitempty" ct:"select,=跟随页面,on=子树滚动显现,off=子树直接显示,sec=background,label=滚动显现"`

	CardLayout string `json:"cardLayout,omitempty" ct:"select,=默认,horizontal=卡片横排,sec=layout,label=内部卡片布局"`

	ContainerQuery bool `json:"containerQuery,omitempty" ct:"bool,sec=layout,label=容器查询上下文"`

	ContentVisibility bool `json:"contentVisibility,omitempty" ct:"bool,sec=background,label=视口外跳过渲染"`

	Order int `json:"order,omitempty"`

	GroupParent bool `json:"groupParent,omitempty"`

	Attributes []AttributeKV `json:"attributes,omitempty"`
}

type AttributeKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type LayoutProps struct {
	Engine string     `json:"engine"`
	Flex   *FlexProps `json:"flex,omitempty"`
	Grid   *GridProps `json:"grid,omitempty"`
}

type FlexProps struct {
	Direction string `json:"direction,omitempty"`

	Justify string `json:"justify,omitempty"`

	Align string `json:"align,omitempty"`

	Wrap bool `json:"wrap,omitempty"`

	Gap string `json:"gap,omitempty"`
}

type GridProps struct {
	Columns ResponsiveInt `json:"columns,omitempty"`

	ColumnGap string `json:"columnGap,omitempty"`

	RowGap string `json:"rowGap,omitempty"`
}

type BoxProps struct {
	Padding Responsive `json:"padding,omitempty" ct:"boxspacing,sec=layout,label=内距"`

	Margin Responsive `json:"margin,omitempty" ct:"boxspacing,sec=layout,label=外距"`

	MinHeight string `json:"minHeight,omitempty" ct:"dimension,maxlen=20,sec=layout,label=最小高度"`

	MaxHeight string `json:"maxHeight,omitempty" ct:"dimension,maxlen=20,sec=layout,label=最大高度"`

	MaxWidth string `json:"maxWidth,omitempty" ct:"dimension,maxlen=30,sec=layout,label=最大宽度"`

	Width string `json:"width,omitempty" ct:"dimension,maxlen=30,sec=layout,label=宽度"`

	Center bool `json:"center,omitempty" ct:"bool,sec=layout,label=水平居中"`

	Overflow string `json:"overflow,omitempty" ct:"select,visible=可见,hidden=隐藏,scroll=滚动,auto=自动,sec=layout,label=溢出处理"`
}

type VisualProps struct {
	BgColor    string `json:"bgColor,omitempty" ct:"color,maxlen=200,sec=background,label=背景色"`
	BgGradient string `json:"bgGradient,omitempty" ct:"safe,maxlen=200,sec=background,label=背景渐变"`

	Pattern string `json:"pattern,omitempty" ct:"select,=无,dots=点阵,grid=网格,overlay=细网格,diagonal-stripes=斜纹,diagonal-lines=细斜线,vertical-stripes=竖纹,horizontal-stripes=横纹,zigzag=锯齿,checkerboard=棋盘,triangles=三角,diamond=菱形,crosses=十字,plus=加号,squares=方块,circles=大圆点,polka=交错波点,ripple=同心波纹,bricks=砖块,rain=雨丝,honeycomb=蜂窝,sec=background,label=图案背景"`

	PatternColor string `json:"patternColor,omitempty" ct:"color,maxlen=200,sec=background,label=图案颜色"`

	BgGradientAnimated bool   `json:"bgGradientAnimated,omitempty" ct:"bool,sec=background,label=渐变流动"`
	BgImage            string `json:"bgImage,omitempty" ct:"media,sec=background,label=背景图片"`

	BgSlides []string `json:"bgSlides,omitempty" ct:"mediaList,sec=background,label=背景轮播图"`

	BgSlideInterval string `json:"bgSlideInterval,omitempty" ct:"number,min=1,max=60,sec=background,label=轮播间隔(s)"`

	BgPosition string `json:"bgPosition,omitempty" ct:"select,default=（默认）,custom=自定义,center=居中,center top=中上,center bottom=中下,left top=左上,left center=左中,left bottom=左下,right top=右上,right center=右中,right bottom=右下,default=default,sec=background,label=背景定位"`

	BgPositionXY string `json:"bgPositionXY,omitempty" ct:"safe,maxlen=40,sec=background,label=自定义定位值"`

	BgAttachment string `json:"bgAttachment,omitempty" ct:"select,default=（默认）,scroll=随页面滚动,fixed=固定（视差）,local=随内容滚动,default=default,sec=background,label=背景附着方式"`

	BgRepeat string `json:"bgRepeat,omitempty" ct:"select,default=（默认）,no-repeat=不重复,repeat=平铺,repeat-x=横向平铺,repeat-y=纵向平铺,default=default,sec=background,label=背景重复"`

	BgSize string `json:"bgSize,omitempty" ct:"select,default=（默认）,auto=原始,contain=完整包含,cover=铺满裁剪,custom=自定义,default=default,sec=background,label=显示尺寸"`

	BgSizeValue string `json:"bgSizeValue,omitempty" ct:"safe,maxlen=40,sec=background,label=自定义尺寸值"`

	BorderWidth string `json:"borderWidth,omitempty" ct:"dimension,maxlen=20,sec=border,label=边框宽度"`
	BorderStyle string `json:"borderStyle,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,sec=border,label=边框样式"`
	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=200,sec=border,label=边框颜色"`

	Radius string `json:"radius,omitempty" ct:"dimension,maxlen=20,sec=border,label=圆角"`

	RadiusTL string `json:"radiusTL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左上圆角"`
	RadiusTR string `json:"radiusTR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右上圆角"`
	RadiusBR string `json:"radiusBR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右下圆角"`
	RadiusBL string `json:"radiusBL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左下圆角"`

	Shadow string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,custom=自定义,sec=border,label=阴影"`

	ShadowX      string `json:"shadowX,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影 X"`
	ShadowY      string `json:"shadowY,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影 Y"`
	ShadowBlur   string `json:"shadowBlur,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影模糊"`
	ShadowSpread string `json:"shadowSpread,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影扩散"`
	ShadowColor  string `json:"shadowColor,omitempty" ct:"color,maxlen=200,sec=border,label=阴影颜色"`
}

type Container struct{}

func (Container) Type() string { return Type }

func (Container) PropsSpec() any { return &Props{} }

func (Container) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"tag": "section",
			"layout": map[string]any{
				"engine": "flex",
				"flex": map[string]any{
					"direction": "column",
					"gap":       "16px",
				},
			},
			"box": map[string]any{
				"padding": map[string]any{
					"desktop": "32px",
				},
			},
		},
		DisplayName: "容器",
		Hint:        "布局容器",
	}
}

func IsSafeCSSValue(v string) bool {
	return core.IsSafeCSSValue(v)
}

var shapeDividers = map[string]string{
	"wave":  shapeDividerMarkup(core.ShapeKindWave),
	"slope": shapeDividerMarkup(core.ShapeKindSlope),
	"curve": shapeDividerMarkup(core.ShapeKindCurve),
}

func shapeDividerMarkup(kind string) string {
	return `<path fill="currentColor" d="` + core.ShapePath(kind, 0, false) + `"/>`
}

var attrKeyRe = regexp.MustCompile(`^(data-[a-z0-9-]{1,32}|aria-[a-z0-9-]{1,32}|role|title|tabindex)$`)

func attrValueSafe(v string) bool {
	if len(v) > 200 {
		return false
	}
	for _, r := range v {
		if r == '"' || r == '\'' || r == '<' || r == '>' || r == '\\' || r == '`' {
			return false
		}
	}
	return true
}

func (c Container) Validate(node *core.Node, ids map[string]bool) error {
	if err := c.ValidateSelf(node, ids); err != nil {
		return err
	}
	return core.ValidateChildren(node, ids)
}

func (c Container) ValidateSelf(node *core.Node, ids map[string]bool) (err error) {
	if !nodeIDRe.MatchString(node.ID) {
		return fmt.Errorf("无效的节点 ID: %q", node.ID)
	}
	if err = core.ValidateNodeName(node.Name); err != nil {
		return err
	}
	if ids[node.ID] {
		return fmt.Errorf("节点 ID 重复: %q", node.ID)
	}
	ids[node.ID] = true

	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err = validateProps(&p); err != nil {
		return fmt.Errorf("节点 %s: %w", node.ID, err)
	}
	return nil
}

//go:embed container.css
var containerCSS string

const cssInteractionPoint = "/* @interaction-point */"

func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	head, tail, ok := strings.Cut(containerCSS, cssInteractionPoint)
	if !ok {
		panic("container.css 缺少分段标记 " + cssInteractionPoint)
	}
	if err := core.ApplyComponentCSSTmplLists(b, sel, head, cssVarsHead(p), cssLists(p)); err != nil {
		panic(fmt.Sprintf("container 组件样式解析失败: %v", err))
	}

	core.CompileInteraction(sel, p.Interaction, b)
	if err := core.ApplyComponentCSSTmpl(b, sel, tail, cssVarsTail(p)); err != nil {
		panic(fmt.Sprintf("container 组件样式解析失败: %v", err))
	}
}

func cssVarsHead(p *Props) map[string]string {
	vars := map[string]string{

		"flexEngine":      core.BoolVar(p.Layout.Engine == EngineFlex),
		"gridEngine":      core.BoolVar(p.Layout.Engine == EngineGrid),
		"flexDirection":   "",
		"flexJustify":     "",
		"flexAlign":       "",
		"flexWrap":        "",
		"flexGap":         "",
		"gridColsDesktop": "",
		"gridColsTablet":  "",
		"gridColsMobile":  "",
		"gridColumnGap":   "",
		"gridRowGap":      "",

		"padDesktop":    p.Box.Padding.Desktop,
		"padTablet":     p.Box.Padding.Tablet,
		"padMobile":     p.Box.Padding.Mobile,
		"marginDesktop": p.Box.Margin.Desktop,
		"marginTablet":  p.Box.Margin.Tablet,
		"marginMobile":  p.Box.Margin.Mobile,
		"minHeight":     p.Box.MinHeight,
		"maxHeight":     p.Box.MaxHeight,
		"maxWidth":      p.Box.MaxWidth,
		"hasWidth":      core.BoolVar(p.Box.Width != ""),
		"width":         p.Box.Width,
		"center":        core.BoolVar(p.Box.Center && p.Box.MaxWidth != ""),
		"overflow":      p.Box.Overflow,

		"bgColor":          p.Visual.BgColor,
		"hasGradient":      "",
		"bgGradient":       p.Visual.BgGradient,
		"gradientAnimated": "",
		"bgFlow":           "",
		"hasImage":         "",
		"bgImage":          p.Visual.BgImage,
		"bgPosition":       "",
		"bgAttachment":     "",
		"bgRepeat":         "",
		"bgSize":           "",
		"hasPattern":       "",
		"patternDecls":     "",
		"hasSlides":        "",
		"slideCount":       "",
		"slideTotal":       "",

		"fadeHold":         "",
		"fadeEnd":          "",
		"hasBorder":        "",
		"borderDecl":       "",
		"hasCorners":       "",
		"radiusCorners":    "",
		"hasRadius":        "",
		"radius":           p.Visual.Radius,
		"shadowCustom":     "",
		"shadowCustomDecl": "",
		"shadowLevel":      "",
		"shadowLevelDecl":  "",
	}

	if f := p.Layout.Flex; f != nil {
		vars["flexDirection"] = f.Direction
		vars["flexJustify"] = justifyMap[f.Justify]
		vars["flexAlign"] = alignMap[f.Align]
		if f.Wrap {
			vars["flexWrap"] = "wrap"
		}
		vars["flexGap"] = f.Gap
	}

	if g := p.Layout.Grid; g != nil {
		vars["gridColsDesktop"] = repeatCount(g.Columns.Desktop)
		vars["gridColsTablet"] = repeatCount(g.Columns.Tablet)
		vars["gridColsMobile"] = repeatCount(g.Columns.Mobile)
		vars["gridColumnGap"] = g.ColumnGap
		vars["gridRowGap"] = g.RowGap
	}

	hasGradient := p.Visual.BgGradient != ""
	hasImage := !hasGradient && p.Visual.BgImage != ""
	hasPattern := !hasGradient && p.Visual.BgImage == "" && p.Visual.Pattern != ""
	vars["hasGradient"] = core.BoolVar(hasGradient)
	vars["gradientAnimated"] = core.BoolVar(p.Visual.BgGradientAnimated)
	if hasGradient && p.Visual.BgGradientAnimated {

		vars["bgFlow"] = strings.Join(core.BackgroundFlowDecls(), "; ")
	}
	vars["hasImage"] = core.BoolVar(hasImage)
	vars["bgPosition"] = bgPositionValue(p)
	vars["bgAttachment"] = bgKeywordValue(p.Visual.BgAttachment)
	vars["bgRepeat"] = bgKeywordValue(p.Visual.BgRepeat)
	vars["bgSize"] = bgSizeValue(p)
	vars["hasPattern"] = core.BoolVar(hasPattern)
	if hasPattern {
		vars["patternDecls"] = strings.Join(core.BackgroundPatternDecls(p.Visual.Pattern, p.Visual.PatternColor), "; ")
	}

	if n := len(p.Visual.BgSlides); n > 0 {
		interval := slideInterval(p.Visual.BgSlideInterval)
		seg := 100 / n
		vars["hasSlides"] = core.BoolVar(true)
		vars["slideCount"] = strconv.Itoa(n)
		vars["slideTotal"] = fmt.Sprintf("%ds", n*interval)
		vars["fadeHold"] = strconv.Itoa(seg - 4)
		vars["fadeEnd"] = strconv.Itoa(seg)
	}

	if p.Visual.BorderWidth != "" || p.Visual.BorderStyle != "" || p.Visual.BorderColor != "" {
		bw := p.Visual.BorderWidth
		if bw == "" {
			bw = "1px"
		}
		bs := p.Visual.BorderStyle
		if bs == "" {
			bs = "solid"
		}
		bc := p.Visual.BorderColor
		if bc == "" {
			bc = "currentColor"
		}
		vars["hasBorder"] = core.BoolVar(true)
		vars["borderDecl"] = strings.Join([]string{bw, bs, bc}, " ")
	}

	hasCorners := p.Visual.RadiusTL != "" || p.Visual.RadiusTR != "" ||
		p.Visual.RadiusBR != "" || p.Visual.RadiusBL != ""
	vars["hasCorners"] = core.BoolVar(hasCorners)
	if hasCorners {
		fallback := p.Visual.Radius
		if fallback == "" {
			fallback = "0"
		}
		corner := func(v string) string {
			if v == "" {
				return fallback
			}
			return v
		}
		vars["radiusCorners"] = strings.Join([]string{
			corner(p.Visual.RadiusTL), corner(p.Visual.RadiusTR),
			corner(p.Visual.RadiusBR), corner(p.Visual.RadiusBL),
		}, " ")
	}
	vars["hasRadius"] = core.BoolVar(!hasCorners && p.Visual.Radius != "")

	if p.Visual.Shadow == "custom" {
		x, y, blur, spread := p.Visual.ShadowX, p.Visual.ShadowY, p.Visual.ShadowBlur, p.Visual.ShadowSpread
		if x == "" {
			x = "0"
		}
		if y == "" {
			y = "4px"
		}
		if blur == "" {
			blur = "12px"
		}
		if spread == "" {
			spread = "0"
		}
		color := p.Visual.ShadowColor
		if color == "" {
			color = "rgba(0,0,0,.12)"
		}
		vars["shadowCustom"] = core.BoolVar(true)
		vars["shadowCustomDecl"] = strings.Join([]string{x, y, blur, spread, color}, " ")
	}
	if v, ok := shadowLevels[p.Visual.Shadow]; p.Visual.Shadow != "" && ok {
		vars["shadowLevel"] = core.BoolVar(true)
		vars["shadowLevelDecl"] = v
	}
	return vars
}

func cssLists(p *Props) map[string][]map[string]string {
	items := make([]map[string]string, 0, len(p.Visual.BgSlides))
	if n := len(p.Visual.BgSlides); n > 0 {
		interval := slideInterval(p.Visual.BgSlideInterval)
		for i := range p.Visual.BgSlides {
			items = append(items, map[string]string{
				"index": strconv.Itoa(i + 1),
				"delay": strconv.Itoa(i * interval),
			})
		}
	}
	return map[string][]map[string]string{"slides": items}
}

func cssVarsTail(p *Props) map[string]string {
	order := ""
	if p.StyleEx.Order != 0 {
		order = strconv.Itoa(p.StyleEx.Order)
	}
	safeArea := ""
	if p.StyleEx.SafeAreaBottom {
		safeArea = strings.Join(core.SafeAreaDecls("bottom"), "; ")
	}

	shapePos := "bottom"
	if p.StyleEx.ShapeDividerPosition == "top" {
		shapePos = "top"
	}
	return map[string]string{
		"posRelative":          core.BoolVar(p.Position.Type == "relative"),
		"posAbsolute":          core.BoolVar(p.Position.Type == "absolute"),
		"posSticky":            core.BoolVar(p.Position.Type == "sticky"),
		"posDrawer":            core.BoolVar(p.Position.Type == "drawer"),
		"posStatic":            core.BoolVar(p.Position.Type == "static"),
		"posTop":               p.Position.Top,
		"posRight":             p.Position.Right,
		"posBottom":            p.Position.Bottom,
		"posLeft":              p.Position.Left,
		"drawerLeft":           core.BoolVar(p.Position.DrawerSide == "left"),
		"drawerRight":          core.BoolVar(p.Position.DrawerSide == "right"),
		"drawerBottom":         core.BoolVar(p.Position.DrawerSide == "bottom"),
		"order":                order,
		"hasBgHover":           core.BoolVar(p.StyleEx.BackgroundHover != ""),
		"bgHover":              p.StyleEx.BackgroundHover,
		"containerQuery":       core.BoolVar(p.StyleEx.ContainerQuery),
		"cardLayoutHorizontal": core.BoolVar(p.StyleEx.CardLayout == "horizontal"),
		"contentVisibility":    core.BoolVar(p.StyleEx.ContentVisibility),
		"safeAreaDecls":        safeArea,
		"hasOverlay":           core.BoolVar(p.StyleEx.Overlay != ""),
		"overlay":              p.StyleEx.Overlay,
		"hasShape":             core.BoolVar(p.StyleEx.ShapeDivider != ""),
		"shapePos":             shapePos,
		"shapeColor":           shapeColor(p),
	}
}

func repeatCount(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func slideInterval(raw string) int {
	v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(raw), "s"))
	if err != nil || v <= 0 || v > 60 {
		return 6
	}
	return v
}

func bgPositionValue(p *Props) string {
	switch v := p.Visual.BgPosition; {
	case v == "" || v == "default":
		return ""
	case v == "custom":
		return p.Visual.BgPositionXY
	default:
		return v
	}
}

func bgKeywordValue(v string) string {
	if v == "default" {
		return ""
	}
	return v
}

func bgSizeValue(p *Props) string {
	v := p.Visual.BgSize
	if v == "" || v == "default" {
		return ""
	}
	if v == "custom" {
		return p.Visual.BgSizeValue
	}
	return v
}

func shapeColor(p *Props) string {
	if p.Visual.BgColor != "" {
		return p.Visual.BgColor
	}
	return "currentColor"
}

func validateProps(p *Props) (err error) {
	if !allowedContainerTags[p.Tag] {
		return fmt.Errorf("无效的语义标签: %q", p.Tag)
	}

	switch p.Layout.Engine {
	case EngineFlex:
		if p.Layout.Flex == nil {
			return errors.New("flex 引擎必须提供 flex 参数")
		}
		f := p.Layout.Flex
		if f.Direction != "" && !allowedFlexDirection[f.Direction] {
			return fmt.Errorf("无效的主轴方向: %q", f.Direction)
		}
		if f.Justify != "" {
			if _, ok := justifyMap[f.Justify]; !ok {
				return fmt.Errorf("无效的主轴对齐: %q", f.Justify)
			}
		}
		if f.Align != "" {
			if _, ok := alignMap[f.Align]; !ok {
				return fmt.Errorf("无效的交叉轴对齐: %q", f.Align)
			}
		}
		if f.Gap != "" && !IsSafeCSSValue(f.Gap) {
			return fmt.Errorf("无效的子元素间距: %q", f.Gap)
		}
	case EngineGrid:
		if p.Layout.Grid == nil {
			return errors.New("grid 引擎必须提供 grid 参数")
		}
		g := p.Layout.Grid
		if g.Columns.Desktop < 1 || g.Columns.Desktop > 12 {
			return fmt.Errorf("桌面端栅格列数必须在 1~12 之间: %d", g.Columns.Desktop)
		}
		for bp, n := range map[string]int{"tablet": g.Columns.Tablet, "mobile": g.Columns.Mobile} {
			if n != 0 && (n < 1 || n > 12) {
				return fmt.Errorf("%s 端栅格列数必须在 1~12 之间: %d", bp, n)
			}
		}
		if g.ColumnGap != "" && !IsSafeCSSValue(g.ColumnGap) {
			return fmt.Errorf("无效的列间距: %q", g.ColumnGap)
		}
		if g.RowGap != "" && !IsSafeCSSValue(g.RowGap) {
			return fmt.Errorf("无效的行间距: %q", g.RowGap)
		}
	default:
		return fmt.Errorf("无效的排版引擎: %q", p.Layout.Engine)
	}

	for name, v := range map[string]string{
		"desktop 内边距": p.Box.Padding.Desktop,
		"tablet 内边距":  p.Box.Padding.Tablet,
		"mobile 内边距":  p.Box.Padding.Mobile,
		"desktop 外边距": p.Box.Margin.Desktop,
		"tablet 外边距":  p.Box.Margin.Tablet,
		"mobile 外边距":  p.Box.Margin.Mobile,
		"最小高度":        p.Box.MinHeight,
		"最大高度":        p.Box.MaxHeight,
	} {
		if v != "" && !IsSafeCSSValue(v) {
			return fmt.Errorf("无效的%s: %q", name, v)
		}
	}
	if p.Box.Overflow != "" && !allowedOverflow[p.Box.Overflow] {
		return fmt.Errorf("无效的溢出处理: %q", p.Box.Overflow)
	}

	for _, item := range []struct{ name, v string }{
		{"背景颜色", p.Visual.BgColor},
		{"背景渐变", p.Visual.BgGradient},
		{"背景图", p.Visual.BgImage},
		{"背景自定义定位", p.Visual.BgPositionXY},
		{"阴影 X", p.Visual.ShadowX},
		{"阴影 Y", p.Visual.ShadowY},
		{"阴影模糊", p.Visual.ShadowBlur},
		{"阴影扩散", p.Visual.ShadowSpread},
		{"阴影颜色", p.Visual.ShadowColor},
		{"背景自定义尺寸", p.Visual.BgSizeValue},
		{"边框粗细", p.Visual.BorderWidth},
		{"边框颜色", p.Visual.BorderColor},
		{"圆角", p.Visual.Radius},
	} {
		if item.v != "" && !IsSafeCSSValue(item.v) {
			return fmt.Errorf("无效的%s: %q", item.name, item.v)
		}
	}

	if p.Visual.BorderStyle != "" && !allowedBorderStyle[p.Visual.BorderStyle] {
		return fmt.Errorf("无效的边框线型: %q", p.Visual.BorderStyle)
	}
	if p.Visual.Shadow != "" && p.Visual.Shadow != "custom" {
		if _, ok := shadowLevels[p.Visual.Shadow]; !ok {
			return fmt.Errorf("无效的阴影级别: %q", p.Visual.Shadow)
		}
	}

	if p.Visual.Pattern != "" && (p.Visual.BgGradient != "" || p.Visual.BgImage != "") {
		return fmt.Errorf("图案背景与渐变/背景图互斥，请只选一种")
	}
	if err := core.ValidateInteraction(p.Interaction); err != nil {
		return err
	}

	if p.StyleEx.ContentVisibility && p.Interaction.Sticky {
		return fmt.Errorf("视口外跳过渲染与滚动吸顶不可同时开启")
	}

	switch p.Position.Type {
	case "", "static":
	case "relative":
	case "absolute":

		if p.Position.Top == "" && p.Position.Right == "" && p.Position.Bottom == "" && p.Position.Left == "" {
			return fmt.Errorf("绝对定位必须提供至少一个坐标（top/right/bottom/left）")
		}
	case "sticky":

	case "drawer":
		if p.Position.DrawerSide == "" {
			return fmt.Errorf("drawer 定位必须提供滑出方向（left/right/bottom）")
		}
		if p.Position.DrawerSide != "left" && p.Position.DrawerSide != "right" && p.Position.DrawerSide != "bottom" {
			return fmt.Errorf("无效的抽屉方向: %q", p.Position.DrawerSide)
		}
		if !nodeIDRe.MatchString(p.Position.DrawerTriggerID) {
			return fmt.Errorf("抽屉必须提供唯一触发 ID")
		}
	default:
		return fmt.Errorf("无效的定位类型: %q", p.Position.Type)
	}
	for name, v := range map[string]string{
		"top 坐标": p.Position.Top, "right 坐标": p.Position.Right,
		"bottom 坐标": p.Position.Bottom, "left 坐标": p.Position.Left,
	} {
		if v != "" && !IsSafeCSSValue(v) {
			return fmt.Errorf("无效的%s: %q", name, v)
		}
	}

	if p.StyleEx.BackgroundHover != "" && !IsSafeCSSValue(p.StyleEx.BackgroundHover) {
		return fmt.Errorf("无效的悬停背景: %q", p.StyleEx.BackgroundHover)
	}
	if p.StyleEx.Overlay != "" && !IsSafeCSSValue(p.StyleEx.Overlay) {
		return fmt.Errorf("无效的背景覆盖层: %q", p.StyleEx.Overlay)
	}
	if p.StyleEx.ShapeDivider != "" {
		if _, ok := shapeDividers[p.StyleEx.ShapeDivider]; !ok {
			return fmt.Errorf("无效的形状分隔线: %q（仅 wave/slope/curve）", p.StyleEx.ShapeDivider)
		}
	}
	if p.StyleEx.ShapeDividerPosition != "" && p.StyleEx.ShapeDividerPosition != "top" && p.StyleEx.ShapeDividerPosition != "bottom" {
		return fmt.Errorf("形状分隔线位置仅支持 top/bottom: %q", p.StyleEx.ShapeDividerPosition)
	}
	if p.StyleEx.Order < -1 || p.StyleEx.Order > 99 {
		if v := p.StyleEx.Reveal; v != "" && v != "on" && v != "off" {
			return fmt.Errorf("无效的滚动显现覆盖: %q（on/off）", v)
		}
		return fmt.Errorf("顺序值必须在 -1~99 之间: %d", p.StyleEx.Order)
	}
	for _, kv := range p.StyleEx.Attributes {
		if !attrKeyRe.MatchString(kv.Key) {
			return fmt.Errorf("无效的自定义属性 key: %q（仅 data-*/aria-*/role/title/tabindex）", kv.Key)
		}
		if !attrValueSafe(kv.Value) {
			return fmt.Errorf("无效的自定义属性 value: %q", kv.Value)
		}
	}
	return nil
}

func init() {
	core.Register(Container{})
	core.RegisterTemplate("container", containerTemplate)
}

//go:embed container.jet
var containerTemplate string
