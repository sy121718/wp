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
	active  []string // 按压规则：不包 hover:hover（:active 在触屏上同样生效，是移动端唯一可靠的按下反馈）
	// 容器查询三桶：按「样式来源层级」分开，输出时分别包进 @layer
	// sky-auto（自动适配：@container 尺寸查询）< sky-theme（主题档位 style 查询）<
	// sky-local（容器/作者显式声明）——优先级由层序决定，不依赖源顺序。
	containersAuto  []string // 自动适配（尺寸查询）→ @layer sky-auto
	containersTheme []string // 主题档位（style 查询）→ @layer sky-theme
	containersLocal []string // 局部显式声明（style 查询）→ @layer sky-local
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

// AddActive 按压规则专用：与 AddHover 的区别是**不包** @media (hover: hover)。
//
// 原因：:active 在触屏上同样触发（手指按下即激活），是移动端唯一可靠的「按下反馈」。
// 若一并包进 hover:hover，触屏设备将完全失去按压反馈 —— 这正是此前按压态缺失
// （effects.go 分类目录按钮 FX 里的 ◻️ 项）留下的体验缺口。
// AddHoverNone 添加「无悬停设备」下生效的规则（触屏）。
//
// 与 AddHover 互补：AddHover 只包 @media (hover: hover)，触屏上整段不输出；
// 而触屏没有悬停，任何依赖 :hover 的形态都必须在这里给出等价形态，
// 否则手机上永远不触发（cardstack 的四种悬停形态踩过的坑）。
// 复用 hover 桶输出 —— 两个媒体查询互斥，顺序无关。
func (b *CSSBuckets) AddHoverNone(sel string, decls []string) {
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
	wrapped := "@media (hover: none) {\n  " + rule + "\n}"
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	key := "hovernone" + wrapped
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	b.hover = append(b.hover, wrapped)
}

func (b *CSSBuckets) AddActive(sel string, decls []string) {
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
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	key := "active\x00" + rule
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	b.active = append(b.active, rule)
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

// AddThemeQuery 主题档位样式查询（@layer sky-theme）：组件响应主题级语义开关
// （如 --sky-density: compact）。层序：sky-auto < sky-theme < sky-local。
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

// ContainerQueryCSS 容器查询块输出（按层聚合）：sky-auto → sky-theme → sky-local。
// 与 String()（内核基础样式）分离，由装配层按层序追加到产物 CSS。
func (b *CSSBuckets) ContainerQueryCSS() string {
	var parts []string
	for _, g := range []struct {
		layer string
		rules []string
	}{
		{"sky-auto", b.containersAuto},
		{"sky-theme", b.containersTheme},
		{"sky-local", b.containersLocal},
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
	// 按压块：置于 hover 之后（同时「悬停且按住」时按压态胜出，符合直觉）。不包 hover:hover。
	if len(b.active) > 0 {
		parts = append(parts, strings.Join(b.active, "\n"))
	}

	return strings.Join(parts, "\n\n")
}

// NodeClass 节点 CSS 类名。
func NodeClass(id string) string {
	return "sky-c-" + id
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
