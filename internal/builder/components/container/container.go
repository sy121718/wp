// Package container 实现 core.container 标准容器组件（规范 docs/02-A §3）：
// 组件树的唯一结构载体，既可作为页面第一层顶级 Section，也可自由嵌套
// （深度上限 builder.MaxNodeDepth=10，由 ValidatePage 统一拦截；正常页面
// 3~6 层，防线针对 globalref 内联叠加与插件预设失控）；
// 编译期直出单层原生 HTML 语义标签，不产生冗余 Wrapper。
package container

import (
	_ "embed" // container.css 经 //go:embed 打进二进制
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.container"

// 布局引擎常量。
const (
	EngineFlex = "flex"
	EngineGrid = "grid"
)

var (
	// nodeIDRe 节点 ID 白名单：字母数字下划线连字符，1~64 位。
	nodeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	// cssValueRe CSS 值白名单：字母数字与常见安全符号，禁止引号/分号/花括号/@/反斜杠/尖括号等注入载体。
	cssValueRe = regexp.MustCompile(`^[A-Za-z0-9#%.,()\-+/:?=&_~ ]*$`)
)

// allowedContainerTags 容器允许的原生语义标签。
var allowedContainerTags = map[string]bool{
	"div": true, "section": true, "article": true, "aside": true,
	"nav": true, "header": true, "footer": true, "main": true,
}

// justifyMap 主轴对齐关键字到 CSS 值的映射。
// 同时接受 CSS 标准值（检查器提交）与旧简写（兼容历史文档）。
var justifyMap = map[string]string{
	// CSS 标准值直通。
	"flex-start": "flex-start", "center": "center", "flex-end": "flex-end",
	"space-between": "space-between", "space-around": "space-around", "space-evenly": "space-evenly",
	// 旧简写兼容。
	"start": "flex-start", "end": "flex-end",
	"between": "space-between", "around": "space-around", "evenly": "space-evenly",
}

// alignMap 交叉轴对齐关键字到 CSS 值的映射。
// 同时接受 CSS 标准值与旧简写（兼容历史文档）。
var alignMap = map[string]string{
	"stretch": "stretch", "center": "center", "baseline": "baseline",
	"flex-start": "flex-start", "flex-end": "flex-end",
	"start": "flex-start", "end": "flex-end",
}

// allowedFlexDirection Flex 主轴方向白名单。
var allowedFlexDirection = map[string]bool{
	"row": true, "row-reverse": true, "column": true, "column-reverse": true,
}

// allowedOverflow 溢出处理白名单。
var allowedOverflow = map[string]bool{
	"visible": true, "hidden": true, "scroll": true, "auto": true,
}

// allowedBorderStyle 边框线型白名单。
var allowedBorderStyle = map[string]bool{
	"solid": true, "dashed": true, "dotted": true, "double": true,
}

// shadowLevels 阴影级别到 CSS 值的映射。
var shadowLevels = map[string]string{
	"sm": "0 1px 3px rgba(0,0,0,0.12)",
	"md": "0 4px 12px rgba(0,0,0,0.12)",
	"lg": "0 10px 28px rgba(0,0,0,0.16)",
	"xl": "0 20px 48px rgba(0,0,0,0.2)",
}

// （悬浮反馈的阴影加深逻辑已随交互管线上收 core.CompileInteraction；

// Responsive 三端字符串值（如内边距、间距）。
type Responsive struct {
	Desktop string `json:"desktop,omitempty"`
	Tablet  string `json:"tablet,omitempty"`
	Mobile  string `json:"mobile,omitempty"`
}

// ResponsiveInt 三端整数值（如栅格列数）。
type ResponsiveInt struct {
	Desktop int `json:"desktop,omitempty"`
	Tablet  int `json:"tablet,omitempty"`
	Mobile  int `json:"mobile,omitempty"`
}

// Props 标准容器能力描述（规范 docs/02-A §3 + docs/03-A 面板能力）。
type Props struct {
	// Tag 原生语义标签：div/section/article/aside/nav/header/footer/main。
	Tag    string      `json:"tag"`
	Layout LayoutProps `json:"layout"`
	// Box 盒模型（内距/外距/尺寸/溢出）；ct:"group" 展开到「布局」区块。
	Box BoxProps `json:"box" ct:"group"`
	// Visual 外观（边框/圆角/阴影/背景）；ct:"group" 让检查器按 visual.* 路径展开渲染。
	Visual      VisualProps           `json:"visual" ct:"group"`
	Interaction core.InteractionProps `json:"interaction"`
	// Position 定位系统（03-A §3.1 Tab1）：static/relative/absolute/sticky/drawer。
	Position PositionProps `json:"position,omitempty" ct:"group"`
	// StyleEx 样式扩展（03-A §3.1 Tab2）：背景双态/遮罩/形状分隔线/顺序/组父联动/属性。
	StyleEx StyleExProps `json:"styleEx,omitempty"`
}

// PositionProps 定位系统（ct:"group" 展开到面板「布局」区块）。
type PositionProps struct {
	// Type 定位类型：static（默认）/ relative / absolute / sticky / drawer。
	Type string `json:"type,omitempty" ct:"select,static=默认,relative=相对,absolute=绝对,sticky=粘性,drawer=抽屉,sec=layout,label=定位方式"`
	// Top/Right/Bottom/Left absolute 精准坐标（CSS 长度值）。
	Top    string `json:"top,omitempty" ct:"dimension,maxlen=20,sec=layout,label=上偏移"`
	Right  string `json:"right,omitempty" ct:"dimension,maxlen=20,sec=layout,label=右偏移"`
	Bottom string `json:"bottom,omitempty" ct:"dimension,maxlen=20,sec=layout,label=下偏移"`
	Left   string `json:"left,omitempty" ct:"dimension,maxlen=20,sec=layout,label=左偏移"`
	// DrawerSide drawer 抽屉滑出边：left / right / bottom。
	DrawerSide string `json:"drawerSide,omitempty" ct:"select,left=左侧,right=右侧,bottom=底部,sec=layout,label=抽屉方向"`
	// DrawerOverlay 抽屉遮罩（:target 显隐，零 JS）。
	DrawerOverlay bool `json:"drawerOverlay,omitempty" ct:"bool,sec=layout,label=抽屉遮罩"`
	// DrawerTriggerID 唯一触发元素 ID（配合 button 等触发协议）。
	DrawerTriggerID string `json:"drawerTriggerId,omitempty" ct:"safe,maxlen=64,sec=layout,label=抽屉触发 ID"`
}

// StyleExProps 样式扩展。
type StyleExProps struct {
	// BackgroundHover 悬停背景（纯色/渐变；继承 Background 的缺省语义）。
	BackgroundHover string `json:"backgroundHover,omitempty"`
	// Overlay 背景覆盖层（纯色/渐变半透明遮罩，保障文本可读）。
	Overlay string `json:"overlay,omitempty"`
	// ShapeDivider 形状分隔线：wave / slope / curve；空=关闭。
	ShapeDivider string `json:"shapeDivider,omitempty"`
	// ShapeDividerPosition 形状位置：top / bottom（默认 bottom）。
	ShapeDividerPosition string `json:"shapeDividerPosition,omitempty"`
	// SafeAreaBottom 底部安全区垫高（H5：padding-bottom 接 env(safe-area-inset-bottom)，
	// 适配 iPhone 底部横条；开启后覆盖用户 padding-bottom。视觉层 viewport 适配分类）。
	SafeAreaBottom bool `json:"safeAreaBottom,omitempty" ct:"bool,sec=background,label=底部安全区垫高"`
	// Reveal 滚动显现覆盖（H5「滚动过去才出内容」子树开关）："" 跟随页面/主题、
	// on 子树强制滚动显现、off 子树豁免直接显示（视觉层分层开关）。
	Reveal string `json:"reveal,omitempty" ct:"select,=跟随页面,on=子树滚动显现,off=子树直接显示,sec=background,label=滚动显现"`
	// CardLayout 内部卡片布局语义开关（H5 结构变体）："" 默认（跟随容器宽度自适应）/
	// horizontal 强制横排。声明到本容器上，内部卡片经 @container style() 响应。
	CardLayout string `json:"cardLayout,omitempty" ct:"select,=默认,horizontal=卡片横排,sec=layout,label=内部卡片布局"`
	// ContainerQuery 容器查询上下文（H5 组件级响应式）：输出 container-type: inline-size，
	// 内部组件可用 @container 规则按「容器宽度」自适应（而非视口宽度），
	// 适合把组件放进侧栏/窄区块的场景。未开启时内部组件始终用视口断点。
	ContainerQuery bool `json:"containerQuery,omitempty" ct:"bool,sec=layout,label=容器查询上下文"`
	// ContentVisibility 视口外跳过渲染（H5 长页面滚动性能；content-visibility: auto +
	// contain-intrinsic-size 占位防滚动条跳动）。注意与滚动显现动效可能相互影响，按需开启。
	ContentVisibility bool `json:"contentVisibility,omitempty" ct:"bool,sec=background,label=视口外跳过渲染"`
	// Order 子项顺序（flex/grid 中 -1~99）。
	Order int `json:"order,omitempty"`
	// GroupParent 父子悬停联动：父容器 hover 时子组件可触发联动样式。
	GroupParent bool `json:"groupParent,omitempty"`
	// Attributes 自定义 HTML 键值对（白名单 key + 安全 value）。
	Attributes []AttributeKV `json:"attributes,omitempty"`
}

// AttributeKV 自定义属性键值对。
type AttributeKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LayoutProps 双排版引擎（Flexbox / Grid）。
type LayoutProps struct {
	// Engine 排版引擎：flex / grid。
	Engine string     `json:"engine"`
	Flex   *FlexProps `json:"flex,omitempty"`
	Grid   *GridProps `json:"grid,omitempty"`
}

// FlexProps Flexbox 排版参数。
type FlexProps struct {
	// Direction 主轴方向：row / row-reverse / column / column-reverse。
	Direction string `json:"direction,omitempty"`
	// Justify 主轴对齐：CSS 标准值 flex-start / center / flex-end /
	// space-between / space-around / space-evenly（旧简写 start/end/between/around/evenly 兼容）。
	Justify string `json:"justify,omitempty"`
	// Align 交叉轴对齐：CSS 标准值 stretch / center / baseline /
	// flex-start / flex-end（旧简写 start/end 兼容）。
	Align string `json:"align,omitempty"`
	// Wrap 是否允许自动换行。
	Wrap bool `json:"wrap,omitempty"`
	// Gap 子元素间距（CSS 长度值）。
	Gap string `json:"gap,omitempty"`
}

// GridProps Grid 栅格参数。
type GridProps struct {
	// Columns 栅格列数（1~12），支持三端响应式降级。
	Columns ResponsiveInt `json:"columns,omitempty"`
	// ColumnGap 列间距。
	ColumnGap string `json:"columnGap,omitempty"`
	// RowGap 行间距。
	RowGap string `json:"rowGap,omitempty"`
}

// BoxProps 盒模型尺寸，三端独立（ct:"group" 展开到面板「布局」区块）。
type BoxProps struct {
	// Padding 内边距（CSS 简写值），三端独立；检查器按「一行四向 + 联动」编辑（boxspacing）。
	Padding Responsive `json:"padding,omitempty" ct:"boxspacing,sec=layout,label=内距"`
	// Margin 外边距（CSS 简写值，含 auto 居中），三端独立；同上四向编辑。
	Margin Responsive `json:"margin,omitempty" ct:"boxspacing,sec=layout,label=外距"`
	// MinHeight 最小高度。
	MinHeight string `json:"minHeight,omitempty" ct:"dimension,maxlen=20,sec=layout,label=最小高度"`
	// MaxHeight 最大高度。
	MaxHeight string `json:"maxHeight,omitempty" ct:"dimension,maxlen=20,sec=layout,label=最大高度"`
	// MaxWidth 最大宽度（**版心约束**）。
	//
	// 为什么必须有它：容器此前只有「内距 / 外距 / 最小高度 / 最大高度」，
	// **没有任何横向约束** —— 正文、卡片、图文区块一律铺满视口宽度。
	// 在 1600px 的视口下，一篇文章每行 100+ 字符，读起来就是一整块灰墙；
	// 作者没有任何办法把内容收进一个适合阅读的版心。
	//
	// 传 CSS 长度即可（常用 "820px" 这类定值）。窄屏不要另配：CSS 的
	// max-width 本来就不会超过可用宽度，写 min(100%, 820px) 也可以。
	MaxWidth string `json:"maxWidth,omitempty" ct:"dimension,maxlen=30,sec=layout,label=最大宽度"`
	// Center 水平居中（配了 MaxWidth 才有效）。
	//
	// 单列出来而不是「设了 MaxWidth 就自动居中」：容器常常是**全宽背景 + 内层版心**
	// 的写法，那时外层不该居中；把居中做成显式开关，两种用法都表达得出来。
	Center bool `json:"center,omitempty" ct:"bool,sec=layout,label=水平居中"`
	// Overflow 内容溢出处理：visible / hidden / scroll / auto。
	Overflow string `json:"overflow,omitempty" ct:"select,visible=可见,hidden=隐藏,scroll=滚动,auto=自动,sec=layout,label=溢出处理"`
}

// VisualProps 视觉装饰。
type VisualProps struct {
	BgColor    string `json:"bgColor,omitempty" ct:"color,maxlen=200,sec=background,label=背景色"`
	BgGradient string `json:"bgGradient,omitempty" ct:"safe,maxlen=200,sec=background,label=背景渐变"` // 如 "linear-gradient(to right, #fff, #000)"
	// Pattern 图案背景（纯 CSS 平铺，20 种；与渐变/背景图互斥——三者同写 background-image）。
	Pattern string `json:"pattern,omitempty" ct:"select,=无,dots=点阵,grid=网格,overlay=细网格,diagonal-stripes=斜纹,diagonal-lines=细斜线,vertical-stripes=竖纹,horizontal-stripes=横纹,zigzag=锯齿,checkerboard=棋盘,triangles=三角,diamond=菱形,crosses=十字,plus=加号,squares=方块,circles=大圆点,polka=交错波点,ripple=同心波纹,bricks=砖块,rain=雨丝,honeycomb=蜂窝,sec=background,label=图案背景"`
	// PatternColor 图案颜色（空 = 8% 黑）。
	PatternColor string `json:"patternColor,omitempty" ct:"color,maxlen=200,sec=background,label=图案颜色"`
	// BgGradientAnimated 背景渐变流动（配合 BgGradient；200% 拉伸 + sky-bg-flow 位移动画）。
	BgGradientAnimated bool   `json:"bgGradientAnimated,omitempty" ct:"bool,sec=background,label=渐变流动"`
	BgImage            string `json:"bgImage,omitempty" ct:"media,sec=background,label=背景图片"` // 背景图 URL（媒体库选择回填；画布/产物直出）
	// BgSlides 背景轮播图（多张，纯 CSS 交叉淡入；填写后优先于单张背景图）。
	BgSlides []string `json:"bgSlides,omitempty" ct:"mediaList,sec=background,label=背景轮播图"`
	// BgSlideInterval 轮播切换间隔（秒，默认 6）。
	BgSlideInterval string `json:"bgSlideInterval,omitempty" ct:"number,min=1,max=60,sec=background,label=轮播间隔(s)"`
	// BgPosition 背景定位（default=浏览器默认）：center / left top 等关键词组合；custom 时取 BgPositionXY。
	BgPosition string `json:"bgPosition,omitempty" ct:"select,default=（默认）,custom=自定义,center=居中,center top=中上,center bottom=中下,left top=左上,left center=左中,left bottom=左下,right top=右上,right center=右中,right bottom=右下,default=default,sec=background,label=背景定位"`
	// BgPositionXY 自定义定位值（BgPosition=custom 时生效），如 "50% 20%"。
	BgPositionXY string `json:"bgPositionXY,omitempty" ct:"safe,maxlen=40,sec=background,label=自定义定位值"`
	// BgAttachment 背景附着方式：default / scroll / fixed / local。
	BgAttachment string `json:"bgAttachment,omitempty" ct:"select,default=（默认）,scroll=随页面滚动,fixed=固定（视差）,local=随内容滚动,default=default,sec=background,label=背景附着方式"`
	// BgRepeat 背景重复：default / no-repeat / repeat / repeat-x / repeat-y。
	BgRepeat string `json:"bgRepeat,omitempty" ct:"select,default=（默认）,no-repeat=不重复,repeat=平铺,repeat-x=横向平铺,repeat-y=纵向平铺,default=default,sec=background,label=背景重复"`
	// BgSize 显示尺寸：default / auto / contain / cover / custom（取 BgSizeValue）。
	BgSize string `json:"bgSize,omitempty" ct:"select,default=（默认）,auto=原始,contain=完整包含,cover=铺满裁剪,custom=自定义,default=default,sec=background,label=显示尺寸"`
	// BgSizeValue 自定义尺寸值（BgSize=custom 时生效），如 "100% auto"。
	BgSizeValue string `json:"bgSizeValue,omitempty" ct:"safe,maxlen=40,sec=background,label=自定义尺寸值"`
	// --- 边框与圆角（通用外观字段，面板「边框」标签） ---
	// 边框三要素：宽度留空即无边框；只填部分时缺省值兜底（1px solid currentColor）。
	BorderWidth string `json:"borderWidth,omitempty" ct:"dimension,maxlen=20,sec=border,label=边框宽度"`
	BorderStyle string `json:"borderStyle,omitempty" ct:"select,solid=实线,dashed=虚线,dotted=点线,double=双线,sec=border,label=边框样式"`
	BorderColor string `json:"borderColor,omitempty" ct:"color,maxlen=200,sec=border,label=边框颜色"`
	// Radius 圆角（四角统一值；四角字段任一填写时以四角为准）。
	Radius string `json:"radius,omitempty" ct:"dimension,maxlen=20,sec=border,label=圆角"`
	// RadiusTL/TR/BR/BL 四角圆角（空 = 用 Radius）。
	RadiusTL string `json:"radiusTL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左上圆角"`
	RadiusTR string `json:"radiusTR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右上圆角"`
	RadiusBR string `json:"radiusBR,omitempty" ct:"dimension,maxlen=20,sec=border,label=右下圆角"`
	RadiusBL string `json:"radiusBL,omitempty" ct:"dimension,maxlen=20,sec=border,label=左下圆角"`
	// Shadow 阴影级别：sm / md / lg / xl；custom 时取 ShadowCustom。
	Shadow string `json:"shadow,omitempty" ct:"select,sm=小,md=中,lg=大,xl=特大,custom=自定义,sec=border,label=阴影"`
	// ShadowCustom 自定义阴影四参 + 颜色（Shadow=custom 时生效）。
	ShadowX      string `json:"shadowX,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影 X"`
	ShadowY      string `json:"shadowY,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影 Y"`
	ShadowBlur   string `json:"shadowBlur,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影模糊"`
	ShadowSpread string `json:"shadowSpread,omitempty" ct:"dimension,maxlen=20,sec=border,label=阴影扩散"`
	ShadowColor  string `json:"shadowColor,omitempty" ct:"color,maxlen=200,sec=border,label=阴影颜色"`
}

// Container core.container 组件实现（交互属性类型已统一为 core.InteractionProps，
// 别名过渡层已删除，docs/06 §6 同源管线）。
type Container struct{}

// Type 实现组件接口。
func (Container) Type() string { return Type }

// PropsSpec 实现 SpecProvider：暴露 Props 生成检查器 schema（声明式控件）。
func (Container) PropsSpec() any { return &Props{} }

// Palette 实现 core.PaletteProvider：组件库呈现元数据（审计 REG-005）。
// 显示名 / 说明 / 分组 / 插入默认 Props 都在 Go 侧声明，前端只消费注入数据。
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

// IsSafeCSSValue 校验 CSS 值是否在安全白名单内（长度上限 500）。导出供其他组件复用。
func IsSafeCSSValue(v string) bool {
	return len(v) <= 500 && cssValueRe.MatchString(v)
}

// shapeDividers 形状分隔线白名单（03-A §3.1 Tab2，纯 SVG 装饰）。
// path 统一取自 core 通用素材库（core/shapes.go，viewBox 1440×120 参数化生成，
// 单一定义），词汇与素材库对齐（原 slant 已更名 slope，开发期无存量站点，
// 含旧词汇的测试数据直接重存）；<path fill> 包装在此完成，
// <svg> 骨架（viewBox 1440×120）由 container.jet 模板渲染（去 Go 拼字符串）。
var shapeDividers = map[string]string{
	"wave":  shapeDividerMarkup(core.ShapeKindWave),
	"slope": shapeDividerMarkup(core.ShapeKindSlope),
	"curve": shapeDividerMarkup(core.ShapeKindCurve),
}

// shapeDividerMarkup 素材库 path → 装饰 <path> 片段
// （fill=currentColor 跟随容器背景反色，语义不变）。
func shapeDividerMarkup(kind string) string {
	return `<path fill="currentColor" d="` + core.ShapePath(kind, 0, false) + `"/>`
}

// attrKeyRe 自定义属性 key 白名单（data-* / aria-* / 常见属性）。
var attrKeyRe = regexp.MustCompile(`^(data-[a-z0-9-]{1,32}|aria-[a-z0-9-]{1,32}|role|title|tabindex)$`)

// attrValueSafe 属性值白名单：允许中文与常见符号，禁引号/尖括号/花括号/@（防属性逃逸）。
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

// Validate 校验容器节点及整棵子树。
func (Container) Validate(node *core.Node, ids map[string]bool) (err error) {
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
	for i, child := range node.Children {
		if err = core.ValidateNode(child, ids); err != nil {
			return fmt.Errorf("节点 %s 子节点 %d: %w", node.ID, i, err)
		}
	}
	return nil
}

// containerCSS 组件样式源：与组件同目录，改样式不必再进 Go 字符串数组。
//
//go:embed container.css
var containerCSS string

// cssInteractionPoint 样式源的分段标记：两段样式源之间插入 core.CompileInteraction
// （按 Props 动效词汇表产出多条规则的 Go 计算，样式源表达不了）。交互规则在主规则之后、
// 定位系统之前落桶，顺序即产物字节，所以在这里切开而不是整段后置。
const cssInteractionPoint = "/* @interaction-point */"

// compileCSS 编译容器样式到三端 bucket。
//
// Go 侧只保留业务判定与兜底值计算（哪些分支生效、缺省值取什么），属性组合与规则顺序
// 全部在 container.css 里；解析失败属于构建期缺陷，直接 panic（静默跳过 = 样式悄悄少了）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	head, tail, ok := strings.Cut(containerCSS, cssInteractionPoint)
	if !ok {
		panic("container.css 缺少分段标记 " + cssInteractionPoint)
	}
	if err := core.ApplyComponentCSSTmplLists(b, sel, head, cssVarsHead(p), cssLists(p)); err != nil {
		panic(fmt.Sprintf("container 组件样式解析失败: %v", err))
	}
	// 交互状态与动画：与全部 Atom 组件共用同一动效管线（docs/06 §6 同源：
	// 弹簧缓动、滚动触发、循环词汇、触屏治理 hover 一次性获得）。
	core.CompileInteraction(sel, p.Interaction, b)
	if err := core.ApplyComponentCSSTmpl(b, sel, tail, cssVarsTail(p)); err != nil {
		panic(fmt.Sprintf("container 组件样式解析失败: %v", err))
	}
}

// cssVarsHead 样式源前半段（背景轮播 + 主规则 + 三端媒体查询）的变量表。
// 空值即「该属性没设」，声明由样式源自行省略 —— 迁移前 Go 里的逐个 if 由它吸收。
func cssVarsHead(p *Props) map[string]string {
	vars := map[string]string{
		// 布局引擎（两分支互斥，其余值不产出 display）。
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
		// 盒模型（三端独立）。
		"padDesktop":    p.Box.Padding.Desktop,
		"padTablet":     p.Box.Padding.Tablet,
		"padMobile":     p.Box.Padding.Mobile,
		"marginDesktop": p.Box.Margin.Desktop,
		"marginTablet":  p.Box.Margin.Tablet,
		"marginMobile":  p.Box.Margin.Mobile,
		"minHeight":     p.Box.MinHeight,
		"maxHeight":     p.Box.MaxHeight,
		"maxWidth":      p.Box.MaxWidth,
		"center":        core.BoolVar(p.Box.Center && p.Box.MaxWidth != ""),
		"overflow":      p.Box.Overflow,
		// 视觉装饰。
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
		// 轮播关键帧的百分比：没有轮播时为空。它们只出现在 @each 块内的帧体里，
		// 而空列表的 @each 也会被试解析一遍（标记变量已消费），所以必须无条件提供。
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
	// Flex 参数（为 nil 时只剩 display: flex）。
	if f := p.Layout.Flex; f != nil {
		vars["flexDirection"] = f.Direction
		vars["flexJustify"] = justifyMap[f.Justify]
		vars["flexAlign"] = alignMap[f.Align]
		if f.Wrap {
			vars["flexWrap"] = "wrap"
		}
		vars["flexGap"] = f.Gap
	}
	// Grid 参数（列数 0 = 该端不降级，声明整条省略）。
	if g := p.Layout.Grid; g != nil {
		vars["gridColsDesktop"] = repeatCount(g.Columns.Desktop)
		vars["gridColsTablet"] = repeatCount(g.Columns.Tablet)
		vars["gridColsMobile"] = repeatCount(g.Columns.Mobile)
		vars["gridColumnGap"] = g.ColumnGap
		vars["gridRowGap"] = g.RowGap
	}

	// 背景三分支互斥：渐变 > 背景图 > 图案（三者同写 background-image）。
	hasGradient := p.Visual.BgGradient != ""
	hasImage := !hasGradient && p.Visual.BgImage != ""
	hasPattern := !hasGradient && p.Visual.BgImage == "" && p.Visual.Pattern != ""
	vars["hasGradient"] = core.BoolVar(hasGradient)
	vars["gradientAnimated"] = core.BoolVar(p.Visual.BgGradientAnimated)
	if hasGradient && p.Visual.BgGradientAnimated {
		// 渐变流动：声明组来自效果基本库（一份实现）。
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

	// 背景轮播：张数决定关键帧名与总时长，逐张延迟走列表变量。
	if n := len(p.Visual.BgSlides); n > 0 {
		interval := slideInterval(p.Visual.BgSlideInterval)
		seg := 100 / n
		vars["hasSlides"] = core.BoolVar(true)
		vars["slideCount"] = strconv.Itoa(n)
		vars["slideTotal"] = fmt.Sprintf("%ds", n*interval)
		vars["fadeHold"] = strconv.Itoa(seg - 4)
		vars["fadeEnd"] = strconv.Itoa(seg)
	}

	// 边框：任一要素填写即生效，缺失项兜底（1px solid currentColor）。
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

	// 圆角：四角字段优先，其次统一值（缺角用统一值兜底，都为空则 0）。
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

	// 阴影：自定义四参 与 预设级别 互斥。
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

// cssLists 样式源前半段的列表变量（@each）：背景轮播的逐张延迟。
// 列表为空也要提供 —— 「@each 引用了这个列表」本身要参与反向校验。
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

// cssVarsTail 样式源后半段（定位系统与样式扩展）的变量表。
func cssVarsTail(p *Props) map[string]string {
	order := ""
	if p.StyleEx.Order != 0 {
		order = strconv.Itoa(p.StyleEx.Order)
	}
	safeArea := ""
	if p.StyleEx.SafeAreaBottom {
		safeArea = strings.Join(core.SafeAreaDecls("bottom"), "; ")
	}
	// 形状分隔线位置：默认 bottom；它同时进选择器与属性名，变量不能为空。
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

// repeatCount 栅格列数的变量形态：未设置（<=0）给空串，声明整条省略。
func repeatCount(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// slideInterval 轮播间隔（秒）：空 / 非法 / 越界（1~60）一律回退 6。
func slideInterval(raw string) int {
	v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(raw), "s"))
	if err != nil || v <= 0 || v > 60 {
		return 6
	}
	return v
}

// bgPositionValue 背景定位值：default / 空不产出；custom 取自定义值（空则不产出）。
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

// bgKeywordValue 背景附着 / 重复的关键词值：default 与空都不产出。
func bgKeywordValue(v string) string {
	if v == "default" {
		return ""
	}
	return v
}

// bgSizeValue 背景尺寸值：default / 空不产出；custom 取自定义值（空则不产出）。
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

// shapeColor 形状分隔线颜色（跟随容器背景反色缺省：当前色）。
func shapeColor(p *Props) string {
	if p.Visual.BgColor != "" {
		return p.Visual.BgColor
	}
	return "currentColor"
}

// validateProps 校验容器能力参数。
func validateProps(p *Props) (err error) {
	if !allowedContainerTags[p.Tag] {
		return fmt.Errorf("无效的语义标签: %q", p.Tag)
	}

	// 布局引擎。
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

	// 盒模型。
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

	// 视觉装饰。
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
	// 边框三要素不再强制同时提供：缺失项由编译端兜底（1px / solid / currentColor）。
	if p.Visual.BorderStyle != "" && !allowedBorderStyle[p.Visual.BorderStyle] {
		return fmt.Errorf("无效的边框线型: %q", p.Visual.BorderStyle)
	}
	if p.Visual.Shadow != "" && p.Visual.Shadow != "custom" {
		if _, ok := shadowLevels[p.Visual.Shadow]; !ok {
			return fmt.Errorf("无效的阴影级别: %q", p.Visual.Shadow)
		}
	}

	// 交互状态与动画：统一走 core 校验（入场/循环/悬浮/滚动叙事/吸顶全词汇 +
	// 互斥规则），与 Atom 组件共用同一词汇表，消除容器窄白名单的能力割裂。
	// 背景来源互斥：图案与渐变/背景图同写 background-image，同时配置会静默忽略其一。
	if p.Visual.Pattern != "" && (p.Visual.BgGradient != "" || p.Visual.BgImage != "") {
		return fmt.Errorf("图案背景与渐变/背景图互斥，请只选一种")
	}
	if err := core.ValidateInteraction(p.Interaction); err != nil {
		return err
	}
	// 视口外跳过渲染与滚动吸顶互斥：content-visibility 创建 containment，
	// 吸顶元素在视口外被跳过渲染时定位不可靠。
	if p.StyleEx.ContentVisibility && p.Interaction.Sticky {
		return fmt.Errorf("视口外跳过渲染与滚动吸顶不可同时开启")
	}

	// 定位系统（03-A）。
	switch p.Position.Type {
	case "", "static":
	case "relative":
	case "absolute":
		// 绝对定位至少一个坐标。
		if p.Position.Top == "" && p.Position.Right == "" && p.Position.Bottom == "" && p.Position.Left == "" {
			return fmt.Errorf("绝对定位必须提供至少一个坐标（top/right/bottom/left）")
		}
	case "sticky":
		// 与 Interaction.Sticky 兼容并存。
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

	// 样式扩展（03-A）。
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

// init 注册容器组件到编译内核。
func init() {
	core.Register(Container{})
	core.RegisterTemplate("container", containerTemplate)
}

// containerTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed container.jet
var containerTemplate string
