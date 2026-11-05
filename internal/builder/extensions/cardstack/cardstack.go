package cardstack

import (
	_ "embed"
	"fmt"
	json "go_wp/pkg/jsonx"
	"math"
	"strconv"
	"strings"

	"go_wp/pkg/builder/core"
)

//go:embed enhance-drag.js
var enhanceDragJS string

//go:embed enhance-deck.js
var enhanceDeckJS string

//go:embed enhance-slide.js
var enhanceSlideJS string

//go:embed cardstack.css
var cardstackCSS string

const Type = "core.cardstack"

const (
	TriggerHover  = "hover"
	TriggerScroll = "scroll"
	TriggerDrag   = "drag"

	TriggerDeck = "deck"

	TriggerSlide = "slide"
)

const (
	ShapeFan  = "fan"
	ShapeLine = "line"

	ShapeBook = "book"
)

const (
	directionHorizontal = "horizontal"
	directionVertical   = "vertical"
)

const (
	defaultCount          = 9
	defaultHueStep        = 50
	defaultSpreadAngle    = 5
	defaultSpreadDistance = 120
	defaultScaleBase      = 94
	defaultScaleStep      = 2

	bookClosedDeg = 86

	cardLiftY = 50

	cardBorder = 10

	viewportGutter = 16

	dragPerspective = 1600.0

	dragGutter = 20

	contentPad = "24px"
)

const (
	widthNum       = "240px"
	heightNum      = "320px"
	widthContent   = "360px"
	heightContent  = "240px"
	widthScroll    = "720px"
	heightScroll   = "240px"
	defaultSpacing = "26vh"
	defaultSticky  = "50%"

	defaultCollectionLimit = 6

	defaultCardGap = "10px"

	defaultCardLinkText        = "查看详情"
	defaultCollectionEmptyText = "暂无内容"

	defaultSlideHeight = "100dvh"

	defaultSlideCount = 3

	slideFitViewport = "viewport"

	slideDirectionHorizontal = "horizontal"

	deckClickNext = "next"

	dragModeCylinder = "cylinder"

	fallbackCardW = 320
	fallbackCardH = 240
)

const (
	colorPrimary = "var(--sky-c-primary, #2563eb)"
	colorSurface = "var(--sky-c-surface, #fff)"
)

type Props struct {
	Trigger string `json:"trigger,omitempty" ct:"select,hover=悬停展开,scroll=滚动堆叠,drag=拖拽旋转,deck=堆叠轮播,slide=全屏分页,default=hover,sec=content,label=触发方式"`

	Shape string `json:"shape,omitempty" ct:"select,fan=扇形展开,line=直线排开,book=翻开的书,default=fan,sec=content,label=展开形态"`

	Direction string `json:"direction,omitempty" ct:"select,horizontal=横排一行,vertical=竖排一列,default=horizontal,sec=content,label=排开方向"`

	Count int `json:"count,omitempty" ct:"slider,min=2,max=12,step=1,sec=content,label=占位卡数量"`

	Width string `json:"width,omitempty" ct:"dimension,maxlen=20,sec=content,label=卡片宽度"`

	Height string `json:"height,omitempty" ct:"dimension,maxlen=20,sec=content,label=卡片高度"`

	HueStep int `json:"hueStep,omitempty" ct:"slider,min=1,max=120,step=1,sec=style,label=色相步长(deg)"`

	SpreadAngle int `json:"spreadAngle,omitempty" ct:"slider,min=1,max=15,step=1,sec=motion,label=展开角度(deg)"`

	SpreadDistance int `json:"spreadDistance,omitempty" ct:"slider,min=1,max=200,step=1,sec=motion,label=展开平移(px)"`

	DeckDirection string `json:"deckDirection,omitempty" ct:"select,horizontal=横向切换,vertical=纵向切换,default=horizontal,sec=motion,label=切换方向"`

	HoverEffect string `json:"hoverEffect,omitempty" ct:"select,=无,glow=发光,flash=闪烁,sec=motion,label=悬停效果"`

	DeckClick string `json:"deckClick,omitempty" ct:"select,zoom=展开放大,next=翻到下一页,default=zoom,sec=motion,label=点击卡片"`

	DeckArrows bool `json:"deckArrows,omitempty" ct:"bool,sec=motion,label=翻页按钮"`

	DeckHighlight string `json:"deckHighlight,omitempty" ct:"select,=无,glow=发光,flash=闪烁,sec=motion,label=主卡高亮"`

	DeckTransition string `json:"deckTransition,omitempty" ct:"select,=平滑,spring=回弹,ease-out=缓出,linear=线性,sec=motion,label=切换曲线"`

	DeckDuration int `json:"deckDuration,omitempty" ct:"slider,min=200,max=900,step=10,sec=motion,label=切换时长(ms)"`

	DeckOffset int `json:"deckOffset,omitempty" ct:"slider,min=5,max=120,step=1,sec=motion,label=相邻间距(%)"`

	DeckRotate int `json:"deckRotate,omitempty" ct:"slider,min=0,max=20,step=1,sec=motion,label=相邻倾斜(deg)"`

	DeckLoop bool `json:"deckLoop,omitempty" ct:"bool,sec=motion,label=循环切换"`

	DeckScaleStep int `json:"deckScaleStep,omitempty" ct:"slider,min=1,max=20,step=1,sec=motion,label=缩放递减(%)"`

	DragMode string `json:"dragMode,omitempty" ct:"select,ring=平面圆环,cylinder=三维环绕,default=ring,sec=motion,label=排布方式"`

	DragRadius int `json:"dragRadius,omitempty" ct:"slider,min=0,max=1200,step=10,sec=motion,label=环形半径(0=自动)"`

	SlideFit string `json:"slideFit,omitempty" ct:"select,=页面内滚动区,viewport=铺满视口(整页分页),sec=layout,label=贴合方式"`

	SlideDirection string `json:"slideDirection,omitempty" ct:"select,vertical=纵向滚动,horizontal=横向滚动,default=vertical,sec=layout,label=滚动方向"`

	SlideEffect string `json:"slideEffect,omitempty" ct:"select,=无（纯覆盖）,fade=淡入,zoom=缩放入场,flip=翻转入场,bounce=弹入,back=回弹入场,rotate=旋转入场,light=光速入场,roll=滚入,jack=弹出,sec=motion,label=切换动画"`

	SlideHighlight string `json:"slideHighlight,omitempty" ct:"select,=无,glow=发光,flash=闪烁,sec=motion,label=当前屏高亮"`

	SlideStack bool `json:"slideStack,omitempty" ct:"bool,sec=layout,label=堆叠翻页"`

	SlideHeight string `json:"slideHeight,omitempty" ct:"dimension,maxlen=20,sec=layout,label=每屏高度"`

	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=layout,label=卡片间距"`

	StickyTop string `json:"stickyTop,omitempty" ct:"dimension,maxlen=20,sec=layout,label=粘住位置"`

	ScaleBase int `json:"scaleBase,omitempty" ct:"slider,min=50,max=100,step=1,sec=motion,label=基准缩放(%)"`

	ScaleStep int `json:"scaleStep,omitempty" ct:"slider,min=1,max=10,step=1,sec=motion,label=缩放增量(%)"`

	CollectionSource string `json:"collectionSource,omitempty" ct:"select,=不使用,content:article=文章列表,content:product=商品列表,content:category=分类列表,sec=collection,label=内容集合"`

	CollectionLimit int `json:"collectionLimit,omitempty" ct:"slider,min=1,max=24,step=1,sec=collection,label=取几条"`

	CollectionEmpty string `json:"collectionEmpty,omitempty" ct:"select,=显示占位文案,hide=隐藏整个组件,sec=collection,label=无内容时"`

	CollectionEmptyText string `json:"collectionEmptyText,omitempty" ct:"text,maxlen=60,sec=collection,label=占位文案"`

	CardLinkText string `json:"cardLinkText,omitempty" ct:"text,maxlen=30,sec=collection,label=链接文案"`

	ListPageLink string `json:"listPageLink,omitempty" ct:"text,maxlen=30,sec=collection,label=列表页链接文案"`

	CardImageField string `json:"cardImageField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=图片字段"`

	CardTitleField string `json:"cardTitleField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=标题字段"`

	CardTextField string `json:"cardTextField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=正文字段"`

	CardMetaField string `json:"cardMetaField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=附注字段"`

	CardLinkField string `json:"cardLinkField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=链接字段"`

	CardLinkPrefix string `json:"cardLinkPrefix,omitempty" ct:"text,maxlen=80,sec=collection,label=链接前缀"`

	CardLayout string `json:"cardLayout,omitempty" ct:"select,column=纵向排列,row=横向排列,default=column,sec=style,label=卡内排列"`

	CardGap string `json:"cardGap,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡内间距"`

	CardJustify string `json:"cardJustify,omitempty" ct:"select,=居中,flex-start=靠前,center=居中,flex-end=靠后,space-between=两端对齐,sec=style,label=主轴对齐"`

	CardAlign string `json:"cardAlign,omitempty" ct:"select,=居中,flex-start=靠前,center=居中,flex-end=靠后,stretch=拉伸,sec=style,label=交叉轴对齐"`

	CardBackground string `json:"cardBackground,omitempty" ct:"color,maxlen=200,sec=style,label=卡片背景"`

	CardPadding string `json:"cardPadding,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡片内边距"`

	CardRadius string `json:"cardRadius,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡片圆角"`

	CardBorder string `json:"cardBorder,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡片边框宽度"`

	Zoom string `json:"zoom,omitempty" ct:"select,on=开启,off=关闭,default=on,sec=content,label=点击放大"`

	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

type Component struct{}

func (*Component) Actions() []core.ActionSpec {
	return []core.ActionSpec{{Key: "list", AllowedKinds: []core.ActionKind{core.ActionPage, core.ActionPresentation, core.ActionExternal}}}
}
func (*Component) RequiredActions() []string { return nil }

func (c *Component) CollectionProp() string { return "collectionSource" }

func init() {
	core.Register(&Component{})
	core.RegisterTemplate("cardstack", cardstackTemplate)

	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCardStacks"},
		Feats:  []string{"data-cardstack-drag"},
		Source: enhanceDragJS,
	})

	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCardDecks"},
		Feats:  []string{"data-cardstack-deck"},
		Source: enhanceDeckJS,
	})

	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initSlideStacks"},
		Feats:  []string{"data-cardstack-slide"},
		Source: enhanceSlideJS,
	})
}

func (c *Component) Type() string { return Type }

func (c *Component) PropsSpec() any { return &Props{} }

func (c *Component) Palette() core.PaletteMeta {
	return core.PaletteMeta{
		Type:     Type,
		Category: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"trigger":        "hover",
			"shape":          "fan",
			"count":          9,
			"hueStep":        50,
			"spreadAngle":    5,
			"spreadDistance": 120,
		},
		DisplayName: "卡片堆叠",
		Hint:        "悬停扇形/直排 · 滚动堆叠",
	}
}

func (c *Component) Validate(node *core.Node, ids map[string]bool) error {
	if err := c.ValidateSelf(node, ids); err != nil {
		return err
	}
	return core.ValidateChildren(node, ids)
}

func (c *Component) ValidateSelf(node *core.Node, ids map[string]bool) (err error) {
	if err = core.ValidateNodeID(node.ID, node.Name, ids); err != nil {
		return err
	}
	var p Props
	if len(node.Props) > 0 {
		if err = json.Unmarshal(node.Props, &p); err != nil {
			return fmt.Errorf("节点 %s props 反序列化失败: %w", node.ID, err)
		}
	}
	if err = core.ValidateSpec(&p, node.ID); err != nil {
		return err
	}
	for _, f := range []struct{ key, val string }{
		{"卡片宽度", p.Width},
		{"卡片高度", p.Height},
		{"卡片间距", p.Spacing},
		{"粘住位置", p.StickyTop},
		{"卡片背景", p.CardBackground},
		{"卡片内边距", p.CardPadding},
		{"卡片圆角", p.CardRadius},
		{"卡片边框宽度", p.CardBorder},
		{"卡内间距", p.CardGap},
	} {
		if f.val != "" && !core.IsSafeCSSValue(f.val) {
			return fmt.Errorf("节点 %s: 无效的%s: %q", node.ID, f.key, f.val)
		}
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	return nil
}

func effectiveTrigger(p *Props) string {
	switch p.Trigger {
	case TriggerScroll:
		return TriggerScroll
	case TriggerDrag:
		return TriggerDrag
	case TriggerDeck:
		return TriggerDeck
	case TriggerSlide:
		return TriggerSlide
	}
	return TriggerHover
}

func effectiveShape(p *Props) string {
	if p.Shape == ShapeLine {
		return ShapeLine
	}
	if p.Shape == ShapeBook {
		return ShapeBook
	}
	return ShapeFan
}

func collectionSource(p *Props) string { return strings.TrimSpace(p.CollectionSource) }

const (
	collectionSourceArticle = "content:article"
	collectionSourceProduct = "content:product"

	defaultArticleMoreText = "更多文章"
	defaultProductMoreText = "全部商品"
)

func collectionListPageLink(node *core.Node, source string, p *Props, ctx *core.RenderContext) (action core.ResolvedAction, text string, err error) {
	if _, configured := node.Actions["list"]; !configured {
		return action, "", nil
	}
	action, err = ctx.ResolveAction(node, "list")
	if err != nil {
		return action, "", err
	}
	text = "查看全部"
	switch source {
	case collectionSourceArticle:
		text = defaultArticleMoreText
	case collectionSourceProduct:
		text = defaultProductMoreText
	}
	if p != nil && strings.TrimSpace(p.ListPageLink) != "" {
		text = strings.TrimSpace(p.ListPageLink)
	}
	return action, text, nil
}

func effectiveCollectionLimit(p *Props) int {
	if p.CollectionLimit <= 0 {
		return defaultCollectionLimit
	}
	return p.CollectionLimit
}

const (
	defaultDeckOffset    = 54
	defaultDeckRotate    = 4
	defaultDeckScaleStep = 6

	defaultDeckOffsetVertical = 12
)

func effectiveDeckDirection(p *Props) string {
	if p.DeckDirection == "vertical" {
		return "vertical"
	}
	return "horizontal"
}

func effectiveDeckOffset(p *Props) int {
	if p.DeckOffset <= 0 {
		return defaultDeckOffset
	}
	return p.DeckOffset
}

func effectiveDeckRotate(p *Props) int {
	if p.DeckRotate <= 0 {
		return defaultDeckRotate
	}
	return p.DeckRotate
}

func effectiveDeckScaleStep(p *Props) int {
	if p.DeckScaleStep <= 0 {
		return defaultDeckScaleStep
	}
	return p.DeckScaleStep
}

func effectiveDirection(p *Props) string {
	if p.Direction == "vertical" {
		return "vertical"
	}
	return "horizontal"
}

func effectiveCount(p *Props) int {
	if p.Count < 2 {
		if effectiveTrigger(p) == TriggerSlide {
			return defaultSlideCount
		}
		return defaultCount
	}
	return p.Count
}

func effectiveHueStep(p *Props) int {
	if p.HueStep <= 0 {
		return defaultHueStep
	}
	return p.HueStep
}

func effectiveSpreadAngle(p *Props) int {
	if p.SpreadAngle <= 0 {
		return defaultSpreadAngle
	}
	return p.SpreadAngle
}

func effectiveSpreadDistance(p *Props) int {
	if p.SpreadDistance <= 0 {
		return defaultSpreadDistance
	}
	return p.SpreadDistance
}

func effectiveSpacing(p *Props) string {
	if p.Spacing == "" {
		return defaultSpacing
	}
	return p.Spacing
}

func effectiveStickyTop(p *Props) string {
	if p.StickyTop == "" {
		return defaultSticky
	}
	return p.StickyTop
}

func effectiveScaleBase(p *Props) int {
	if p.ScaleBase <= 0 {
		return defaultScaleBase
	}
	return p.ScaleBase
}

func effectiveScaleStep(p *Props) int {
	if p.ScaleStep <= 0 {
		return defaultScaleStep
	}
	return p.ScaleStep
}

func zoomEnabled(p *Props) bool {
	if effectiveTrigger(p) == TriggerSlide {
		return false
	}
	if effectiveTrigger(p) == TriggerDeck && p.DeckClick == deckClickNext {
		return false
	}
	return p.Zoom != "off"
}

func hasContent(node *core.Node) bool { return node != nil && len(node.Children) > 0 }

func cardCount(node *core.Node, p *Props) int {
	if hasContent(node) {
		return len(node.Children)
	}
	return effectiveCount(p)
}

func cardSize(p *Props, trigger string, content bool) (width, height string) {
	width, height = p.Width, p.Height
	defW, defH := widthNum, heightNum
	switch {
	case trigger == TriggerScroll:
		defW, defH = widthScroll, heightScroll
	case trigger == TriggerSlide:

		defW, defH = "100%", widthContent
	case content:
		defW, defH = widthContent, heightContent
	}
	if width == "" {
		width = defW
	}
	if height == "" {
		height = defH
	}
	return width, height
}

func num(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func fnum(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }

func scaleOf(i, base, step int) float64 { return float64(base+i*step) / 100 }

func keyframesName(id string, i int) string { return "sky-cs-" + id + "-" + strconv.Itoa(i+1) }

func CompileCSS(node *core.Node, p *Props, cardN int, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(node.ID)
	trigger := effectiveTrigger(p)

	content := hasContent(node) || collectionSource(p) != ""
	n := cardN
	if n <= 0 {
		n = cardCount(node, p)
	}
	width, height := cardSize(p, trigger, content)

	vars := cardstackVars(p, trigger, content, width, height)
	cardStyleVars(p, vars)
	hoverCSSVars(p, n, height, vars)
	scrollCSSVars(p, vars)
	dragCSSVars(p, n, width, height, vars)
	deckCSSVars(p, n, width, height, vars)
	slideCSSVars(p, vars)

	lists := map[string][]map[string]string{
		"hoverCards":  hoverCards(p, n, width),
		"scrollCards": scrollCards(node.ID, p, n),
		"dragCards":   dragCards(p, n, width, height),
		"deckCards":   deckCards(p, n, width, height),
		"slideCards":  slideCards(n),
	}

	switch trigger {
	case TriggerDeck:
		needKeyframe(b, loopEffectKey(p.DeckHighlight))
	case TriggerSlide:
		needKeyframe(b, slideEffectKeyframe(p))
		needKeyframe(b, loopEffectKey(p.SlideHighlight))
	case TriggerHover:
		needKeyframe(b, loopEffectKey(p.HoverEffect))
	}

	if err := core.ApplyComponentCSSTmplLists(b, sel, cardstackCSS, vars, lists); err != nil {
		panic(fmt.Sprintf("cardstack 组件样式解析失败: %v", err))
	}
}

func needKeyframe(b *core.CSSBuckets, name string) {
	if name != "" {
		b.NeedKeyframes(name)
	}
}

func cardstackVars(p *Props, trigger string, content bool, width, height string) map[string]string {
	return map[string]string{
		"triggerHover":  core.BoolVar(trigger == TriggerHover),
		"triggerScroll": core.BoolVar(trigger == TriggerScroll),
		"triggerDrag":   core.BoolVar(trigger == TriggerDrag),
		"triggerDeck":   core.BoolVar(trigger == TriggerDeck),
		"triggerSlide":  core.BoolVar(trigger == TriggerSlide),
		"content":       core.BoolVar(content),
		"solid":         core.BoolVar(!content),
		"zoom":          core.BoolVar(zoomEnabled(p)),
		"collection":    core.BoolVar(collectionSource(p) != ""),
		"focusRing":     core.BoolVar(trigger == TriggerDrag || trigger == TriggerDeck),
		"width":         width,
		"height":        height,

		"cardBorderX2": strconv.Itoa(2 * cardBorder),
	}
}

func cardStyleVars(p *Props, vars map[string]string) {
	padding := p.CardPadding
	if padding == "" {
		padding = contentPad
	}
	radius := p.CardRadius
	if radius == "" {
		radius = "16px"
	}
	border := p.CardBorder
	if border == "" {
		border = strconv.Itoa(cardBorder) + "px"
	}
	background := p.CardBackground
	if background == "" {
		background = colorSurface
	}
	gap := p.CardGap
	if gap == "" {
		gap = defaultCardGap
	}

	vars["cardLayoutContent"] = pickEnum(p.CardLayout, "column", "column", "row")
	vars["cardJustifyContent"] = pickEnum(p.CardJustify, "center", "flex-start", "center", "flex-end", "space-between")
	vars["cardAlignContent"] = pickEnum(p.CardAlign, "center", "flex-start", "center", "flex-end", "stretch")
	vars["cardGapContent"] = gap
	vars["cardPaddingContent"] = padding
	vars["cardBackgroundContent"] = background
	vars["cardBorderContent"] = border
	vars["cardRadiusContent"] = radius

	solidRadius := p.CardRadius
	if solidRadius == "" {
		solidRadius = "8px"
	}
	solidBorder := p.CardBorder
	if solidBorder == "" {
		solidBorder = strconv.Itoa(cardBorder) + "px"
	}
	solidBackground := p.CardBackground
	if solidBackground == "" {
		solidBackground = colorPrimary
	}
	vars["cardRadiusSolid"] = solidRadius
	vars["cardBorderSolid"] = solidBorder
	vars["cardBackgroundSolid"] = solidBackground
}

func hoverCSSVars(p *Props, n int, height string, vars map[string]string) {
	dist := float64(effectiveSpreadDistance(p))
	mid := float64(n-1) / 2.0
	fan := effectiveShape(p) == ShapeFan
	vertical := !fan && effectiveDirection(p) == directionVertical
	trackHeight := fmt.Sprintf("min-height: calc(%s + %dpx)", height, 2*cardLiftY)
	switch {
	case vertical:
		trackHeight = fmt.Sprintf("min-height: calc(%s + %dpx)", height, int(2*mid*dist))
	case !fan:
		trackHeight = "min-height: " + height
	}
	vars["hoverTrackHeight"] = trackHeight
	vars["book"] = core.BoolVar(effectiveShape(p) == ShapeBook)
	kf := loopEffectKey(p.HoverEffect)
	vars["hoverEffect"] = core.BoolVar(kf != "")
	vars["hoverEffectKey"] = kf
}

func hoverCards(p *Props, n int, width string) []map[string]string {
	mid := float64(n-1) / 2.0
	hueStep := float64(effectiveHueStep(p))
	angle := float64(effectiveSpreadAngle(p))
	dist := float64(effectiveSpreadDistance(p))
	fan := effectiveShape(p) == ShapeFan
	book := effectiveShape(p) == ShapeBook
	vertical := !fan && effectiveDirection(p) == directionVertical

	cards := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		offset := float64(i) - mid
		item := map[string]string{
			"nth": strconv.Itoa(i + 1),

			"hue": fmt.Sprintf("%.0f", offset*hueStep),

			"origin":    "left center",
			"closedDeg": num(float64(bookClosedDeg)),
		}
		if offset < 0 {
			item["origin"] = "right center"
			item["closedDeg"] = num(-float64(bookClosedDeg))
		}

		var fixed, adaptive string
		switch {
		case book:

			openX := offset * cssPx(width, fallbackCardW) * 0.58

			allow := fmt.Sprintf("calc((50vw - %dpx - 50%%) / %s)", viewportGutter, fnum(mid))
			openXDecl := fmt.Sprintf("clamp(calc(-1 * %s), %spx, %s)", allow, num(openX), allow)
			fixed = "rotateY(0deg) translateX(" + openXDecl + ")"
			adaptive = fixed
		case fan:

			rad := math.Abs(offset*angle) * math.Pi / 180
			cosT, sinT := math.Cos(rad), math.Sin(rad)
			allow := fmt.Sprintf("calc((50vw - %dpx - %spx - %s * 50%% - %s * var(--sky-cardstack-h) / 2) / %s)",
				viewportGutter, fnum(cardLiftY*sinT), fnum(cosT), fnum(sinT), fnum(cosT*mid))
			fixed = fmt.Sprintf("rotate(%sdeg) translate(%spx, -%dpx)", num(offset*angle), num(offset*dist), cardLiftY)
			adaptive = fmt.Sprintf("rotate(%sdeg) translate(calc(%s * clamp(0px, %s, %dpx)), -%dpx)",
				num(offset*angle), num(offset), allow, int(dist), cardLiftY)
		case vertical:

			allow := fmt.Sprintf("calc((50vh - %dpx - var(--sky-cardstack-h) / 2) / %s)", viewportGutter, num(mid))
			fixed = fmt.Sprintf("translate(0px, %spx)", num(offset*dist))
			adaptive = fmt.Sprintf("translate(0px, calc(%s * clamp(0px, %s, %dpx)))",
				num(offset), allow, int(dist))
		default:

			allow := fmt.Sprintf("calc((50vw - %dpx - 50%%) / %s)", viewportGutter, num(mid))
			fixed = fmt.Sprintf("translate(%spx, 0px)", num(offset*dist))
			adaptive = fmt.Sprintf("translate(calc(%s * clamp(0px, %s, %dpx)), 0px)",
				num(offset), allow, int(dist))
		}

		item["fixed"] = fixed
		item["adaptive"] = adaptive
		cards = append(cards, item)
	}
	return cards
}

func scrollCSSVars(p *Props, vars map[string]string) {
	vars["spacing"] = effectiveSpacing(p)
	vars["stickyTop"] = effectiveStickyTop(p)
}

func scrollCards(id string, p *Props, n int) []map[string]string {
	base, step := effectiveScaleBase(p), effectiveScaleStep(p)
	cards := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		s := scaleOf(i, base, step)
		cards = append(cards, map[string]string{
			"nth":       strconv.Itoa(i + 1),
			"scale":     fnum(s),
			"scaleFrom": fnum(s * 1.12),
			"kf":        keyframesName(id, i),
		})
	}
	return cards
}

func dragCSSVars(p *Props, n int, width, height string, vars map[string]string) {
	radius := dragRadius(p, n, width, height)
	cardH := cssPx(height, fallbackCardH)
	cylinder := p.DragMode == dragModeCylinder
	occupancy := width
	if cylinder && radius > 0 && radius < dragPerspective {
		occupancy = fmt.Sprintf("calc(%s * %.4f)", width, dragPerspective/(dragPerspective-radius))
	}

	vars["dragTrackMinHeight"] = fmt.Sprintf("min-height: %dpx", int(2*radius+cardH))
	vars["dragSideRoom"] = fmt.Sprintf("max(0px, calc((100vw - %s) / 2 - %dpx))", occupancy, dragGutter)
	vars["dragCylinder"] = core.BoolVar(cylinder)
	vars["dragPerspective"] = strconv.Itoa(int(dragPerspective))
	vars["deckArrows"] = core.BoolVar(p.DeckArrows)
}

func dragCards(p *Props, n int, width, height string) []map[string]string {
	radius := dragRadius(p, n, width, height)
	cylinder := p.DragMode == dragModeCylinder
	hueStep := float64(effectiveHueStep(p))
	cards := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		angle := 360 * float64(i) / float64(n)
		cards = append(cards, map[string]string{
			"nth":       strconv.Itoa(i + 1),
			"transform": dragCardTransform(angle, radius, cylinder),

			"hue": fmt.Sprintf("%.0f", float64(i)*hueStep),
		})
	}
	return cards
}

func slideEffectKeyframe(p *Props) string {
	horizontal := p.SlideDirection == slideDirectionHorizontal

	name := ""
	switch p.SlideEffect {
	case "fade":
		if horizontal {
			name = "fade-in-bottom-right"
		} else {
			name = "fade-in-bottom-left"
		}
	case "zoom":
		if horizontal {
			name = "zoom-in-right"
		} else {
			name = "zoom-in-up"
		}
	case "flip":
		if horizontal {
			name = "flip-in-y"
		} else {
			name = "flip-in-x"
		}
	case "bounce":
		if horizontal {
			name = "bounce-in-right"
		} else {
			name = "bounce-in-up"
		}
	case "back":
		if horizontal {
			name = "back-in-right"
		} else {
			name = "back-in-up"
		}
	case "rotate":
		if horizontal {
			name = "rotate-in-up-right"
		} else {
			name = "rotate-in-up-left"
		}
	case "light":
		if horizontal {
			name = "light-speed-in-right"
		} else {
			name = "light-speed-in-left"
		}
	case "roll":
		name = "roll-in"
	case "jack":
		name = "jack-in-the-box"
	}
	return core.EffectKeyframeName(core.KindEntrance, name)
}

func loopEffectKey(v string) string {
	name := ""
	switch v {
	case "glow":
		name = "glow"
	case "flash":
		name = "flash"
	}
	return core.EffectKeyframeName(core.KindLoop, name)
}

func deckEasing(p *Props) string {
	switch p.DeckTransition {
	case "spring":
		return "cubic-bezier(.34,1.56,.64,1)"
	case "ease-out":
		return "ease-out"
	case "linear":
		return "linear"
	}
	return "cubic-bezier(.22,.61,.36,1)"
}

func effectiveDeckDuration(p *Props) int {
	if p.DeckDuration < 200 {
		return 450
	}
	return p.DeckDuration
}

func deckTransform(offset, rot int, scaleStep float64, vertical bool) string {
	axis := "X"
	if vertical {
		axis = "Y"
	}

	return fmt.Sprintf("transform: translate%s(calc(var(--sky-deck-off, 0) * %d%%)) rotate(calc(var(--sky-deck-off, 0) * %ddeg)) scale(calc(1 - var(--sky-deck-abs, 0) * %s))",
		axis, offset, rot, fnum(scaleStep))
}

func dragCardTransform(angle float64, radius float64, cylinder bool) string {
	if cylinder {
		return fmt.Sprintf("transform: rotateY(calc(%sdeg + var(--sky-cardstack-rot, 0deg))) translateZ(min(%dpx, var(--sky-cardstack-side, 40vw)))",
			num(angle), int(radius))
	}
	return fmt.Sprintf("transform: rotate(calc(%sdeg + var(--sky-cardstack-rot, 0deg))) translateY(calc(-1 * min(%dpx, var(--sky-cardstack-side, 40vw)))) rotate(calc(-%sdeg - var(--sky-cardstack-rot, 0deg)))",
		num(angle), int(radius), num(angle))
}

func dragRadius(p *Props, n int, width, height string) float64 {
	if p.DragRadius > 0 {
		return float64(p.DragRadius)
	}
	cardW := cssPx(width, fallbackCardW)
	cardH := cssPx(height, fallbackCardH)
	if n < 2 {
		return cardH / 2
	}

	r := cardW / (2 * math.Sin(math.Pi/float64(n)))
	if min := cardH/2 + 24; r < min {
		r = min
	}
	return r
}

func pickEnum(v, def string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return def
}

func cssPx(v string, fallback float64) float64 {
	if s, ok := strings.CutSuffix(v, "px"); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
			return f
		}
	}
	return fallback
}

func deckCSSVars(p *Props, n int, width, height string, vars map[string]string) {
	rot := effectiveDeckRotate(p)
	vertical := effectiveDeckDirection(p) == "vertical"

	if vertical && p.DeckOffset <= 0 {

		if p.DeckRotate <= 0 {
			rot = defaultDeckRotate / 2
		}
	}
	mid := float64(n-1) / 2.0

	maxOff := math.Max(mid, 1)
	cardW := cssPx(width, fallbackCardW)
	cardH := cssPx(height, fallbackCardH)
	tiltOut := cardW * math.Sin(float64(rot)*math.Pi/180) * maxOff * 0.35
	trackH := cardH + 2*math.Max(24, tiltOut)

	touchAction := "pan-y"
	if vertical {
		touchAction = "pan-x"
	}
	vars["deckTouchAction"] = touchAction
	vars["deckTrackMinHeight"] = fmt.Sprintf("min-height: %dpx", int(trackH))
	vars["deckDurationMs"] = strconv.Itoa(effectiveDeckDuration(p))
	vars["deckEasing"] = deckEasing(p)
	kf := loopEffectKey(p.DeckHighlight)
	vars["deckHighlight"] = core.BoolVar(kf != "")
	vars["deckHighlightKey"] = kf
}

func deckCards(p *Props, n int, width, height string) []map[string]string {
	offset := effectiveDeckOffset(p)
	rot := effectiveDeckRotate(p)
	scaleStep := float64(effectiveDeckScaleStep(p)) / 100
	vertical := effectiveDeckDirection(p) == "vertical"
	if vertical && p.DeckOffset <= 0 {
		offset = defaultDeckOffsetVertical
		if p.DeckRotate <= 0 {
			rot = defaultDeckRotate / 2
		}
	}
	mid := float64(n-1) / 2.0
	hueStep := float64(effectiveHueStep(p))
	cards := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		static := float64(i) - mid
		cards = append(cards, map[string]string{
			"nth":       strconv.Itoa(i + 1),
			"static":    num(static),
			"staticAbs": num(math.Abs(static)),
			"transform": deckTransform(offset, rot, scaleStep, vertical),
			"hue":       fmt.Sprintf("%.0f", static*hueStep),
		})
	}
	return cards
}

func slideCSSVars(p *Props, vars map[string]string) {
	screen := strings.TrimSpace(p.SlideHeight)
	if screen == "" {
		screen = defaultSlideHeight
	}
	fallback := screen
	dvh := strings.HasSuffix(screen, "dvh")
	if dvh {
		fallback = strings.TrimSuffix(screen, "dvh") + "vh"
	}
	horizontal := p.SlideDirection == slideDirectionHorizontal
	overflowMain, overflowCross := "overflow-y: auto", "overflow-x: hidden"
	snapAxis := "y"
	stickyAxis := "top: 0"
	if horizontal {
		overflowMain, overflowCross = "overflow-x: auto", "overflow-y: hidden"
		snapAxis = "x"
		stickyAxis = "left: 0"
	}
	vars["slideFitViewport"] = core.BoolVar(p.SlideFit == slideFitViewport)
	vars["slideScreenDvh"] = core.BoolVar(dvh)
	vars["slideScreen"] = screen
	vars["slideScreenFallback"] = fallback
	vars["slideOverflowMain"] = overflowMain
	vars["slideOverflowCross"] = overflowCross
	vars["slideSnapAxis"] = snapAxis
	vars["slideHorizontal"] = core.BoolVar(horizontal)
	vars["slideStack"] = core.BoolVar(p.SlideStack)
	vars["slideStickyAxis"] = stickyAxis

	kf := slideEffectKeyframe(p)
	vars["slideEffect"] = core.BoolVar(kf != "")
	vars["slideEffectKey"] = kf
	highlight := loopEffectKey(p.SlideHighlight)
	vars["slideHighlight"] = core.BoolVar(highlight != "")
	vars["slideHighlightKey"] = highlight
}

func slideCards(n int) []map[string]string {
	cards := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		cards = append(cards, map[string]string{"nth": strconv.Itoa(i + 1)})
	}
	return cards
}

//go:embed cardstack.jet
var cardstackTemplate string
