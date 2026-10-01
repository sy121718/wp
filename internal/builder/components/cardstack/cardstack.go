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
	_ "embed" // enhance.js 经 //go:embed 打进二进制
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// 组件行为源。与 .go / .css / .jet 同目录：改交互不必再去 enhance.js 里找。
//
//go:embed enhance-drag.js
var enhanceDragJS string

//
//go:embed enhance-deck.js
var enhanceDeckJS string

//
//go:embed enhance-slide.js
var enhanceSlideJS string

// cardstackCSS 组件样式源。与 .go / .css / .jet 同目录：改样式不必再进 Go 字符串数组。
//
//go:embed cardstack.css
var cardstackCSS string

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
	// ShapeBook 翻开的书：静止时卡片竖起叠成书脊，悬停时向两侧摊开。
	ShapeBook = "book"
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
	// bookClosedDeg 书合上时每页竖起的角度（略小于 90，留一线厚度感）。
	bookClosedDeg = 86
	// cardLiftY 悬停上抬量（px）：容器据此在上下各预留同等空间。
	cardLiftY = 50
	// cardBorder 卡片边框宽度（px）：参与 border box 换算（translate 的百分比基于 border box）。
	cardBorder = 10
	// viewportGutter 横向收敛时给视口边缘留的安全余量（px）。
	// 桌面浏览器 100vw 含经典滚动条（≈15px，即半宽 7.5px），收敛式按 50vw 算出的位移
	// 会比实际可视半宽多 7.5px —— 留白必须盖住这份误差（实测 1024 视口曾溢出 7px）。
	viewportGutter = 16
	// dragPerspective 圆柱环绕的透视距离（px），与样式源里输出的 perspective 保持一致。
	// 卡片在 translateZ(R) 处被放大 d/(d−R) 倍，算单侧可用空间时必须把这份放大计入占位。
	dragPerspective = 1600.0
	// dragGutter 环形/圆柱每侧留白（px）：12px 视觉留白 + 7.5px 滚动条误差 + 余量。
	dragGutter = 20
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
	// deckClickNext DeckClick 的「翻下一页」取值。
	deckClickNext = "next"
	// dragModeCylinder DragMode 的三维环绕取值。
	dragModeCylinder = "cylinder"
	// fallbackCardW / fallbackCardH 拖拽旋转算环形半径时的兜底卡片尺寸（非 px 宽度时使用）。
	fallbackCardW = 320
	fallbackCardH = 240
)

// 取色（与 badge/quote/progress 同一约定：CSS 变量 + 兜底色，主题可整体覆写）。
// 色值本身写在 cardstack.css 里；这里只留 Go 侧算兜底值时引用的两个。
const (
	colorPrimary = "var(--sky-c-primary, #2563eb)"
	colorSurface = "var(--sky-c-surface, #fff)"
)

// Props 卡片堆叠属性。
type Props struct {
	// Trigger 触发方式：hover 悬停展开 / scroll 滚动堆叠（纯 CSS）/ drag 拖拽旋转（增强脚本）。
	Trigger string `json:"trigger,omitempty" ct:"select,hover=悬停展开,scroll=滚动堆叠,drag=拖拽旋转,deck=堆叠轮播,slide=全屏分页,default=hover,sec=content,label=触发方式"`
	// Shape 展开形态（悬停模式）：fan 弧线扇形 / line 排开（卡片不带任何角度）。
	Shape string `json:"shape,omitempty" ct:"select,fan=扇形展开,line=直线排开,book=翻开的书,default=fan,sec=content,label=展开形态"`
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
	// DeckClick 点击卡片的行为：zoom 展开放大 / next 直接翻到下一页。
	// next 是「看书」的语义 —— 点哪儿都往后翻一页，与滑动 / 拖拽 / 按钮并用。
	DeckClick string `json:"deckClick,omitempty" ct:"select,zoom=展开放大,next=翻到下一页,default=zoom,sec=motion,label=点击卡片"`
	// DeckArrows 显示「上一页 / 下一页」按钮（仅 deck 模式）：
	// 点击按钮切换主卡；不用按钮时拖拽 / 滚轮 / 方向键 / 点侧卡同样能切；
	// 点主卡则是展开放大 —— 三种操作各管一件事，互不打架。
	DeckArrows bool `json:"deckArrows,omitempty" ct:"bool,sec=motion,label=翻页按钮"`
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
	// DragMode 拖拽的排布方式：ring 平面圆环 / cylinder 三维圆柱环绕。
	// cylinder 用 perspective + rotateY + translateZ 把卡片贴到圆柱面上，拖动转 360° ——
	// 视觉上是"一页页围成一圈"，正对观察者的那张最清楚。
	DragMode string `json:"dragMode,omitempty" ct:"select,ring=平面圆环,cylinder=三维环绕,default=ring,sec=motion,label=排布方式"`
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
	// ListPageLink 列表页链接的文案（留空按集合源取缺省：「更多文章」/「全部商品」）。
	//
	// 链接**目标不由作者填**：它来自系统页面槽位（文章集合 → blog、商品集合 → shop，BIZ-2）——
	// 「列表页是哪一页」是站点级事实，运维在槽位里绑一次，页面改 URL 后链接自动跟着走。
	// 槽位没绑或那页没发布时**整块不输出**（绝不猜路径：猜错的链接就是死链，
	// 而作者从产物上看不出它是猜的）。
	ListPageLink string `json:"listPageLink,omitempty" ct:"text,maxlen=30,sec=collection,label=列表页链接文案"`
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

// CollectionProp 实现 core.CollectionProvider：集合源取自 Props.CollectionSource
// （审计 ARCH-01）。手写组件（非 core.Atom 基座）必须显式声明，否则依赖登记看不见它 ——
// 表现是「用了 cardstack 集合模式的页面，新增内容后永不重建」。
func (c *Component) CollectionProp() string { return "collectionSource" }

// init 注册组件。
func init() {
	core.Register(&Component{})
	core.RegisterTemplate("cardstack", cardstackTemplate)
	// 每个增强块独立注册：命中任一特征只注入它自己 —— 合并注册会让「用了轮播」的页面白背灯箱代码。
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCardStacks"},
		Feats:  []string{"data-cardstack-drag"},
		Source: enhanceDragJS,
	})
	// 每个增强块独立注册：命中任一特征只注入它自己 —— 合并注册会让「用了轮播」的页面白背灯箱代码。
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCardDecks"},
		Feats:  []string{"data-cardstack-deck"},
		Source: enhanceDeckJS,
	})
	// 每个增强块独立注册：命中任一特征只注入它自己 —— 合并注册会让「用了轮播」的页面白背灯箱代码。
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initSlideStacks"},
		Feats:  []string{"data-cardstack-slide"},
		Source: enhanceSlideJS,
	})
}

// Type 实现组件接口。
func (c *Component) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema。
func (c *Component) PropsSpec() any { return &Props{} }

// Palette 实现 core.PaletteProvider：组件库呈现元数据（审计 REG-005）。
// 显示名 / 说明 / 分组 / 插入默认 Props 都在 Go 侧声明，前端只消费注入数据。
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
	if p.Shape == ShapeBook {
		return ShapeBook
	}
	return ShapeFan
}

// collectionSource 集合源（空 = 静态卡片模式）。
func collectionSource(p *Props) string { return strings.TrimSpace(p.CollectionSource) }

// 集合源 → 系统页面槽位与缺省文案（BIZ-2）。
//
// 槽位键在这里写的是**字面量**：builder 是底层包（依赖方向 module → builder），
// 不能反向 import page 模块拿常量。键名与 core.SiteSlot* / page enums 保持一致，
// 两边一致由测试钉住（core 侧已有「键集合必须完全相同」的同类测试）。
const (
	collectionSourceArticle = "content:article"
	collectionSourceProduct = "content:product"
	siteSlotBlog            = "blog"
	siteSlotShop            = "shop"

	defaultArticleMoreText = "更多文章"
	defaultProductMoreText = "全部商品"
)

// collectionListPageLink 集合列表的「列表页入口」链接（系统页面槽位）。
//
// 映射：文章集合 → blog 槽位（更多文章）、商品集合 → shop 槽位（全部商品）。
// 其他集合源没有对应的系统页面语义（分类列表页不是槽位），一律不输出链接 ——
// 宁可少一个入口，也不要一个猜出来的死链。
//
// 槽位没绑、或绑了但那页没发布时，ctx.SitePages 里根本没有这个键
// （page 侧只返回已绑且已发布的绑定），此时返回 has=false，模板整块不渲染。
func collectionListPageLink(source string, p *Props, ctx *core.RenderContext) (has bool, href, text string) {
	if ctx == nil {
		return false, "", ""
	}
	var slot, fallback string
	switch source {
	case collectionSourceArticle:
		slot, fallback = siteSlotBlog, defaultArticleMoreText
	case collectionSourceProduct:
		slot, fallback = siteSlotShop, defaultProductMoreText
	default:
		return false, "", ""
	}
	href = strings.TrimSpace(ctx.SitePage(slot))
	if href == "" {
		return false, "", ""
	}
	text = fallback
	if p != nil && strings.TrimSpace(p.ListPageLink) != "" {
		text = strings.TrimSpace(p.ListPageLink)
	}
	return true, href, text
}

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
// 两种触发方式自动关闭：全屏分页（卡片本来就占满一屏，再"放大"等于原地不动）、
// deck 把点击改成「翻下一页」（点了就翻页，永远展不开）。
func zoomEnabled(p *Props) bool {
	if effectiveTrigger(p) == TriggerSlide {
		return false
	}
	if effectiveTrigger(p) == TriggerDeck && p.DeckClick == deckClickNext {
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
// 拆分：几何与兜底值留在 Go（同 props 同字节、无随机、无运行时测量），
// 规则的存废、声明顺序与逐卡展开交给同目录的 cardstack.css（顺序即产物字节序）。
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

	// 值变量必须给全：样式源里每一种触发方式的分支都会被解析（未命中的输出进临时桶），
	// 它们引用的变量同样要做「引用即须存在」校验。
	vars := cardstackVars(p, trigger, content, width, height)
	cardStyleVars(p, vars)
	hoverCSSVars(p, n, height, vars)
	scrollCSSVars(p, vars)
	dragCSSVars(p, n, width, height, vars)
	deckCSSVars(p, n, width, height, vars)
	slideCSSVars(p, vars)

	// 逐卡列表全部按当前卡片数填满：未命中的触发方式分支输出进临时桶被丢弃，
	// 但它的 @each 会真实展开 —— 循环体里的变量因此同样要通过「引用即须存在」的校验，
	// 少填一份列表会让一批样式错误只在特定触发方式下才暴露。
	lists := map[string][]map[string]string{
		"hoverCards":  hoverCards(p, n, width),
		"scrollCards": scrollCards(node.ID, p, n),
		"dragCards":   dragCards(p, n, width, height),
		"deckCards":   deckCards(p, n, width, height),
		"slideCards":  slideCards(n),
	}

	// 动效关键帧由 Go 侧登记：词表白名单（loopEffectKey / slideEffectKeyframe）本来就在这里，
	// 且样式源的 @need-keyframes 只认字面名 -- 效果名是 props 算出来的，写进样式源登记不上。
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

// needKeyframe 登记内建关键帧（无效果时不登记）。
func needKeyframe(b *core.CSSBuckets, name string) {
	if name != "" {
		b.NeedKeyframes(name)
	}
}

// cardstackVars 触发方式 / 内容来源 / 各开关的值变量（Go 侧只翻译判定，不拼样式文本）。
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
		// 卡片 border box 高（含上下边框）由样式源用 calc 相加，这里只给 2 倍边框宽。
		"cardBorderX2": strconv.Itoa(2 * cardBorder),
	}
}

// cardStyleVars 卡片外观（背景/内边距/圆角/边框宽度/卡内布局）的兜底值。
//
// 两套：内容卡要读得下正文、数字卡是巨字号居中；props 给了值就一律以用户值为准。
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
	// 卡内布局：与容器组件的 flex 参数同源，卡片因此可以当容器用。
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

// hoverCSSVars 悬停展开的轨道高度与形态开关。
//
// 轨道高度按形态预留，保证展开后不压到下方内容：
//
//	fan        卡高 + 2x 上抬量（展开时卡片整体上抬）
//	line 横排  卡高（只横向铺开，纵向不越界）
//	line 竖排  卡高 + 2x 最大步距 x 位移（纵向整列铺开）
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

// hoverCards 悬停展开的逐卡几何。
//
// 形态差异只有一处 —— 变换顺序：
//   - fan               弧线：rotate() translate()，位移落在旋转后的坐标系，卡片沿弧线切向散开；
//   - line + horizontal 横排：纯 translate(x)，不带任何旋转，卡片平铺成一行；
//   - line + vertical   竖排：纯 translate(0, y)，不带任何旋转，卡片堆成一列。
//
// 收敛公式随之不同，三者都保证展开不撑出视口：
//
//	fan          |dx*cosT + L*sinT| + (W/2)cosT + (H/2)sinT <= 50vw - gutter
//	line 横排     |dx| + W/2 <= 50vw - gutter
//	line 竖排     |dy| + H/2 <= 50vh - gutter
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
			// 数字卡带位置派生色相（中间卡 0 偏移，两侧按 +/-step 渐变）。
			"hue": fmt.Sprintf("%.0f", offset*hueStep),
			// 书的几何：旋转轴放在书脊那一侧（左半取右缘、右半取左缘），
			// 合上时每页竖起 +/-86 度（略小于 90，留一线厚度）。
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
			// 摊开：转平（rotateY 0）并按序号向两外侧移，像把书页摊在桌上。
			// 0.58 倍卡宽是刻意留的重叠量 —— 完全按卡宽铺开会显得像并排卡片，不像书页。
			openX := offset * cssPx(width, fallbackCardW) * 0.58
			// 收敛与 fan/line 同一套：可用空间 = 视口半宽 - 留白 - 卡半宽，再除以最大步距 mid。
			allow := fmt.Sprintf("calc((50vw - %dpx - 50%%) / %s)", viewportGutter, fnum(mid))
			openXDecl := fmt.Sprintf("clamp(calc(-1 * %s), %spx, %s)", allow, num(openX), allow)
			fixed = "rotateY(0deg) translateX(" + openXDecl + ")"
			adaptive = fixed
		case fan:
			// 弧线：收敛 = 视口半宽 - 留白 - 旋转外扩 -（上抬量被旋转投影的那一份），
			// 再除以 cosT*最大步距（dx 要先经 cosT 才变成世界坐标的水平位移）。
			rad := math.Abs(offset*angle) * math.Pi / 180
			cosT, sinT := math.Cos(rad), math.Sin(rad)
			allow := fmt.Sprintf("calc((50vw - %dpx - %spx - %s * 50%% - %s * var(--sky-cardstack-h) / 2) / %s)",
				viewportGutter, fnum(cardLiftY*sinT), fnum(cosT), fnum(sinT), fnum(cosT*mid))
			fixed = fmt.Sprintf("rotate(%sdeg) translate(%spx, -%dpx)", num(offset*angle), num(offset*dist), cardLiftY)
			adaptive = fmt.Sprintf("rotate(%sdeg) translate(calc(%s * clamp(0px, %s, %dpx)), -%dpx)",
				num(offset*angle), num(offset), allow, int(dist), cardLiftY)
		case vertical:
			// 竖排：不带旋转、横向不动，收敛 = 视口半高 - 留白 - 卡半高。
			allow := fmt.Sprintf("calc((50vh - %dpx - var(--sky-cardstack-h) / 2) / %s)", viewportGutter, num(mid))
			fixed = fmt.Sprintf("translate(0px, %spx)", num(offset*dist))
			adaptive = fmt.Sprintf("translate(0px, calc(%s * clamp(0px, %s, %dpx)))",
				num(offset), allow, int(dist))
		default:
			// 横排：不带旋转、纵向不动（堆叠态本来就在容器中线），
			// 收敛 = 视口半宽 - 留白 - 卡半宽（50% 即 border box 半宽）。
			allow := fmt.Sprintf("calc((50vw - %dpx - 50%%) / %s)", viewportGutter, num(mid))
			fixed = fmt.Sprintf("translate(%spx, 0px)", num(offset*dist))
			adaptive = fmt.Sprintf("translate(calc(%s * clamp(0px, %s, %dpx)), 0px)",
				num(offset), allow, int(dist))
		}
		// fixed 是参数原值（宽屏形态），adaptive 带 clamp 收敛（窄屏不横向溢出）。
		item["fixed"] = fixed
		item["adaptive"] = adaptive
		cards = append(cards, item)
	}
	return cards
}

// scrollCSSVars 滚动堆叠的间距与粘住位置。
func scrollCSSVars(p *Props, vars map[string]string) {
	vars["spacing"] = effectiveSpacing(p)
	vars["stickyTop"] = effectiveStickyTop(p)
}

// scrollCards 滚动堆叠的逐卡几何：每张卡一份独立关键帧，终态是该卡的静态缩放。
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

// dragCSSVars 拖拽旋转的轨道几何与开关。
//
// 单侧可用空间 = (视口宽 - 卡片实际占位) / 2 - 留白。圆柱还要再扣掉透视放大：
// 卡片在 translateZ(R) 处被 perspective 放大 d/(d-R) 倍，只按卡宽预留会漏掉这一份。
func dragCSSVars(p *Props, n int, width, height string, vars map[string]string) {
	radius := dragRadius(p, n, width, height)
	cardH := cssPx(height, fallbackCardH)
	cylinder := p.DragMode == dragModeCylinder
	occupancy := width
	if cylinder && radius > 0 && radius < dragPerspective {
		occupancy = fmt.Sprintf("calc(%s * %.4f)", width, dragPerspective/(dragPerspective-radius))
	}
	// 轨道高度 = 圆周外接盒（2R + 卡高），与相邻区块不会重叠。
	vars["dragTrackMinHeight"] = fmt.Sprintf("min-height: %dpx", int(2*radius+cardH))
	vars["dragSideRoom"] = fmt.Sprintf("max(0px, calc((100vw - %s) / 2 - %dpx))", occupancy, dragGutter)
	vars["dragCylinder"] = core.BoolVar(cylinder)
	vars["dragPerspective"] = strconv.Itoa(int(dragPerspective))
	vars["deckArrows"] = core.BoolVar(p.DeckArrows)
}

// dragCards 拖拽旋转的逐卡几何：第 i 张卡落在半径 R 的圆周上（角度 360i/N）。
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
			// 环形没有「中间卡」，色相直接按序号均匀铺开。
			"hue": fmt.Sprintf("%.0f", float64(i)*hueStep),
		})
	}
	return cards
}

// slideEffectKeyframe 切换动画 → 通用动效词汇名（不新增关键帧，直接复用 core 那套）。
// 返回空串 = 无动画（缺省，只有位置变化）。
func slideEffectKeyframe(p *Props) string {
	horizontal := p.SlideDirection == slideDirectionHorizontal
	// 只写词汇名，关键帧名由 core 拼 —— 前缀只有一处定义（core.EffectKeyframeName），
	// 改命名空间时这里跟着走，不必逐个改字面量。
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

// loopEffectKey 循环效果 → 通用词汇名（只返回不占用 transform 的两条：
// glow 走 filter、flash 走 opacity；其余 loop 词汇都改 transform，会顶掉位移/缩放）。
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
	// 居中交给 grid，变换里不再带 translate(-50%, -50%)。
	return fmt.Sprintf("transform: translate%s(calc(var(--sky-deck-off, 0) * %d%%)) rotate(calc(var(--sky-deck-off, 0) * %ddeg)) scale(calc(1 - var(--sky-deck-abs, 0) * %s))",
		axis, offset, rot, fnum(scaleStep))
}

// dragCardTransform 拖拽卡片的变换。
//
//	ring     平面圆环绕中心排布，卡片始终正立（先转到角度、位移、再抵消旋转）；
//	cylinder 三维圆柱：卡片贴在外侧面朝外，靠透视产生环绕感与近大远小。
func dragCardTransform(angle float64, radius float64, cylinder bool) string {
	if cylinder {
		return fmt.Sprintf("transform: rotateY(calc(%sdeg + var(--sky-cardstack-rot, 0deg))) translateZ(min(%dpx, var(--sky-cardstack-side, 40vw)))",
			num(angle), int(radius))
	}
	return fmt.Sprintf("transform: rotate(calc(%sdeg + var(--sky-cardstack-rot, 0deg))) translateY(calc(-1 * min(%dpx, var(--sky-cardstack-side, 40vw)))) rotate(calc(-%sdeg - var(--sky-cardstack-rot, 0deg)))",
		num(angle), int(radius), num(angle))
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

// deckCSSVars 堆叠轮播的轨道高度、切换轴与过渡参数。
func deckCSSVars(p *Props, n int, width, height string, vars map[string]string) {
	rot := effectiveDeckRotate(p)
	vertical := effectiveDeckDirection(p) == "vertical"
	// 纵向切换且没给偏移时走纵向缺省档（偏移影响的是逐卡位移，见 deckCards）。
	if vertical && p.DeckOffset <= 0 {
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
	// touch-action 必须跟着切换轴走：切换方向的轴归脚本（否则触摸手势被浏览器
	// 拿去滚页面，滑动切换在触屏上完全失效），另一个轴让给页面滚动。
	touchAction := "pan-y" // 横向切换：纵向留给页面
	if vertical {
		touchAction = "pan-x" // 纵向切换：横向留给页面
	}
	vars["deckTouchAction"] = touchAction
	vars["deckTrackMinHeight"] = fmt.Sprintf("min-height: %dpx", int(trackH))
	vars["deckDurationMs"] = strconv.Itoa(effectiveDeckDuration(p))
	vars["deckEasing"] = deckEasing(p)
	kf := loopEffectKey(p.DeckHighlight)
	vars["deckHighlight"] = core.BoolVar(kf != "")
	vars["deckHighlightKey"] = kf
}

// deckCards 堆叠轮播的逐卡静态降级值（i - mid）：没有增强脚本时卡片按序号摊开成一摞。
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

// slideCSSVars 全屏分页的每屏高度与滚动轴参数。
//
// 每屏高度降级链：dvh 不认识时退回 vh（老浏览器仍是一屏一张，只是地址栏收放时略跳）。
// 降级链顺序是「旧值在前、新值在后」—— 后写的覆盖先写的；反过来写 dvh 会被 vh 永久盖掉。
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

// slideCards 全屏分页的逐卡序号（其余声明由每屏高度与滚动轴参数决定，与序号无关）。
func slideCards(n int) []map[string]string {
	cards := make([]map[string]string, 0, n)
	for i := 0; i < n; i++ {
		cards = append(cards, map[string]string{"nth": strconv.Itoa(i + 1)})
	}
	return cards
}

// cardstackTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed cardstack.jet
var cardstackTemplate string
