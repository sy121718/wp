// Package cardstack 实现 core.cardstack 卡片堆叠组件：同一份「N 张卡按序号派生几何」
// 的机制，两种触发方式 × 两种展开形态。
//
//	trigger: hover  —— 悬停展开（纯 CSS，零 JS）：卡片叠成一摞，悬停时按位置派生
//	                  逐张旋转/平移/错色散开；点击任意一张放大到视口中央。
//	trigger: scroll —— 滚动堆叠（sticky + CSS scroll-driven，零 JS）：卡片垂直排列，
//	                  滚到视口时逐张粘住并收敛到目标缩放；不支持 scroll-driven 的
//	                  浏览器降级为纯 sticky 层叠（布局与层级完整，只是没有跟手缩放）。
//	shape:   fan     —— 弧线散开（rotate 在 translate 之前，位移落在旋转后的坐标系）
//	         line    —— 直线排开（translate 在 rotate 之前）
//
// 卡片内容两种来源，遵循「有子节点就用子节点」：拖入子组件即内容卡（每张卡一个
// 子节点）；没有子节点则退回 1~N 数字占位卡（方便先调几何再填内容）。
//
// 确定性：所有几何都由编译期算好写进静态 CSS（逐卡 :nth-child 规则 + 每卡独立的
// scroll-driven 关键帧），同 props 同字节，不依赖运行时 JS 或随机数。
package cardstack

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.cardstack"

// 触发方式。
const (
	TriggerHover  = "hover"
	TriggerScroll = "scroll"
	TriggerDrag   = "drag"
	// TriggerDeck 堆叠轮播：主卡居中正立，两侧卡片叠开，滑动/拖拽/点击切换主卡。
	TriggerDeck = "deck"
	// TriggerSlide 全屏分页：一屏一张卡，原生滚动吸附切换（不劫持滚动）。
	TriggerSlide = "slide"
)

// 展开形态。
const (
	ShapeFan  = "fan"
	ShapeLine = "line"
)

// 排开方向（仅 line 形态）。
const (
	directionHorizontal = "horizontal"
	directionVertical   = "vertical"
)

// 缺省值与几何常量。
//
// 数值缺省遵循 core 的「零值 = 未设置」约定（ValidateSpec 对数值 0 直接放行），
// 因此 ct 下界一律声明为 1/下限值，0 不会被当成有效值。
const (
	defaultCount          = 9
	defaultHueStep        = 50
	defaultSpreadAngle    = 5
	defaultSpreadDistance = 120
	defaultScaleBase      = 94
	defaultScaleStep      = 2
	// cardLiftY 悬停上抬量（px）：容器据此在上下各预留同等空间。
	cardLiftY = 50
	// cardBorder 卡片边框宽度（px）：参与 border box 换算（translate 的百分比基于 border box）。
	cardBorder = 10
	// viewportGutter 横向收敛时给视口边缘留的安全余量（px）。
	viewportGutter = 16
	// contentPad 内容卡内边距。
	contentPad = "24px"
)

// 缺省尺寸按「触发方式 + 卡片来源」取：数字占位卡小而方正，内容卡要读得下正文，
// 滚动堆叠则是整栏的宽卡。props 显式给了值就一律以用户值为准。
const (
	widthNum       = "240px"
	heightNum      = "320px"
	widthContent   = "360px"
	heightContent  = "240px"
	widthScroll    = "720px"
	heightScroll   = "240px"
	defaultSpacing = "26vh"
	defaultSticky  = "50%"
	// defaultCollectionLimit 内容集合缺省取几条。
	defaultCollectionLimit = 6
	// defaultCardGap 内容卡内部元素间距缺省值。
	defaultCardGap = "10px"
	// defaultCardLinkText / defaultCollectionEmptyText 内置文案缺省值（可由 props 覆盖）。
	defaultCardLinkText        = "查看详情"
	defaultCollectionEmptyText = "暂无内容"
	// defaultSlideHeight 全屏分页每屏高度缺省值（dvh：跟随移动端地址栏收放）。
	defaultSlideHeight = "100dvh"
	// defaultSlideCount 全屏分页占位卡缺省数量。
	defaultSlideCount = 3
	// slideFitViewport slideFit 的「铺满视口」取值。
	slideFitViewport = "viewport"
	// slideDirectionHorizontal slideDirection 的横向取值。
	slideDirectionHorizontal = "horizontal"
	// fallbackCardW / fallbackCardH 拖拽旋转算环形半径时的兜底卡片尺寸（非 px 宽度时使用）。
	fallbackCardW = 320
	fallbackCardH = 240
)

// 取色（与 badge/quote/progress 同一约定：CSS 变量 + 兜底色，主题可整体覆写）。
const (
	colorPrimary = "var(--sky-c-primary, #5e5cfc)"
	colorLabel   = "var(--sky-cardstack-label, rgba(0,0,0,.25))"
	colorDim     = "var(--sky-cardstack-dim, #333)"
	colorSurface = "var(--sky-c-surface, #fff)"
)

// Props 卡片堆叠属性。
type Props struct {
	// Trigger 触发方式：hover 悬停展开 / scroll 滚动堆叠（纯 CSS）/ drag 拖拽旋转（增强脚本）。
	Trigger string `json:"trigger,omitempty" ct:"select,hover=悬停展开,scroll=滚动堆叠,drag=拖拽旋转,deck=堆叠轮播,slide=全屏分页,default=hover,sec=content,label=触发方式"`
	// Shape 展开形态（悬停模式）：fan 弧线扇形 / line 排开（卡片不带任何角度）。
	Shape string `json:"shape,omitempty" ct:"select,fan=扇形展开,line=直线排开,default=fan,sec=content,label=展开形态"`
	// Direction 排开方向（仅 shape=line 生效）：horizontal 横排一行 / vertical 竖排一列。
	Direction string `json:"direction,omitempty" ct:"select,horizontal=横排一行,vertical=竖排一列,default=horizontal,sec=content,label=排开方向"`
	// Count 数字占位卡数量（2~12，缺省 9）；拖入子节点后以子节点数量为准。
	Count int `json:"count,omitempty" ct:"slider,min=2,max=12,step=1,sec=content,label=占位卡数量"`
	// Width 单卡宽度的上限（缺省按模式：数字卡 240px / 内容卡 360px / 滚动 720px）。
	Width string `json:"width,omitempty" ct:"dimension,maxlen=20,sec=content,label=卡片宽度"`
	// Height 单卡高度：数字卡为固定高度，内容卡为最小高度。
	Height string `json:"height,omitempty" ct:"dimension,maxlen=20,sec=content,label=卡片高度"`
	// HueStep 相邻卡片色相步长（deg，1~120，缺省 50）：中间卡 0 偏移，两侧按 ±step 渐变。
	HueStep int `json:"hueStep,omitempty" ct:"slider,min=1,max=120,step=1,sec=style,label=色相步长(deg)"`
	// SpreadAngle 悬停展开角度系数（deg/张，1~15，缺省 5）。
	SpreadAngle int `json:"spreadAngle,omitempty" ct:"slider,min=1,max=15,step=1,sec=motion,label=展开角度(deg)"`
	// SpreadDistance 悬停展开平移系数（px/张，1~200，缺省 120；同时是「铺满」开关）。
	SpreadDistance int `json:"spreadDistance,omitempty" ct:"slider,min=1,max=200,step=1,sec=motion,label=展开平移(px)"`
	// DeckDirection 堆叠轮播的切换方向：horizontal 横向 / vertical 纵向。
	// 纵向即「首屏一张卡叠着，上下滑动翻到下一张」—— 与 slide 的纵向平铺滚动是两回事。
	DeckDirection string `json:"deckDirection,omitempty" ct:"select,horizontal=横向切换,vertical=纵向切换,default=horizontal,sec=motion,label=切换方向"`
	// HoverEffect 悬停时的循环效果（仅 hover 模式）：只挑不抢 transform 的两条词汇 ——
	// 展开位移已经占用了 transform，swing/wobble/pulse 之类会把位移顶掉。
	HoverEffect string `json:"hoverEffect,omitempty" ct:"select,=无,glow=发光,flash=闪烁,sec=motion,label=悬停效果"`
	// DeckHighlight 主卡高亮循环效果（仅 deck 模式）：同样只走 filter/opacity。
	DeckHighlight string `json:"deckHighlight,omitempty" ct:"select,=无,glow=发光,flash=闪烁,sec=motion,label=主卡高亮"`
	// DeckTransition 卡片切换的过渡曲线：缺省平滑缓出，spring 带回弹。
	// （deck 的卡片始终在视口内，入场类动画会和位置变换抢 transform，所以这里走过渡曲线。）
	DeckTransition string `json:"deckTransition,omitempty" ct:"select,=平滑,spring=回弹,ease-out=缓出,linear=线性,sec=motion,label=切换曲线"`
	// DeckDuration 切换时长 ms（缺省 450）。
	DeckDuration int `json:"deckDuration,omitempty" ct:"slider,min=200,max=900,step=10,sec=motion,label=切换时长(ms)"`
	// DeckOffset 堆叠轮播的相邻卡间距（%，横向相对卡宽缺省 54、纵向相对卡高缺省 12）。
	DeckOffset int `json:"deckOffset,omitempty" ct:"slider,min=5,max=120,step=1,sec=motion,label=相邻间距(%)"`
	// DeckRotate 堆叠轮播相邻卡的倾斜角度（deg，缺省 4；0 = 不倾斜，用默认）。
	DeckRotate int `json:"deckRotate,omitempty" ct:"slider,min=0,max=20,step=1,sec=motion,label=相邻倾斜(deg)"`
	// DeckLoop 堆叠轮播循环切换：滑到最后一张继续往前会回到第一张。
	DeckLoop bool `json:"deckLoop,omitempty" ct:"bool,sec=motion,label=循环切换"`
	// DeckScaleStep 堆叠轮播每远一张的缩放递减（%，缺省 6）。
	DeckScaleStep int `json:"deckScaleStep,omitempty" ct:"slider,min=1,max=20,step=1,sec=motion,label=缩放递减(%)"`
	// DragRadius 拖拽旋转的环形半径 px（0 = 自动：按卡片宽度与数量保证相邻卡片不重叠）。
	DragRadius int `json:"dragRadius,omitempty" ct:"slider,min=0,max=1200,step=10,sec=motion,label=环形半径(0=自动)"`
	// SlideFit 全屏分页的贴合方式：inline 页面内滚动区 / viewport 铺满视口。
	// viewport 让容器脱离文档流铺满视口，父容器的内边距不再影响它 ——
	// 「整页分页」不必再手动把父容器 padding 归零；代价是页面上不能有别的同级内容。
	SlideFit string `json:"slideFit,omitempty" ct:"select,=页面内滚动区,viewport=铺满视口(整页分页),sec=layout,label=贴合方式"`
	// SlideDirection 全屏分页的滚动方向：vertical 纵向 / horizontal 横向。
	SlideDirection string `json:"slideDirection,omitempty" ct:"select,vertical=纵向滚动,horizontal=横向滚动,default=vertical,sec=layout,label=滚动方向"`
	// SlideEffect 卡片切换动画：复用通用动效词汇（core/keyframes_animate.go），
	// 卡片进入视口时播放；缺省为空 = 纯覆盖（只有位置变化，不加动画）。
	SlideEffect string `json:"slideEffect,omitempty" ct:"select,=无（纯覆盖）,fade=淡入,zoom=缩放入场,flip=翻转入场,bounce=弹入,back=回弹入场,rotate=旋转入场,light=光速入场,roll=滚入,jack=弹出,sec=motion,label=切换动画"`
	// SlideHighlight 当前屏的循环高亮（发光 / 闪烁）：卡片滚到视口中段时才亮，
	// 与切换动画并存 —— 两者各占一条 animation，各自带自己的 timeline 与 range。
	SlideHighlight string `json:"slideHighlight,omitempty" ct:"select,=无,glow=发光,flash=闪烁,sec=motion,label=当前屏高亮"`
	// SlideStack 堆叠翻页：卡片粘在同一位置，下一张滑上来盖住前一张（缺省平铺）。
	// 与平铺的区别：平铺时上滑会把前一张推走，堆叠时前一张留在原地被覆盖。
	SlideStack bool `json:"slideStack,omitempty" ct:"bool,sec=layout,label=堆叠翻页"`
	// SlideHeight 全屏分页的每屏高度（缺省 100dvh —— 用 dvh 而非 vh，移动端地址栏收放时不会跳）。
	SlideHeight string `json:"slideHeight,omitempty" ct:"dimension,maxlen=20,sec=layout,label=每屏高度"`
	// Spacing 滚动模式的卡片间距（缺省 26vh）。
	Spacing string `json:"spacing,omitempty" ct:"dimension,maxlen=20,sec=layout,label=卡片间距"`
	// StickyTop 滚动模式卡片的粘住位置（缺省 50%，即视口垂直居中）。
	StickyTop string `json:"stickyTop,omitempty" ct:"dimension,maxlen=20,sec=layout,label=粘住位置"`
	// ScaleBase 滚动模式基准缩放（%，50~100，缺省 94）。
	ScaleBase int `json:"scaleBase,omitempty" ct:"slider,min=50,max=100,step=1,sec=motion,label=基准缩放(%)"`
	// ScaleStep 滚动模式按序号的缩放增量（%，1~10，缺省 2）：第 i 张 = base + i*step。
	ScaleStep int `json:"scaleStep,omitempty" ct:"slider,min=1,max=10,step=1,sec=motion,label=缩放增量(%)"`
	// —— 内容集合（自动出卡：卡片数量随内容条数变化，无需手工摆卡）——
	// CollectionSource 集合源；空 = 静态卡片（子节点即卡片 / 数字占位卡）。
	CollectionSource string `json:"collectionSource,omitempty" ct:"select,=不使用,content:article=文章列表,content:product=商品列表,content:category=分类列表,sec=collection,label=内容集合"`
	// CollectionLimit 取前几条（1~24，缺省 6）。
	CollectionLimit int `json:"collectionLimit,omitempty" ct:"slider,min=1,max=24,step=1,sec=collection,label=取几条"`
	// CollectionEmpty 集合无内容时的表现：留空显示占位文案、hide 隐藏整个组件。
	CollectionEmpty string `json:"collectionEmpty,omitempty" ct:"select,=显示占位文案,hide=隐藏整个组件,sec=collection,label=无内容时"`
	// CollectionEmptyText 无内容时的占位文案（缺省「暂无内容」）。
	CollectionEmptyText string `json:"collectionEmptyText,omitempty" ct:"text,maxlen=60,sec=collection,label=占位文案"`
	// CardLinkText 卡片详情链接的文案（缺省「查看详情」；内置文案做成可配，多语言站点不必改代码）。
	CardLinkText string `json:"cardLinkText,omitempty" ct:"text,maxlen=30,sec=collection,label=链接文案"`
	// CardImageField 图片字段名（如 product.images / article.featuredImage；留空不渲染图片）。
	CardImageField string `json:"cardImageField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=图片字段"`
	// CardTitleField 标题字段名（如 product.name / article.title）。
	CardTitleField string `json:"cardTitleField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=标题字段"`
	// CardTextField 正文字段名（如 article.excerpt / product.description）。
	CardTextField string `json:"cardTextField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=正文字段"`
	// CardMetaField 附注字段名（价格/分类等一行次要信息，如 product.price）。
	CardMetaField string `json:"cardMetaField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=附注字段"`
	// CardLinkField 链接字段名（通常填 slug）。
	CardLinkField string `json:"cardLinkField,omitempty" ct:"collectionfield,maxlen=40,sec=collection,label=链接字段"`
	// CardLinkPrefix 链接前缀（如 /article/），与链接字段拼接成 href。
	CardLinkPrefix string `json:"cardLinkPrefix,omitempty" ct:"text,maxlen=80,sec=collection,label=链接前缀"`
	// CardLayout 卡内排列方向（内容卡）：column 纵向 / row 横向。
	CardLayout string `json:"cardLayout,omitempty" ct:"select,column=纵向排列,row=横向排列,default=column,sec=style,label=卡内排列"`
	// CardGap 卡内元素间距（缺省 10px）。
	CardGap string `json:"cardGap,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡内间距"`
	// CardJustify 主轴对齐（缺省 center；集合卡缺省 flex-start：内容从上往下排）。
	CardJustify string `json:"cardJustify,omitempty" ct:"select,=居中,flex-start=靠前,center=居中,flex-end=靠后,space-between=两端对齐,sec=style,label=主轴对齐"`
	// CardAlign 交叉轴对齐（缺省 center）。
	CardAlign string `json:"cardAlign,omitempty" ct:"select,=居中,flex-start=靠前,center=居中,flex-end=靠后,stretch=拉伸,sec=style,label=交叉轴对齐"`
	// CardBackground 卡片背景（缺省：内容卡取主题 surface 色、数字卡取主色）。
	CardBackground string `json:"cardBackground,omitempty" ct:"color,maxlen=200,sec=style,label=卡片背景"`
	// CardPadding 内容卡内边距（缺省 24px；数字卡为居中排版，不受此项影响）。
	CardPadding string `json:"cardPadding,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡片内边距"`
	// CardRadius 卡片圆角（缺省：内容卡 16px、数字卡 8px）。
	CardRadius string `json:"cardRadius,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡片圆角"`
	// CardBorder 卡片边框宽度（缺省 10px；设为 1px 即为细描边卡片）。
	CardBorder string `json:"cardBorder,omitempty" ct:"dimension,maxlen=20,sec=style,label=卡片边框宽度"`
	// Zoom 点击放大到视口中央（零 JS，同组互斥，一次只放大一张）。
	Zoom string `json:"zoom,omitempty" ct:"select,on=开启,off=关闭,default=on,sec=content,label=点击放大"`
	// Advanced 通用高级属性（基座自动校验与编译）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Component 卡片堆叠组件（结构型：子节点即卡片内容，可留空退回数字占位卡）。
type Component struct{}

// init 注册组件。
func init() { core.Register(&Component{}) }

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema。
func (c *Component) PropsSpec() any { return &Props{} }

// Validate 校验：props 合法性 + 尺寸为 CSS 安全值 + 子节点递归校验。
func (c *Component) Validate(node *core.Node, ids map[string]bool) (err error) {
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
	for _, child := range node.Children {
		if err = core.ValidateNode(child, ids); err != nil {
			return fmt.Errorf("节点 %s 卡片: %w", node.ID, err)
		}
	}
	if adv := core.AdvancedOf(&p); adv != nil {
		return core.ValidateAdvanced(adv, node.ID, ids)
	}
	return nil
}

// --- 有效值解析（缺省兜底）---

// effectiveTrigger 触发方式缺省 hover。
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

// effectiveShape 展开形态缺省 fan。
func effectiveShape(p *Props) string {
	if p.Shape == ShapeLine {
		return ShapeLine
	}
	return ShapeFan
}

// collectionSource 集合源（空 = 静态卡片模式）。
func collectionSource(p *Props) string { return strings.TrimSpace(p.CollectionSource) }

// effectiveCollectionLimit 集合取几条，缺省 6。
func effectiveCollectionLimit(p *Props) int {
	if p.CollectionLimit <= 0 {
		return defaultCollectionLimit
	}
	return p.CollectionLimit
}

// 堆叠轮播缺省值。
const (
	defaultDeckOffset    = 54 // 相邻卡横向间距（%）
	defaultDeckRotate    = 4  // 相邻卡倾斜（deg）
	defaultDeckScaleStep = 6
	// defaultDeckOffsetVertical 纵向切换的相邻卡偏移（%，相对卡高）——
	// 纵向叠卡的分层靠缩放与层级，位移给大了会散成一列。
	defaultDeckOffsetVertical = 12 // 每远一张的缩放递减（%）
)

// effectiveDeckDirection 堆叠轮播切换方向缺省横向。
// （与 shape 的 direction 是两回事：那个是悬停展开的排开方向。）
func effectiveDeckDirection(p *Props) string {
	if p.DeckDirection == "vertical" {
		return "vertical"
	}
	return "horizontal"
}

// effectiveDeckOffset 堆叠轮播相邻卡间距（横向按卡宽 %，纵向按卡高 %），缺省 54。
func effectiveDeckOffset(p *Props) int {
	if p.DeckOffset <= 0 {
		return defaultDeckOffset
	}
	return p.DeckOffset
}

// effectiveDeckRotate 相邻卡倾斜缺省 4°。
func effectiveDeckRotate(p *Props) int {
	if p.DeckRotate <= 0 {
		return defaultDeckRotate
	}
	return p.DeckRotate
}

// effectiveDeckScaleStep 缩放递减缺省 6%。
func effectiveDeckScaleStep(p *Props) int {
	if p.DeckScaleStep <= 0 {
		return defaultDeckScaleStep
	}
	return p.DeckScaleStep
}

// effectiveDirection 排开方向缺省横排。
func effectiveDirection(p *Props) string {
	if p.Direction == "vertical" {
		return "vertical"
	}
	return "horizontal"
}

// effectiveCount 占位卡数量缺省 9（ct 声明 min=2，0/1 视作未设置）。
// 全屏分页缺省 3 屏 —— 9 屏占位卡在滚动吸附下要翻很久，不是合理起点。
func effectiveCount(p *Props) int {
	if p.Count < 2 {
		if effectiveTrigger(p) == TriggerSlide {
			return defaultSlideCount
		}
		return defaultCount
	}
	return p.Count
}

// effectiveHueStep 空值缺省 50。
func effectiveHueStep(p *Props) int {
	if p.HueStep <= 0 {
		return defaultHueStep
	}
	return p.HueStep
}

// effectiveSpreadAngle 空值缺省 5。
func effectiveSpreadAngle(p *Props) int {
	if p.SpreadAngle <= 0 {
		return defaultSpreadAngle
	}
	return p.SpreadAngle
}

// effectiveSpreadDistance 空值缺省 120。
func effectiveSpreadDistance(p *Props) int {
	if p.SpreadDistance <= 0 {
		return defaultSpreadDistance
	}
	return p.SpreadDistance
}

// effectiveSpacing 空值缺省 26vh。
func effectiveSpacing(p *Props) string {
	if p.Spacing == "" {
		return defaultSpacing
	}
	return p.Spacing
}

// effectiveStickyTop 空值缺省 50%。
func effectiveStickyTop(p *Props) string {
	if p.StickyTop == "" {
		return defaultSticky
	}
	return p.StickyTop
}

// effectiveScaleBase 空值缺省 94（%）。
func effectiveScaleBase(p *Props) int {
	if p.ScaleBase <= 0 {
		return defaultScaleBase
	}
	return p.ScaleBase
}

// effectiveScaleStep 空值缺省 2（%）：与其它数值 prop 一致，0 视作未设置。
func effectiveScaleStep(p *Props) int {
	if p.ScaleStep <= 0 {
		return defaultScaleStep
	}
	return p.ScaleStep
}

// zoomEnabled 点击放大默认开启（显式 off 才关）。
// 全屏分页自动关闭：卡片本来就占满一屏，再"放大到视口中央"等于原地不动。
func zoomEnabled(p *Props) bool {
	if effectiveTrigger(p) == TriggerSlide {
		return false
	}
	return p.Zoom != "off"
}

// hasContent 是否用子节点作为卡片内容。
func hasContent(node *core.Node) bool { return node != nil && len(node.Children) > 0 }

// cardCount 卡片数量：有子节点即子节点数，否则占位卡数量。
func cardCount(node *core.Node, p *Props) int {
	if hasContent(node) {
		return len(node.Children)
	}
	return effectiveCount(p)
}

// cardSize 卡片尺寸兜底：按触发方式与内容来源取模式缺省，用户给了值就用用户值。
func cardSize(p *Props, trigger string, content bool) (width, height string) {
	width, height = p.Width, p.Height
	defW, defH := widthNum, heightNum
	switch {
	case trigger == TriggerScroll:
		defW, defH = widthScroll, heightScroll
	case trigger == TriggerSlide:
		// 每屏一张：宽度占满容器，高度交给 SlideHeight（min-height 一屏）。
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

// num 紧凑数值（-4 / -1.5），供 calc() 表达式拼接。
func num(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// fnum 定点数值（4 位小数）：几何系数固定精度输出，保证产物字节稳定。
func fnum(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }

// scaleOf 第 i 张卡的缩放比（1 = 原尺寸），滚动模式用。
func scaleOf(i, base, step int) float64 { return float64(base+i*step) / 100 }

// keyframesName scroll-driven 每卡独立关键帧名（含节点 id，跨实例唯一）。
func keyframesName(id string, i int) string { return "sky-cs-" + id + "-" + strconv.Itoa(i+1) }

// CompileCSS 生成容器 / 轨道 / 卡片 / 放大层全部样式。
//
// 不变量：
//   - 逐卡几何编译期展开成 :nth-child(N) 规则，增删卡片 CSS 自动重算；
//   - 同 props 同字节（无随机、无运行时测量）；
//   - 收敛上限始终是用户参数，装不下时才按视口收缩（响应式，不改写参数）。
//
// cardN 为调用方解析后的卡片数（内容集合模式的条数运行期才知道），
// <= 0 时回退到静态数量（无子节点 → 占位卡数量；有子节点 → 子节点数）。
func CompileCSS(node *core.Node, p *Props, cardN int, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(node.ID)
	trigger := effectiveTrigger(p)
	// 集合模式同样是「内容卡」：走卡片级背景/内边距那套样式，而不是数字卡的巨字号居中。
	content := hasContent(node) || collectionSource(p) != ""
	n := cardN
	if n <= 0 {
		n = cardCount(node, p)
	}
	width, height := cardSize(p, trigger, content)

	// 容器：相对定位，同时是悬停模式绝对定位卡片的包含块。
	b.Add(core.BreakpointDesktop, sel, []string{
		"position: relative",
		"width: 100%",
		// 触屏点按的高亮块会盖在卡片上，统一去掉（卡片自身已有按压态）。
		"-webkit-tap-highlight-color: transparent",
	})

	switch trigger {
	case TriggerScroll:
		compileScrollCSS(b, sel, node.ID, p, n, width, height, content)
	case TriggerDrag:
		compileDragCSS(b, sel, p, n, width, height, content)
	case TriggerDeck:
		compileDeckCSS(b, sel, p, n, width, height, content)
	case TriggerSlide:
		compileSlideCSS(b, sel, p, n, height, content)
	default:
		compileHoverCSS(b, sel, p, n, width, height, content)
	}
	compileZoomCSS(b, sel, p, content)
	if collectionSource(p) != "" {
		compileCollectionCSS(b, sel)
		// 空状态：集合没有内容时给一块可见占位，而不是留一片空白（也可整体隐藏）。
		b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-empty", []string{
			"margin: 0",
			"padding: 48px 24px",
			"text-align: center",
			"opacity: .6",
			"background-color: " + colorSurface,
			"border: 1px dashed rgba(0,0,0,.16)",
			"border-radius: 16px",
		})
		b.Add(core.BreakpointDesktop, sel+".is-empty-hidden", []string{"display: none"})
	}
	// 键盘可达：拖拽类模式的容器可聚焦（tabindex=0），焦点环与组件库其余组件一致。
	if trigger == TriggerDrag || trigger == TriggerDeck {
		b.Add(core.BreakpointDesktop, sel+":focus-visible", core.FocusRingDecls())
	}
}

// compileCollectionCSS 集合卡片内部元素样式：卡内从上往下排（图 → 标题 → 正文 → 附注 → 链接）。
func compileCollectionCSS(b *core.CSSBuckets, sel string) {
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-card", []string{
		"justify-content: flex-start",
		"gap: 10px",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-img", []string{
		"width: 100%",
		"height: 150px",
		"object-fit: cover",
		"border-radius: 10px",
		"flex: 0 0 auto",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-title", []string{
		"margin: 0",
		"font-size: 1.15em",
		"line-height: 1.35",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-text", []string{
		"margin: 0",
		"font-size: .92em",
		"line-height: 1.55",
		"opacity: .78",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-meta", []string{
		"margin: 0",
		"font-size: 1.05em",
		"font-weight: 700",
		"color: " + colorPrimary,
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-link", []string{
		"align-self: flex-start",
		"margin-top: auto",
		"font-size: .88em",
		"color: " + colorPrimary,
		"text-decoration: none",
		"border-bottom: 1px solid currentColor",
	})
}

// cardBaseDecls 卡片外观（两种触发方式共用，差异部分由调用方追加）。
//
// 卡片级样式（背景/内边距/圆角/边框宽度）都由 props 决定，缺省按卡片来源取值 ——
// 这样「每张卡一个背景」不必再套一层容器，而套容器仍可用于更细的内部分区样式。
func cardBaseDecls(content bool, p *Props) []string {
	padding := p.CardPadding
	radius := p.CardRadius
	border := p.CardBorder
	background := p.CardBackground

	if content {
		if padding == "" {
			padding = contentPad
		}
		if radius == "" {
			radius = "16px"
		}
		if border == "" {
			border = strconv.Itoa(cardBorder) + "px"
		}
		if background == "" {
			background = colorSurface
		}
		// 卡内布局：与容器组件的 flex 参数同源，卡片因此可以当容器用。
		layout := pickEnum(p.CardLayout, "column", "column", "row")
		justify := pickEnum(p.CardJustify, "center", "flex-start", "center", "flex-end", "space-between")
		align := pickEnum(p.CardAlign, "center", "flex-start", "center", "flex-end", "stretch")
		gap := p.CardGap
		if gap == "" {
			gap = defaultCardGap
		}
		return []string{
			// border-box：卡片有内边距时，"卡片宽度/高度"必须含 padding 才是外尺寸 ——
			// 逐卡几何（扇形收敛、环形半径、每屏高度）都按 props 里的数值计算，
			// content-box 会让实际尺寸比参数大一圈，几何随之全部偏移。
			"box-sizing: border-box",
			"display: flex",
			"flex-direction: " + layout,
			"justify-content: " + justify,
			"align-items: " + align,
			"gap: " + gap,
			"padding: " + padding,
			"background-color: " + background,
			"color: var(--sky-cardstack-text, #1f2430)",
			fmt.Sprintf("border: %s solid rgba(0,0,0,.08)", border),
			"border-radius: " + radius,
			"box-shadow: 0 15px 50px rgba(0,0,0,.12)",
			"overflow: hidden",
			"transition: .5s",
			"cursor: zoom-in",
			"user-select: none",
		}
	}
	if radius == "" {
		radius = "8px"
	}
	if border == "" {
		border = strconv.Itoa(cardBorder) + "px"
	}
	if background == "" {
		background = colorPrimary
	}
	return []string{
		"box-sizing: border-box",
		"display: flex",
		"justify-content: center",
		"align-items: center",
		"background-color: " + background,
		fmt.Sprintf("border: %s solid rgba(0,0,0,.1)", border),
		"border-radius: " + radius,
		"box-shadow: 0 15px 50px rgba(0,0,0,.1)",
		"color: rgba(0,0,0,0)",
		"font-size: 8em",
		"font-weight: 700",
		"transition: .5s",
		"cursor: zoom-in",
		"user-select: none",
	}
}

// compileHoverCSS 悬停展开模式：卡片绝对堆叠于轨道中心，悬停时按位置派生散开。
//
// 形态差异只有一处 —— 变换顺序：
// 三种形态：
//   - fan              弧线：rotate() translate()，位移落在旋转后的坐标系，卡片沿弧线切向散开；
//   - line + horizontal 横排：纯 translate(x)，**不带任何旋转**，卡片平铺成一行；
//   - line + vertical   竖排：纯 translate(0, y)，**不带任何旋转**，卡片堆成一列。
//
// 收敛公式随之不同，三者都保证展开不撑出视口：
//
//	fan          |dx·cosθ + L·sinθ| + (W/2)cosθ + (H/2)sinθ ≤ 50vw − gutter
//	line 横排     |dx| + W/2 ≤ 50vw − gutter
//	line 竖排     |dy| + H/2 ≤ 50vh − gutter
func compileHoverCSS(b *core.CSSBuckets, sel string, p *Props, n int, width, height string, content bool) {
	track := sel + " .sky-cardstack-track"
	hueStep := float64(effectiveHueStep(p))
	angle := float64(effectiveSpreadAngle(p))
	dist := float64(effectiveSpreadDistance(p))
	mid := float64(n-1) / 2.0
	fan := effectiveShape(p) == ShapeFan
	vertical := !fan && effectiveDirection(p) == directionVertical

	// 轨道高度按形态预留，保证展开后不压到下方内容：
	//   fan        卡高 + 2×上抬量（展开时卡片整体上抬）
	//   line 横排  卡高（只横向铺开，纵向不越界）
	//   line 竖排  卡高 + 2×最大步距×位移（纵向整列铺开）
	trackHeight := fmt.Sprintf("min-height: calc(%s + %dpx)", height, 2*cardLiftY)
	switch {
	case vertical:
		trackHeight = fmt.Sprintf("min-height: calc(%s + %dpx)", height, int(2*mid*dist))
	case !fan:
		trackHeight = "min-height: " + height
	}
	b.Add(core.BreakpointDesktop, track, []string{
		"position: relative",
		"display: flex",
		"justify-content: center",
		"align-items: center",
		trackHeight,
		// 卡片 border box 高（含上下边框）：旋转外扩与竖排收敛都要用它，
		// 而 translate 的百分比只能拿到宽度，高度必须以变量传入。
		fmt.Sprintf("--sky-cardstack-h: calc(%s + %dpx)", height, 2*cardBorder),
	})

	for i := 0; i < n; i++ {
		offset := float64(i) - mid
		nth := strconv.Itoa(i + 1)
		card := track + " .sky-cardstack-card:nth-child(" + nth + ")"

		// 基础态：绝对堆叠；数字卡带位置派生色相，内容卡保持原色。
		decls := []string{"position: absolute", "width: " + width}
		if content {
			decls = append(decls, "min-height: "+height, "height: auto")
		} else {
			decls = append(decls, "height: "+height,
				fmt.Sprintf("filter: hue-rotate(%.0fdeg)", offset*hueStep))
		}
		decls = append(decls, cardBaseDecls(content, p)...)
		b.Add(core.BreakpointDesktop, card, decls)

		// 悬停展开：旋转 + 平移 + 文字/阴影加深；被放大的那张卡退出扇形逻辑。
		// 注意：:hover 挂在容器上，经轨道下到卡片；track 变量本身已含 sel 前缀，
		// 这里不能再拼一次（否则是「容器:hover 容器 轨道 …」这种永不匹配的选择器）。
		hover := sel + ":hover .sky-cardstack-track .sky-cardstack-card:nth-child(" + nth +
			"):not(:has(> .sky-cardstack-toggle:checked))"
		var fixed, adaptive string
		switch {
		case fan:
			// 弧线：收敛 = 视口半宽 − 留白 − 旋转外扩 −（上抬量被旋转投影的那一份），
			// 再除以 cosθ·最大步距（dx 要先经 cosθ 才变成世界坐标的水平位移）。
			rad := math.Abs(offset*angle) * math.Pi / 180
			cosT, sinT := math.Cos(rad), math.Sin(rad)
			allow := fmt.Sprintf("calc((50vw - %dpx - %spx - %s * 50%% - %s * var(--sky-cardstack-h) / 2) / %s)",
				viewportGutter, fnum(cardLiftY*sinT), fnum(cosT), fnum(sinT), fnum(cosT*mid))
			fixed = fmt.Sprintf("rotate(%sdeg) translate(%spx, -%dpx)", num(offset*angle), num(offset*dist), cardLiftY)
			adaptive = fmt.Sprintf("rotate(%sdeg) translate(calc(%s * clamp(0px, %s, %dpx)), -%dpx)",
				num(offset*angle), num(offset), allow, int(dist), cardLiftY)
		case vertical:
			// 竖排：不带旋转、横向不动，收敛 = 视口半高 − 留白 − 卡半高。
			allow := fmt.Sprintf("calc((50vh - %dpx - var(--sky-cardstack-h) / 2) / %s)", viewportGutter, num(mid))
			fixed = fmt.Sprintf("translate(0px, %spx)", num(offset*dist))
			adaptive = fmt.Sprintf("translate(0px, calc(%s * clamp(0px, %s, %dpx)))",
				num(offset), allow, int(dist))
		default:
			// 横排：不带旋转、纵向不动（堆叠态本来就在容器中线），
			// 收敛 = 视口半宽 − 留白 − 卡半宽（50% 即 border box 半宽）。
			allow := fmt.Sprintf("calc((50vw - %dpx - 50%%) / %s)", viewportGutter, num(mid))
			fixed = fmt.Sprintf("translate(%spx, 0px)", num(offset*dist))
			adaptive = fmt.Sprintf("translate(calc(%s * clamp(0px, %s, %dpx)), 0px)",
				num(offset), allow, int(dist))
		}
		hoverDecls := []string{
			"transform: " + fixed,
			"transform: " + adaptive,
			"color: " + colorLabel,
			"box-shadow: 0 15px 50px rgba(0,0,0,.25)",
		}
		// 悬停循环效果：只加在悬停规则里，移开鼠标动画自然停止。
		if kf := loopEffectKey(p.HoverEffect); kf != "" {
			b.NeedKeyframes(kf)
			hoverDecls = append(hoverDecls, "animation: "+kf+" 2s ease-in-out infinite")
		}
		b.AddHover(hover, hoverDecls)
	}

	// 按压：容器按下时全部卡变暗；被点的卡恢复原色（并显形数字）后置顶。
	// 走 AddActive（不包 hover:hover）：触屏按下同样触发 —— 触屏没有 hover，
	// 数字颜色若只写在悬停规则里，移动端将永远看不到卡片数字。
	b.AddActive(sel+":active .sky-cardstack-card", []string{"background-color: " + colorDim})
	b.AddActive(sel+" .sky-cardstack-card:active", []string{
		"background-color: " + colorPrimary,
		"color: " + colorLabel,
		"z-index: 100",
	})
}

// compileScrollCSS 滚动堆叠模式（sticky + CSS scroll-driven，零 JS）。
//
// 基础规则本身就是「降级形态」：sticky 层叠 + 静态缩放，完全不依赖新特性。
// 跟手收敛叠在同一条规则的 animation 声明上 —— 支持 scroll-driven 的浏览器用
// view() 时间线驱动它；不支持的浏览器把 animation-timeline / animation-range
// 当未知属性丢弃，动画按 0s 播完并由 fill-mode: both 停在终态，视觉与静态缩放一致。
// 因此不需要 @supports 包裹：未知属性天然被忽略，降级是「白送」的。
func compileScrollCSS(b *core.CSSBuckets, sel, id string, p *Props, n int, width, height string, content bool) {
	track := sel + " .sky-cardstack-track"
	spacing := effectiveSpacing(p)
	stickyTop := effectiveStickyTop(p)
	base, step := effectiveScaleBase(p), effectiveScaleStep(p)

	// 轨道：块级垂直排列（sticky 在块流里行为最稳），上下留一点滚动余量。
	b.Add(core.BreakpointDesktop, track, []string{"display: block", "padding: 6vh 0"})

	for i := 0; i < n; i++ {
		nth := strconv.Itoa(i + 1)
		card := track + " .sky-cardstack-card:nth-child(" + nth + ")"
		s := scaleOf(i, base, step)
		kf := keyframesName(id, i)
		decls := []string{
			"position: sticky",
			"top: " + stickyTop,
			// 配合 top 把卡片在粘住位置垂直居中（独立属性，与悬停模式的 transform 互不干扰）。
			"translate: 0 -50%",
			"margin: 0 auto " + spacing,
			// 序号越大越靠上：后出现的卡盖住先出现的。
			"z-index: " + strconv.Itoa(i+1),
			fmt.Sprintf("scale: %s", fnum(s)),
			"width: min(100%, " + width + ")",
			// 跟手收敛：卡片进入视口的区间内从略大略淡收到目标态。
			"animation: " + kf + " linear both",
			"animation-timeline: view()",
			"animation-range: entry 0% entry 60%",
		}
		if content {
			decls = append(decls, "min-height: "+height)
		} else {
			decls = append(decls, "height: "+height)
		}
		decls = append(decls, cardBaseDecls(content, p)...)
		b.Add(core.BreakpointDesktop, card, decls)

		// 每张卡独立关键帧：终态是该卡的静态缩放，降级时正好停在同一点。
		b.AddKeyframesDecls(kf, []string{
			fmt.Sprintf("from { scale: %s; opacity: .5 }", fnum(s*1.12)),
			fmt.Sprintf("to { scale: %s; opacity: 1 }", fnum(s)),
		})
	}
}

// compileDragCSS 拖拽旋转模式（路径 C：构建期输出环形骨架 + data-* 属性，
// 公共增强脚本 enhance.js 按需初始化）。
//
// 几何：第 i 张卡落在半径 R 的圆周上（角度 360i/N），卡片**始终正立** ——
// 变换链 translate(-50%,-50%) → rotate(θ+rot) → translateY(-R) → rotate(-(θ+rot))：
// 第一个 rotate 把位移送到圆周方向，后一个把它转回来抵消朝向，所以卡片沿圆环走位而不歪。
//
// 降级：没有增强脚本时 --sky-cardstack-rot 恒为 0deg，卡片静态环形分布 ——
// 比堆叠态更接近最终形态，且点击放大、键盘聚焦照旧可用，不依赖 JS 才看得见。
func compileDragCSS(b *core.CSSBuckets, sel string, p *Props, n int, width, height string, content bool) {
	track := sel + " .sky-cardstack-track"
	radius := dragRadius(p, n, width, height)
	cardH := cssPx(height, fallbackCardH)

	// 容器整块可拖拽；touch-action 只让出纵向，横向留给旋转（否则移动端拖不动页面）。
	b.Add(core.BreakpointDesktop, sel, []string{
		"position: relative",
		"width: 100%",
		"cursor: grab",
		"touch-action: pan-y",
		// 触屏点按的高亮块会盖在卡片上，去掉（卡片自身已有按压态）。
		"-webkit-tap-highlight-color: transparent",
	})
	b.Add(core.BreakpointDesktop, sel+".is-dragging", []string{"cursor: grabbing"})
	// 轨道高度 = 圆周外接盒（2R + 卡高），与相邻区块不会重叠。
	b.Add(core.BreakpointDesktop, track, []string{
		"position: relative",
		"display: block",
		fmt.Sprintf("height: %dpx", int(2*radius+cardH)),
		// 旋转角由增强脚本改写；无脚本时保持 0，卡片静态成环。
		"--sky-cardstack-rot: 0deg",
	})

	hueStep := float64(effectiveHueStep(p))
	for i := 0; i < n; i++ {
		angle := 360 * float64(i) / float64(n)
		card := track + " .sky-cardstack-card:nth-child(" + strconv.Itoa(i+1) + ")"
		decls := []string{
			"position: absolute",
			"left: 50%",
			"top: 50%",
			"width: " + width,
			fmt.Sprintf("transform: translate(-50%%, -50%%) rotate(calc(%sdeg + var(--sky-cardstack-rot, 0deg))) translateY(-%dpx) rotate(calc(-%sdeg - var(--sky-cardstack-rot, 0deg)))",
				num(angle), int(radius), num(angle)),
			"transition: transform .35s ease",
		}
		if content {
			decls = append(decls, "min-height: "+height, "height: auto")
		} else {
			// 环形没有「中间卡」，色相直接按序号均匀铺开。
			decls = append(decls, "height: "+height,
				fmt.Sprintf("filter: hue-rotate(%.0fdeg)", float64(i)*hueStep))
		}
		decls = append(decls, cardBaseDecls(content, p)...)
		b.Add(core.BreakpointDesktop, card, decls)
	}

	// 拖拽过程中取消过渡，否则卡片会追着指针慢半拍。
	b.Add(core.BreakpointDesktop, sel+".is-dragging .sky-cardstack-card", []string{"transition: none"})
}

// slideEffectKeyframe 切换动画 → 通用动效词汇名（不新增关键帧，直接复用 core 那套）。
// 返回空串 = 无动画（缺省，只有位置变化）。
func slideEffectKeyframe(p *Props) string {
	horizontal := p.SlideDirection == slideDirectionHorizontal
	switch p.SlideEffect {
	case "fade":
		if horizontal {
			return "sky-fade-in-bottom-right"
		}
		return "sky-fade-in-bottom-left"
	case "zoom":
		if horizontal {
			return "sky-zoom-in-right"
		}
		return "sky-zoom-in-up"
	case "flip":
		if horizontal {
			return "sky-flip-in-y"
		}
		return "sky-flip-in-x"
	case "bounce":
		if horizontal {
			return "sky-bounce-in-right"
		}
		return "sky-bounce-in-up"
	case "back":
		if horizontal {
			return "sky-back-in-right"
		}
		return "sky-back-in-up"
	case "rotate":
		if horizontal {
			return "sky-rotate-in-up-right"
		}
		return "sky-rotate-in-up-left"
	case "light":
		if horizontal {
			return "sky-light-speed-in-right"
		}
		return "sky-light-speed-in-left"
	case "roll":
		return "sky-roll-in"
	case "jack":
		return "sky-jack-in-the-box"
	}
	return ""
}

// loopEffectKey 循环效果 → 通用词汇名（只返回不占用 transform 的两条：
// glow 走 filter、flash 走 opacity；其余 loop 词汇都改 transform，会顶掉位移/缩放）。
func loopEffectKey(v string) string {
	switch v {
	case "glow":
		return "sky-loop-glow"
	case "flash":
		return "sky-loop-flash"
	}
	return ""
}

// deckEasing 切换曲线预设（缺省平滑缓出）。
func deckEasing(p *Props) string {
	switch p.DeckTransition {
	case "spring":
		return "cubic-bezier(.34,1.56,.64,1)" // 轻微过冲，手感像回弹
	case "ease-out":
		return "ease-out"
	case "linear":
		return "linear"
	}
	return "cubic-bezier(.22,.61,.36,1)"
}

// effectiveDeckDuration 切换时长（ms），缺省 450。
func effectiveDeckDuration(p *Props) int {
	if p.DeckDuration < 200 {
		return 450
	}
	return p.DeckDuration
}

// deckTransform 堆叠轮播的变换：横向沿 X 位移，纵向沿 Y 位移（缩放与层级共用偏移绝对值）。
func deckTransform(offset, rot int, scaleStep float64, vertical bool) string {
	axis := "X"
	if vertical {
		axis = "Y"
	}
	return fmt.Sprintf("transform: translate(-50%%, -50%%) translate%s(calc(var(--sky-deck-off, 0) * %d%%)) rotate(calc(var(--sky-deck-off, 0) * %ddeg)) scale(calc(1 - var(--sky-deck-abs, 0) * %s))",
		axis, offset, rot, fnum(scaleStep))
}

// dragRadius 环形半径：用户值优先；0 = 自动，保证相邻卡片弦长不小于卡宽。
func dragRadius(p *Props, n int, width, height string) float64 {
	if p.DragRadius > 0 {
		return float64(p.DragRadius)
	}
	cardW := cssPx(width, fallbackCardW)
	cardH := cssPx(height, fallbackCardH)
	if n < 2 {
		return cardH / 2
	}
	// 相邻夹角 2π/n → 弦长 2R·sin(π/n)；要 ≥ 卡宽，故 R ≥ 卡宽 / (2·sin(π/n))。
	r := cardW / (2 * math.Sin(math.Pi/float64(n)))
	if min := cardH/2 + 24; r < min {
		r = min
	}
	return r
}

// pickEnum 枚举白名单：不在白名单内退回缺省 —— 枚举值会直接进入 CSS 声明，
// 必须封闭（不靠 Validate 兜底，编译期也要自防御）。
func pickEnum(v, def string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return def
}

// cssPx 取 px 数值；非 px 单位退回兜底值 —— 环形半径要在编译期算三角函数，
// 拿不到百分比（用户把宽度写成 % 时按兜底值算半径，几何仍成立，只是留白可能偏大）。
func cssPx(v string, fallback float64) float64 {
	if s, ok := strings.CutSuffix(v, "px"); ok {
		if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil && f > 0 {
			return f
		}
	}
	return fallback
}

// compileDeckCSS 堆叠轮播：主卡居中正立，两侧卡片按「相对主卡的偏移」叠开。
//
// 几何全部由每张卡的两个 CSS 变量驱动：
//
//	--sky-deck-off  相对当前主卡的偏移（整数，0 = 主卡）
//	--sky-deck-abs  偏移的绝对值（CSS 没有 abs()，缩放/层级要用它）
//
// 编译期逐卡写入的是**静态降级值**（i - mid）：没有增强脚本时卡片按序号摊开成一摞，
// 依旧可点、可放大；脚本接管后改写为「相对主卡」的偏移 —— 切换主卡只改这两个变量，
// 位移/倾斜/缩放/层级的关系全在静态 CSS 里，脚本端不碰任何几何数值。
func compileDeckCSS(b *core.CSSBuckets, sel string, p *Props, n int, width, height string, content bool) {
	track := sel + " .sky-cardstack-track"
	offset := effectiveDeckOffset(p)
	rot := effectiveDeckRotate(p)
	scaleStep := float64(effectiveDeckScaleStep(p)) / 100
	vertical := effectiveDeckDirection(p) == "vertical"
	if vertical && p.DeckOffset <= 0 {
		offset = defaultDeckOffsetVertical
		// 纵向倾斜减半：竖向位移配大角度会显得歪。
		if p.DeckRotate <= 0 {
			rot = defaultDeckRotate / 2
		}
	}
	mid := float64(n-1) / 2.0

	// 轨道高度：倾斜 + 缩放后卡片的外接盒，按最大偏移保守预留。
	maxOff := math.Max(mid, 1)
	cardW := cssPx(width, fallbackCardW)
	cardH := cssPx(height, fallbackCardH)
	tiltOut := cardW * math.Sin(float64(rot)*math.Pi/180) * maxOff * 0.35
	trackH := cardH + 2*math.Max(24, tiltOut)

	// touch-action 必须**跟着切换轴走**：切换方向的轴归 JS（否则触摸手势被浏览器
	// 拿去滚页面，滑动切换在触屏上完全失效），另一个轴让给页面滚动。
	touchAction := "pan-y" // 横向切换：纵向留给页面
	if vertical {
		touchAction = "pan-x" // 纵向切换：横向留给页面
	}
	b.Add(core.BreakpointDesktop, sel, []string{
		"position: relative",
		"width: 100%",
		"cursor: grab",
		"touch-action: " + touchAction,
		// 触屏点按的高亮块会盖在卡片上，去掉（卡片自身已有按压态）。
		"-webkit-tap-highlight-color: transparent",
	})
	b.Add(core.BreakpointDesktop, sel+".is-dragging", []string{"cursor: grabbing"})
	b.Add(core.BreakpointDesktop, track, []string{
		"position: relative",
		"display: block",
		fmt.Sprintf("height: %dpx", int(trackH)),
	})

	hueStep := float64(effectiveHueStep(p))
	for i := 0; i < n; i++ {
		static := float64(i) - mid
		card := track + " .sky-cardstack-card:nth-child(" + strconv.Itoa(i+1) + ")"
		decls := []string{
			// 静态降级值：按序号摊开（无脚本时的形态）。
			fmt.Sprintf("--sky-deck-off: %s", num(static)),
			fmt.Sprintf("--sky-deck-abs: %s", num(math.Abs(static))),
			"position: absolute",
			"left: 50%",
			"top: 50%",
			"width: " + width,
			deckTransform(offset, rot, scaleStep, vertical),
			"z-index: calc(50 - var(--sky-deck-abs, 0))",
			// 越远越淡：卡片多时不至于在两侧无限堆远（max() 不被支持时退化为全不透明，不影响可用性）。
			"opacity: max(0, calc(1 - var(--sky-deck-abs, 0) * 0.28))",
			"transition: transform " + strconv.Itoa(effectiveDeckDuration(p)) + "ms " + deckEasing(p) + ", box-shadow .3s, opacity .3s",
			"cursor: pointer",
		}
		if content {
			decls = append(decls, "min-height: "+height, "height: auto")
		} else {
			decls = append(decls, "height: "+height,
				fmt.Sprintf("filter: hue-rotate(%.0fdeg)", static*hueStep))
		}
		decls = append(decls, cardBaseDecls(content, p)...)
		b.Add(core.BreakpointDesktop, card, decls)
	}

	// 主卡：抬升层级 + 加重投影（由脚本切 is-active 类）；可选循环高亮。
	activeDecls := []string{
		"z-index: 60",
		"box-shadow: 0 24px 60px rgba(0,0,0,.26)",
	}
	if kf := loopEffectKey(p.DeckHighlight); kf != "" {
		b.NeedKeyframes(kf)
		activeDecls = append(activeDecls, "animation: "+kf+" 2s ease-in-out infinite")
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-card.is-active", activeDecls)
	// 拖动过程中取消过渡，否则卡片追着指针慢半拍。
	b.Add(core.BreakpointDesktop, sel+".is-dragging .sky-cardstack-card", []string{"transition: none"})
}

// compileSlideCSS 全屏分页：一屏一张卡，原生滚动吸附切换。
//
// 关键取舍：**用 scroll-snap，不劫持滚动**。滚动条本身仍归浏览器管 ——
// 惯性、触控板、键盘 PageDown/空格、屏幕阅读器全都照旧可用；JS 滚动劫持
// （wheel + preventDefault + 自算动画）在移动端与辅助技术上是灾难，不值得。
//
// 每屏高度用 dvh 而非 vh：移动端地址栏收放时 vh 会跳、内容跟着抖，dvh 会跟着变。
// 卡片给 min-height（不是 height）：内容超出一屏时卡片自己长高、原地可读，
// 而不是被裁掉或压成卡内滚动条 —— 但这属于「这一屏内容太多了」，应在内容侧解决。
func compileSlideCSS(b *core.CSSBuckets, sel string, p *Props, n int, height string, content bool) {
	track := sel + " .sky-cardstack-track"
	screen := strings.TrimSpace(p.SlideHeight)
	if screen == "" {
		screen = defaultSlideHeight
	}
	// 每屏高度降级链：dvh 不认识时退回 vh（老浏览器仍是一屏一张，只是地址栏收放时略跳）。
	// 降级链顺序：**旧值在前、新值在后** —— 后写的覆盖先写的。
	// 反过来写的话 dvh 会被 vh 永久盖掉，等于白写。
	screenDecl := []string{"height: " + screen}
	cardMin := []string{"min-height: " + screen}
	if strings.HasSuffix(screen, "dvh") {
		fallback := strings.TrimSuffix(screen, "dvh") + "vh"
		screenDecl = []string{"height: " + fallback, "height: " + screen}
		cardMin = []string{"min-height: " + fallback, "min-height: " + screen}
	}

	b.Add(core.BreakpointDesktop, sel, []string{
		"position: relative",
		"width: 100%",
	})
	// 铺满视口：容器脱离文档流，父容器的内边距与宽度都不再影响它 —— 省掉「手动把
	// 父容器 padding 归零」这一步。代价是它与页面同级内容会重叠，只适合「整页只有它」。
	if p.SlideFit == slideFitViewport {
		fitDecls := []string{
			"position: fixed",
			"inset: 0",
			"z-index: 30",
			"width: 100vw",
		}
		fitDecls = append(fitDecls, screenDecl...)
		b.Add(core.BreakpointDesktop, sel, fitDecls)
	}
	// 滚动方向：纵向滚动用 Y 轴与 pan-y，横向滚动把整套换成 X 轴。
	horizontal := p.SlideDirection == slideDirectionHorizontal
	scrollAxis, snapAxis := "y", "y"
	overflowMain, overflowCross := "overflow-y: auto", "overflow-x: hidden"
	snapAlign := "scroll-snap-align: start"
	stickyAxis := "top: 0"
	if horizontal {
		scrollAxis, snapAxis = "x", "x"
		overflowMain, overflowCross = "overflow-x: auto", "overflow-y: hidden"
		snapAlign = "scroll-snap-align: start"
		stickyAxis = "left: 0"
	}
	_ = scrollAxis

	// 滚动容器：原生滚动 + 强制吸附（一次只翻一屏）。
	trackDecls := append([]string{
		"position: relative",
		"display: block",
		overflowMain,
		overflowCross,
		"scroll-snap-type: " + snapAxis + " mandatory",
		"-webkit-overflow-scrolling: touch",
		// 隐藏滚动条：全屏分页里滚动条是纯视觉噪音，右下角的页码角标已经说明了位置。
		// 隐藏不影响滚动本身 —— 滚轮、触摸板、键盘、触屏手势照旧。
		"scrollbar-width: none",
		"-ms-overflow-style: none",
	}, screenDecl...)
	// WebKit/Blink 用伪元素隐藏（scrollbar-width 在它们上面还不生效）。
	b.Add(core.BreakpointDesktop, track+"::-webkit-scrollbar", []string{
		"display: none",
		"width: 0",
		"height: 0",
	})
	if horizontal {
		// 横向分页必须让卡片真正横排：块级元素默认纵向堆叠，宽度不会溢出，
		// 轨道 scrollWidth 恒等于 clientWidth —— 滚动条根本出不来（实测踩过）。
		trackDecls = append(trackDecls, "display: flex", "flex-direction: row")
	}
	// 页码：CSS counter 自动编号（卡片逐个 increment），总数由编译期写进 attr()——
	// 全程零 JS，滚动中也能看出「第几屏 / 共几屏」。
	trackDecls = append(trackDecls, "counter-reset: sky-page")
	b.Add(core.BreakpointDesktop, track, trackDecls)
	b.Add(core.BreakpointDesktop, track+" .sky-cardstack-page", []string{
		"position: absolute",
		"right: 20px",
		"bottom: 16px",
		"font-size: 13px",
		"letter-spacing: .08em",
		"opacity: .45",
		"pointer-events: none",
	})
	b.Add(core.BreakpointDesktop, track+" .sky-cardstack-page::before", []string{
		`content: counter(sky-page) " / " attr(data-total)`,
	})

	for i := 0; i < n; i++ {
		card := track + " .sky-cardstack-card:nth-child(" + strconv.Itoa(i+1) + ")"
		decls := []string{
			"width: 100%",
			"counter-increment: sky-page",
			snapAlign,
			// always：一次手势只翻一屏，不会连跳好几屏。
			"scroll-snap-stop: always",
		}
		if horizontal {
			// flex 子项不能靠 width: 100% 定宽（会被压缩），用 flex 基准定成整屏宽。
			decls = append(decls, "flex: 0 0 100%")
		}
		// 卡片本身不挂动画：入场动画与当前屏高亮都由脚本在卡片上切换类名触发
		// （见本函数末尾的 .is-enter / .is-current 规则）。
		//
		// 为什么不用 animation-timeline: view()：slide 的轨道是**内嵌滚动容器**，
		// 实测 view() 时间线在该场景下不驱动动画 —— 时间线对象创建成功、进度随滚动
		// 正常变化，但元素的计算值（transform / filter）恒为初始值，动画等于没跑。
		// 换成脚本驱动后行为与 hover / deck 的循环效果一致，且不依赖浏览器新特性。
		if p.SlideStack {
			// 堆叠翻页：每张卡都粘在同一位置，靠递增 z-index 让后一张**盖住**前一张。
			// 平铺时上滑会把前一张推走，堆叠时它留在原地被覆盖 —— 视觉上是「翻页」。
			// 纯 sticky + z-index，零 JS、不依赖 scroll-driven。
			decls = append(decls,
				"position: sticky",
				stickyAxis,
				"z-index: "+strconv.Itoa(i+1),
			)
		}
		decls = append(decls, cardMin...)
		if content {
			decls = append(decls, "height: auto")
		}
		decls = append(decls, cardBaseDecls(content, p)...)
		// 全屏卡片自己就是页面，圆角与投影会露出拼接感，去掉。
		decls = append(decls, "border-radius: 0", "box-shadow: none")
		b.Add(core.BreakpointDesktop, card, decls)
	}

	// 入场动画：卡片进入轨道视口时脚本加 .is-enter，播一次后由脚本在它离开时移除，
	// 这样往回滚可以重播。duration 固定 800ms —— 参数只负责「选哪种效果」。
	if kf := slideEffectKeyframe(p); kf != "" {
		b.NeedKeyframes(kf)
		b.Add(core.BreakpointDesktop, track+" .sky-cardstack-card.is-enter", []string{
			"animation: " + kf + " 800ms cubic-bezier(.22,.61,.36,1) both",
		})
	}
	// 当前屏高亮：卡片基本占满视口时脚本加 .is-current，离开即移除，持续循环。
	if kf := loopEffectKey(p.SlideHighlight); kf != "" {
		b.NeedKeyframes(kf)
		b.Add(core.BreakpointDesktop, track+" .sky-cardstack-card.is-current", []string{
			"animation: " + kf + " 2s ease-in-out infinite",
		})
	}
}

// compileZoomCSS 点击放大到视口中央（零 JS：label + radio，同组互斥，一次只放大一张）。
//
// 卡片本身就是 label，内部藏一个 radio：点卡片即选中它；遮罩是末尾那个 label，
// 点它选中同组的关闭 radio，所有卡随之回到原位。
func compileZoomCSS(b *core.CSSBuckets, sel string, p *Props, content bool) {
	if !zoomEnabled(p) {
		return
	}
	// 隐藏但可聚焦的单选：键盘 Tab 能聚焦、Space/方向键能切换。
	// 关闭单选单独写一条：它不能共用 toggle 类，否则「有 toggle 被选中」在关闭后
	// 依然为真（关闭单选自己被选中），遮罩与关闭按钮就再也收不回去。
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-toggle", []string{
		"position: absolute",
		"width: 1px",
		"height: 1px",
		"margin: 0",
		"padding: 0",
		"border: 0",
		"opacity: 0",
		// 点击穿透到 label：label 的原生行为会激活它内部的控件。
		"pointer-events: none",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-close", []string{
		"position: absolute",
		"width: 1px",
		"height: 1px",
		"margin: 0",
		"padding: 0",
		"border: 0",
		"opacity: 0",
		"pointer-events: none",
	})
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-card:focus-within", core.FocusRingDecls())

	// 放大态：脱离堆叠、居中到视口。用 inset:0 + margin:auto 居中而不是 transform，
	// 免得和悬停展开/滚动位移抢属性；同时清掉独立属性 translate/scale 的残留。
	zoom := []string{
		"position: fixed",
		"inset: 0",
		"margin: auto",
		"width: min(88vw, 560px)",
		"height: auto",
		"min-height: min(70vh, 560px)",
		"max-height: 86vh",
		"translate: none",
		"scale: 1",
		"transform: none",
		"z-index: 1001",
		"box-shadow: 0 30px 90px rgba(0,0,0,.45)",
		"cursor: zoom-out",
	}
	if content {
		zoom = append(zoom, "overflow: auto")
	} else {
		zoom = append(zoom, "color: "+colorLabel, "font-size: clamp(3rem, 20vw, 13rem)")
	}
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-card:has(> .sky-cardstack-toggle:checked)", zoom)

	// 遮罩：默认不占位；有卡被放大时铺满视口，点它即关闭。
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-scrim", []string{
		"display: none",
		"position: fixed",
		"inset: 0",
		"z-index: 1000",
		"background: rgba(12,14,26,.72)",
		"cursor: zoom-out",
	})
	b.Add(core.BreakpointDesktop, sel+":has(.sky-cardstack-toggle:checked) .sky-cardstack-scrim", []string{"display: block"})

	// 显式关闭按钮：radio 无法「再点一次取消」，所以关闭必须由另一个控件完成 ——
	// 遮罩（点空白处）与这个按钮（点右上角）都指向同一个关闭单选。
	b.Add(core.BreakpointDesktop, sel+" .sky-cardstack-close-btn", []string{
		"display: none",
		"position: fixed",
		"top: 20px",
		"right: 24px",
		"z-index: 1002",
		"width: 40px",
		"height: 40px",
		"align-items: center",
		"justify-content: center",
		"font-size: 20px",
		"line-height: 1",
		"color: #fff",
		"background: rgba(255,255,255,.16)",
		"border-radius: 9999px",
		"cursor: zoom-out",
		"user-select: none",
	})
	b.Add(core.BreakpointDesktop, sel+":has(.sky-cardstack-toggle:checked) .sky-cardstack-close-btn", []string{"display: flex"})
}
