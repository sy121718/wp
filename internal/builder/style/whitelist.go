// Package style 插件组件的声明式样式引擎（插件体系核心之一）。
//
// 设计定位：把「检查器 props 值 + manifest 样式声明」确定性编译进
// core.CSSBuckets（与内置组件同一管线、同一确定性约束）。
//
// 安全模型（三重约束，全部受控生成，无任意 CSS 逃逸）：
//  1. CSS 属性名白名单（本文件 safeProps）：排除 behavior/binding 等危险属性；
//  2. 声明值经 core.IsSafeCSSValue（全组件共用的值白名单唯一入口，
//     封禁引号/分号/花括号与 url() 外联注入）；
//  3. 选择器受控拼装：target 子元素类白名单 + 伪类枚举，
//     不接受任意选择器字符串（防选择器注入越权命中其他组件）。
//
// 表达力（统一 Rule 原语）：
//   - 属性绑定（bindings）：检查器控件值 → CSS 声明；
//   - 变量导出（vars）：检查器控件值 → CSS 变量（--name），供插件包 assets/*.css 用
//     var(--name) 消费 —— 静态 CSS 拿不到单个组件的检查器值，这是两者之间的唯一桥；
//   - 变体（when 条件）：枚举值等值匹配 → 启用声明集（对标 button 的
//     solid/outline/ghost variant 系统）；
//   - 状态（pseudo）：hover / hover-none / active 走专用桶（触屏治理，与内置组件同规则），
//     focus 族与结构伪类（first-child 等）拼在选择器尾部；
//   - 响应式（breakpoints）：tablet/mobile 断点覆盖（desktop 为主声明）；
//   - 容器/主题查询（queries）：尺寸查询 + 主题档位 style 查询，分层输出；
//   - 子元素样式（target）：".badge" 等受限类选择器（后代语义，1~3 段）。
//
// 不在本引擎表达的（有意取舍，见 docs/06-E-plugin-authoring.md）：
//   - 伪元素（::before/::after）与 content 属性 —— content 是字符串注入载体，
//     装饰性伪元素请写进 assets/*.css；
//   - keyframes 与复杂动画 —— 走 assets/*.css；
//   - @font-face / @import / 外联资源 —— 值白名单封禁 url() 外联；
//   - 子代组合符（">"）与任意选择器 —— 放宽组合符只增加越权命中面，后代选择器已够用。
package style

import (
	"regexp"

	"go_wp/internal/builder/core"
)

// pseudoClasses 支持的伪类枚举（受控，不手写选择器字符串）。
//
// 三类槽位不走「拼 :伪类」路径，由 Compile 路由到专用样式桶（触屏治理）：
//   - hover      → AddHover（规则包进 @media (hover: hover)，触屏整段不输出）；
//   - hover-none → AddHoverNone（触屏等价形态，@media (hover: none)）；
//   - active     → AddActive（不包媒体查询：:active 在触屏同样触发，是移动端唯一可靠的按压反馈）。
//
// focus 族与结构伪类（first-child / last-child / only-child）按普通伪类拼在选择器尾部。
var pseudoClasses = map[string]bool{
	"hover": true, "hover-none": true, "focus": true, "active": true,
	"focus-visible": true, "focus-within": true, "disabled": true,
	"first-child": true, "last-child": true, "only-child": true,
}

// dedicatedBucketPseudos 走专用桶的伪类槽位（Compile 据此分派，见 compile.go）。
var dedicatedBucketPseudos = map[string]bool{
	"hover": true, "hover-none": true, "active": true,
}

// varNameRe 自定义属性名白名单（不含前导 "--"，拼装时由引擎补上）。
var varNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

// containerCondRe 容器尺寸查询条件白名单：形如 "(width >= 480px)"。
// 只收「单个比较式」，不收 and/or 组合与任意函数 —— 组合需求用多条 Query 表达。
var containerCondRe = regexp.MustCompile(`^\([a-z-]{2,20} *(>=|<=|>|<|=) *[0-9.]{1,8}(px|em|rem|%|ch|vw|vh)?\)$`)

// containerNameRe 样式查询容器名白名单（如 sky-theme）。
var containerNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

// queryPropRe 样式查询属性白名单（只允许自定义属性，不接受任意 CSS 属性）。
var queryPropRe = regexp.MustCompile(`^--[a-z][a-z0-9-]{0,40}$`)

// queryValueRe 样式查询期望值白名单（枚举式标识，不接受任意字符串）。
var queryValueRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// targetRe 子元素选择器白名单：空（组件根）或 1~3 段类选择器
// （".badge"、".head .title"，类名字符 [A-Za-z0-9_-]，防选择器注入）。
var targetRe = regexp.MustCompile(`^(\.[A-Za-z0-9_-]{1,60})( \.[A-Za-z0-9_-]{1,60}){0,2}$`)

// keyCharRe 条件键与 props 键的白名单字符（字母数字下划线连字符，同
// CustomClassRe 语义，防 when 条件注入）。定义于 whitelist 与其他受控白名单同处。
var keyCharRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// nodeIDRe 节点 ID 白名单（与 core.ValidateNodeID 同规则：1~64 字符，
// 字母数字下划线连字符）。防御深度：内置路径经页面校验，插件场景为
// 未信任输入，引擎入口自卫，防畸形 ID 拼出损坏选择器。
var nodeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// IsSafeNodeID 节点 ID 是否合法。
func IsSafeNodeID(id string) bool {
	return nodeIDRe.MatchString(id)
}

// safeProps CSS 属性名白名单：布局/盒模型/视觉/排版/变换的安全集合。
// 排除项说明：behavior/binding（IE 脚本载体）、content（字符串注入面）、
// font-family 简写外的 @ 规则载体等一律不收；background 允许（值白名单
// 已封 url() 外联，站内相对路径保持既有行为，与内置组件一致）。
var safeProps = map[string]bool{}

func init() {
	for _, p := range []string{
		// 布局
		"display", "flex-direction", "flex-wrap", "flex-grow", "flex-shrink", "flex-basis",
		"align-items", "align-content", "align-self", "justify-content", "justify-items",
		"justify-self", "order", "gap", "row-gap", "column-gap",
		"grid-template-columns", "grid-template-rows", "grid-auto-flow",
		"grid-column", "grid-row", "place-items", "place-content", "place-self",
		// 盘模型
		"width", "height", "min-width", "min-height", "max-width", "max-height",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border", "border-width", "border-style", "border-color", "border-radius",
		"border-top", "border-right", "border-bottom", "border-left",
		// 单边分量（强调条 / 分隔线的常见写法：左/上单独设宽与色而不动其余三边）
		"border-top-width", "border-top-style", "border-top-color",
		"border-right-width", "border-right-style", "border-right-color",
		"border-bottom-width", "border-bottom-style", "border-bottom-color",
		"border-left-width", "border-left-style", "border-left-color",
		"box-sizing", "aspect-ratio", "object-fit", "object-position",
		"overflow", "overflow-x", "overflow-y",
		// 视觉
		"color", "background", "background-color", "opacity", "box-shadow",
		"outline", "outline-width", "outline-style", "outline-color",
		"cursor", "visibility", "pointer-events",
		// 排版
		"font-size", "font-weight", "font-family", "font-style", "line-height",
		"letter-spacing", "word-spacing", "text-align", "text-decoration",
		"text-transform", "text-indent", "text-overflow", "white-space",
		"word-break", "direction",
		// 变换/过渡（keyframes 声明走静态 CSS 资产，不在白名单）
		"transform", "transform-origin", "transition", "transition-property",
		"transition-duration", "transition-timing-function", "transition-delay",
	} {
		safeProps[p] = true
	}
}

// IsSafeProp 属性名是否命中白名单（插件 schema 校验入口之一）。
func IsSafeProp(prop string) bool {
	return safeProps[prop]
}

// IsSafePseudo 伪类是否在受控枚举内。
func IsSafePseudo(pseudo string) bool {
	return pseudo == "" || pseudoClasses[pseudo]
}

// IsDedicatedBucketPseudo 伪类是否走专用样式桶（hover / hover-none / active）。
// 专用桶没有断点维度，Breakpoints 与它们互斥（Validate 拒绝，见 schema.go）。
func IsDedicatedBucketPseudo(pseudo string) bool {
	return dedicatedBucketPseudos[pseudo]
}

// IsSafeVarName 自定义属性名是否合规（不含前导 "--"）。
func IsSafeVarName(name string) bool {
	return varNameRe.MatchString(name)
}

// IsSafeContainerCondition 容器尺寸查询条件是否合规。
func IsSafeContainerCondition(cond string) bool {
	return containerCondRe.MatchString(cond)
}

// IsSafeContainerName 样式查询容器名是否合规。
func IsSafeContainerName(name string) bool {
	return containerNameRe.MatchString(name)
}

// IsSafeQueryProp 样式查询属性是否合规（仅自定义属性）。
func IsSafeQueryProp(prop string) bool {
	return queryPropRe.MatchString(prop)
}

// IsSafeQueryValue 样式查询期望值是否合规。
func IsSafeQueryValue(v string) bool {
	return queryValueRe.MatchString(v)
}

// IsSafeTarget 子元素选择器是否合规（空串表示组件根）。
func IsSafeTarget(target string) bool {
	return target == "" || targetRe.MatchString(target)
}

// breakpointsLegal 断点标识合法性（复用 core 三端常量）。
func breakpointsLegal(bp string) bool {
	return bp == core.BreakpointDesktop || bp == core.BreakpointTablet || bp == core.BreakpointMobile
}
