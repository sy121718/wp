package core

import (
	"fmt"
	"strings"
)

// 响应式断点标识。
const (
	BreakpointDesktop = "desktop" // 默认样式，无媒体查询
	BreakpointTablet  = "tablet"  // @media (max-width: 1024px)
	BreakpointMobile  = "mobile"  // @media (max-width: 767px)
)

// breakpointMedia 断点媒体查询（desktop-first）。
var breakpointMedia = map[string]string{
	BreakpointTablet: "@media (max-width: 1024px)",
	BreakpointMobile: "@media (max-width: 767px)",
}

// Keyframe 动效关键帧条目（Name 经 NeedKeyframes 激活，CSS 为 @keyframes 规则体）。
type Keyframe struct {
	Name string
	CSS  string
}

// keyframesBuiltin 内置动效关键帧（入场 → 循环 → 用法 FX → 滚动叙事；
// 切片顺序即输出顺序，确定性内建于数据结构，无独立顺序表）。
var keyframesBuiltin = []Keyframe{
	// 入场（entrance）
	{Name: "wp-fade-in", CSS: "@keyframes wp-fade-in {\n  from { opacity: 0 }\n  to { opacity: 1 }\n}"},
	{Name: "wp-fade-up", CSS: "@keyframes wp-fade-up {\n  from { opacity: 0; transform: translateY(16px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-fade-down", CSS: "@keyframes wp-fade-down {\n  from { opacity: 0; transform: translateY(-16px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-fade-left", CSS: "@keyframes wp-fade-left {\n  from { opacity: 0; transform: translateX(16px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-fade-right", CSS: "@keyframes wp-fade-right {\n  from { opacity: 0; transform: translateX(-16px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-zoom-in", CSS: "@keyframes wp-zoom-in {\n  from { opacity: 0; transform: scale(.92) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-zoom-out", CSS: "@keyframes wp-zoom-out {\n  from { opacity: 0; transform: scale(1.08) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-slide-up", CSS: "@keyframes wp-slide-up {\n  from { opacity: 0; transform: translateY(24px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-slide-down", CSS: "@keyframes wp-slide-down {\n  from { opacity: 0; transform: translateY(-24px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-slide-left", CSS: "@keyframes wp-slide-left {\n  from { opacity: 0; transform: translateX(24px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-slide-right", CSS: "@keyframes wp-slide-right {\n  from { opacity: 0; transform: translateX(-24px) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-flip-x", CSS: "@keyframes wp-flip-x {\n  from { opacity: 0; transform: perspective(600px) rotateX(-12deg) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-flip-y", CSS: "@keyframes wp-flip-y {\n  from { opacity: 0; transform: perspective(600px) rotateY(-12deg) }\n  to { opacity: 1; transform: none }\n}"},
	{Name: "wp-blur-in", CSS: "@keyframes wp-blur-in {\n  from { opacity: 0; filter: blur(8px) }\n  to { opacity: 1; filter: none }\n}"},
	{Name: "wp-bounce-in", CSS: "@keyframes wp-bounce-in {\n  0% { opacity: 0; transform: scale(.8) }\n  60% { opacity: 1; transform: scale(1.04) }\n  100% { opacity: 1; transform: none }\n}"},
	{Name: "wp-rotate-in", CSS: "@keyframes wp-rotate-in {\n  from { opacity: 0; transform: rotate(-6deg) scale(.96) }\n  to { opacity: 1; transform: none }\n}"},
	// 循环（attention）。shake/jello/heartbeat 效果参考 Animate.css（MIT）/CSShake，
	// 按循环动效场景重写的简化帧（非逐字复制）。
	{Name: "wp-loop-pulse", CSS: "@keyframes wp-loop-pulse {\n  0%, 100% { transform: scale(1) }\n  50% { transform: scale(1.03) }\n}"},
	{Name: "wp-loop-float", CSS: "@keyframes wp-loop-float {\n  0%, 100% { transform: translateY(0) }\n  50% { transform: translateY(-8px) }\n}"},
	{Name: "wp-loop-drift", CSS: "@keyframes wp-loop-drift {\n  0%, 100% { transform: translateX(0) }\n  50% { transform: translateX(-8px) }\n}"},
	{Name: "wp-loop-shake", CSS: "@keyframes wp-loop-shake {\n  0%, 100% { transform: translateX(0) }\n  25% { transform: translateX(-4px) }\n  75% { transform: translateX(4px) }\n}"},
	{Name: "wp-loop-jello", CSS: "@keyframes wp-loop-jello {\n  0%, 100% { transform: skewX(0) }\n  25% { transform: skewX(-6deg) }\n  75% { transform: skewX(6deg) }\n}"},
	{Name: "wp-loop-heartbeat", CSS: "@keyframes wp-loop-heartbeat {\n  0%, 100% { transform: scale(1) }\n  14% { transform: scale(1.08) }\n  28% { transform: scale(1) }\n  42% { transform: scale(1.08) }\n  70% { transform: scale(1) }\n}"},
	{Name: "wp-loop-blob", CSS: "@keyframes wp-loop-blob {\n  0%, 100% { border-radius: 60% 40% 30% 70% / 60% 30% 70% 40%; transform: rotate(0) }\n  50% { border-radius: 30% 60% 70% 40% / 50% 60% 30% 60%; transform: rotate(8deg) }\n}"},
	// glow 用 filter: drop-shadow（合成器友好；box-shadow 动画在移动端逐帧重绘，
	// 循环场景持续掉帧耗电）。drop-shadow 沿元素轮廓发光，圆角/透明底更自然。
	{Name: "wp-loop-glow", CSS: "@keyframes wp-loop-glow {\n  0%, 100% { filter: drop-shadow(0 0 0 rgba(59,130,246,0)) }\n  50% { filter: drop-shadow(0 0 8px rgba(59,130,246,.45)) }\n}"},
	{Name: "wp-loop-spin", CSS: "@keyframes wp-loop-spin {\n  from { transform: rotate(0deg) }\n  to { transform: rotate(360deg) }\n}"},
	{Name: "wp-bg-flow", CSS: "@keyframes wp-bg-flow {\n  0% { background-position: 0% 50% }\n  50% { background-position: 100% 50% }\n  100% { background-position: 0% 50% }\n}"},
	{Name: "wp-border-flow", CSS: "@keyframes wp-border-flow {\n  to { --wp-flow-angle: 360deg }\n}"},
	// 滚动叙事（scroll-driven，view() 进度连续绑定；Apple 产品页式叙事的原子变换）。
	{Name: "wp-story-zoom", CSS: "@keyframes wp-story-zoom {\n  from { transform: scale(.78) }\n  to { transform: scale(1.08) }\n}"},
	{Name: "wp-story-rise", CSS: "@keyframes wp-story-rise {\n  from { transform: translateY(60px) }\n  to { transform: translateY(0) }\n}"},
	{Name: "wp-story-fade", CSS: "@keyframes wp-story-fade {\n  from { opacity: .15 }\n  to { opacity: 1 }\n}"},
}

// keyframesCatalog 全部动效关键帧（内置组 + Animate 拆解组，组内序即输出序）。
var keyframesCatalog = joinKeyframes(keyframesBuiltin, keyframesAnimate)

// keyframeIndex 名称 → 规则体（输出查询用）；构建期派生，重名 fail-fast。
var keyframeIndex = buildKeyframeIndex(keyframesCatalog)

// joinKeyframes 拼接关键帧组（组间顺序即参数顺序）。
func joinKeyframes(groups ...[]Keyframe) []Keyframe {
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	out := make([]Keyframe, 0, total)
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// buildKeyframeIndex 构建名称索引（重名 fail-fast，对齐项目启动期防御文化）。
func buildKeyframeIndex(list []Keyframe) map[string]string {
	m := make(map[string]string, len(list))
	for _, k := range list {
		if _, dup := m[k.Name]; dup {
			panic("core: 重复动效词汇: " + k.Name)
		}
		m[k.Name] = k.CSS
	}
	return m
}

// CSSBuckets 三端 CSS 规则集合。
// 规则按文档序（前序遍历）追加，最终按 关键帧 → 桌面 → 平板 → 手机 的固定顺序拼接，
// 保证确定性输出（同一 Page Document 产生相同字节）。
// CSSDecl 构造单条 CSS 声明：property: v1 v2 v3。
// 跳过空值（空值不参与拼接，避免产生 "prop:  " 这类无效声明）。
// 例：CSSDecl("border-top", "1px", "solid", "red") → "border-top: 1px solid red"
func CSSDecl(prop string, values ...string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" {
			parts = append(parts, v)
		}
	}
	return prop + ": " + strings.Join(parts, " ")
}

type CSSBuckets struct {
	desktop []string
	tablet  []string
	mobile  []string
	hover   []string // 触屏治理：悬浮规则包 @media (hover: hover)（H5 sticky hover 治理）
	// 容器查询三桶：按「样式来源层级」分开，输出时分别包进 @layer
	// wp-auto（自动适配：@container 尺寸查询）< wp-theme（主题档位 style 查询）<
	// wp-local（容器/作者显式声明）——优先级由层序决定，不依赖源顺序。
	containersAuto  []string // 自动适配（尺寸查询）→ @layer wp-auto
	containersTheme []string // 主题档位（style 查询）→ @layer wp-theme
	containersLocal []string // 局部显式声明（style 查询）→ @layer wp-local
	// topLevel 注册类顶层规则（@property 等）：不进任何 @layer——这类规则的注册
	// 是全局的，放未分层最稳（避免浏览器对「层内注册」的实现差异）。
	topLevel  []string
	keyframes map[string]bool
	// seen 已输出的规则（断点+规则体），重复规则只保留首份：
	// 多张图共享同一骨架规则时不产生重复 CSS，确定性不受影响。
	seen map[string]bool
	// 组件自定义关键帧（如容器背景轮播）：按加入顺序输出，同名只输出一次。
	customKeyframes map[string]string
	customOrder     []string
}

// AddKeyframes 追加组件自定义关键帧（name 唯一，重复调用只保留首份，保证确定性）。
func (b *CSSBuckets) AddKeyframes(name, css string) {
	// 注册类顶层规则（@property 等）不进层：转存未分层桶（装配层置于层块之后）。
	if strings.HasPrefix(css, "@property") {
		for _, r := range b.topLevel {
			if r == css {
				return
			}
		}
		b.topLevel = append(b.topLevel, css)
		return
	}
	if b.customKeyframes == nil {
		b.customKeyframes = map[string]string{}
	}
	if _, ok := b.customKeyframes[name]; ok {
		return
	}
	b.customKeyframes[name] = css
	b.customOrder = append(b.customOrder, name)
}

// AddKeyframesDecls 以声明列表形式注册组件自定义关键帧（帧内用 "选区 { 声明 }" 行写法）。
// 与 AddKeyframes 同区输出（单一关键帧区），避免 @keyframes 混入断点桶
// （媒体查询内的 @keyframes 语义混乱，Add 已加防护拦截）。
func (b *CSSBuckets) AddKeyframesDecls(name string, frames []string) {
	if len(frames) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("@keyframes " + name + " {\n")
	for _, f := range frames {
		sb.WriteString("  " + f + "\n")
	}
	sb.WriteString("}")
	b.AddKeyframes(name, sb.String())
}

// Add 向指定断点追加一条规则；空声明被忽略，无有效声明的规则不输出。
func (b *CSSBuckets) Add(breakpoint, selector string, decls []string) {
	// 防护：@keyframes 只能进桌面桶（媒体查询内的 @keyframes 虽合法但语义混乱，
	// 且关键帧本不该受断点限制）。组件自定义帧用 AddKeyframes 或桌面桶。
	if breakpoint != BreakpointDesktop && strings.HasPrefix(selector, "@keyframes ") {
		panic("core: @keyframes 只能加入桌面桶: " + selector)
	}
	filtered := make([]string, 0, len(decls))
	for _, d := range decls {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		return
	}
	rule := selector + " {\n" + indentDecl(filtered) + "}"
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	key := breakpoint + "\x00" + rule
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	switch breakpoint {
	case BreakpointDesktop:
		b.desktop = append(b.desktop, rule)
	case BreakpointTablet:
		b.tablet = append(b.tablet, rule)
	case BreakpointMobile:
		b.mobile = append(b.mobile, rule)
	}
}

// AddHover 悬浮规则专用：规则体包 @media (hover: hover)，仅在支持真悬浮的
// 指针设备生效——触屏设备不再出现「点一下卡住 hover 态」的粘滞问题
// （H5 移动端适配标准做法，docs/06 视觉层：分类效果库 viewport 适配）。
func (b *CSSBuckets) AddHover(sel string, decls []string) {
	filtered := make([]string, 0, len(decls))
	for _, d := range decls {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		return
	}
	rule := sel + " {\n" + indentDecl(filtered) + "\n}"
	wrapped := "@media (hover: hover) {\n  " + rule + "\n}"
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	key := "hover\x00" + wrapped
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	b.hover = append(b.hover, wrapped)
}

// AddContainer 容器查询规则（组件级响应式）：按「组件所在最近容器」的宽度适配，
// 与三端媒体查询（视口级）并存、互不干扰。condition 形如 "(width >= 480px)"。
// 未包裹在启用 container-type 的容器内时不匹配（自然降级为默认样式，零副作用）。
func (b *CSSBuckets) AddContainer(condition, sel string, decls []string) {
	filtered := make([]string, 0, len(decls))
	for _, d := range decls {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		return
	}
	rule := sel + " {\n" + indentDecl(filtered) + "\n}"
	wrapped := "@container " + condition + " {\n  " + rule + "\n}"
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	key := "container\x00" + wrapped
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	b.containersAuto = append(b.containersAuto, wrapped)
}

// AddThemeQuery 主题档位样式查询（@layer wp-theme）：组件响应主题级语义开关
// （如 --wp-density: compact）。层序：wp-auto < wp-theme < wp-local。
func (b *CSSBuckets) AddThemeQuery(containerName, prop, value, sel string, decls []string) {
	b.addQuery(&b.containersTheme, containerName, prop, value, sel, decls)
}

// addQuery 样式查询规则写入指定桶（三桶共用：局部/主题）。
func (b *CSSBuckets) addQuery(bucket *[]string, containerName, prop, value, sel string, decls []string) {
	filtered := make([]string, 0, len(decls))
	for _, d := range decls {
		if d != "" {
			filtered = append(filtered, d)
		}
	}
	if len(filtered) == 0 {
		return
	}
	condition := containerName + " style(" + prop + ": " + value + ")"
	rule := sel + " {\n" + indentDecl(filtered) + "\n}"
	wrapped := "@container " + condition + " {\n  " + rule + "\n}"
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	key := "query\x00" + wrapped
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	*bucket = append(*bucket, wrapped)
}

// ContainerQueryCSS 容器查询块输出（按层聚合）：wp-auto → wp-theme → wp-local。
// 与 String()（内核基础样式）分离，由装配层按层序追加到产物 CSS。
func (b *CSSBuckets) ContainerQueryCSS() string {
	var parts []string
	for _, g := range []struct {
		layer string
		rules []string
	}{
		{"wp-auto", b.containersAuto},
		{"wp-theme", b.containersTheme},
		{"wp-local", b.containersLocal},
	} {
		if len(g.rules) > 0 {
			parts = append(parts, "@layer "+g.layer+" {\n"+strings.Join(g.rules, "\n")+"\n}")
		}
	}
	return strings.Join(parts, "\n\n")
}

// TopLevelCSS 未分层顶层规则（@property 注册等），装配层置于全部 @layer 块之后。
func (b *CSSBuckets) TopLevelCSS() string {
	return strings.Join(b.topLevel, "\n")
}

// AddStyleQuery 样式查询规则（@container <name> style(<prop>: <value>)）：
// 组件响应「容器/主题声明的语义开关」做布局级适配（比颜色变量更深一层——
// 可切换结构，而非只换颜色）。与尺寸查询同桶输出；容器未声明该属性时
// 不匹配（自然降级为默认样式，零副作用）。
func (b *CSSBuckets) AddStyleQuery(containerName, prop, value, sel string, decls []string) {
	b.addQuery(&b.containersLocal, containerName, prop, value, sel, decls)
}

// NeedKeyframes 标记需要输出的关键帧。
func (b *CSSBuckets) NeedKeyframes(name string) {
	if b.keyframes == nil {
		b.keyframes = map[string]bool{}
	}
	b.keyframes[name] = true
}

// String 按固定顺序拼接全部 CSS。
func (b *CSSBuckets) String() string {
	parts := make([]string, 0, len(b.desktop)+len(b.tablet)+len(b.mobile)+2)
	for _, k := range keyframesCatalog {
		if b.keyframes[k.Name] {
			parts = append(parts, k.CSS)
		}
	}
	for _, name := range b.customOrder {
		parts = append(parts, b.customKeyframes[name])
	}
	parts = append(parts, b.desktop...)
	if len(b.tablet) > 0 {
		parts = append(parts, fmt.Sprintf("%s {\n%s\n}", breakpointMedia[BreakpointTablet], strings.Join(b.tablet, "\n")))
	}
	if len(b.mobile) > 0 {
		parts = append(parts, fmt.Sprintf("%s {\n%s\n}", breakpointMedia[BreakpointMobile], strings.Join(b.mobile, "\n")))
	}
	// 悬浮块：触屏治理（@media hover:hover 包裹），置于最末（覆盖语义与源序一致）。
	if len(b.hover) > 0 {
		parts = append(parts, strings.Join(b.hover, "\n"))
	}

	return strings.Join(parts, "\n\n")
}

// NodeClass 节点 CSS 类名。
func NodeClass(id string) string {
	return "wp-c-" + id
}

// indentDecl 声明列表缩进格式化。
func indentDecl(decls []string) string {
	var sb strings.Builder
	for _, d := range decls {
		sb.WriteString("  ")
		sb.WriteString(d)
		sb.WriteString(";\n")
	}
	return sb.String()
}
