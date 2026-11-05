package core

import (
	"fmt"
	"strings"
)

const (
	BreakpointDesktop = "desktop"
	BreakpointTablet  = "tablet"
	BreakpointMobile  = "mobile"
)

var breakpointMedia = map[string]string{
	BreakpointTablet: "@media (max-width: 1024px)",
	BreakpointMobile: "@media (max-width: 767px)",
}

type Keyframe struct {
	Name string
	CSS  string
}

var keyframesCatalog = joinKeyframes(keyframesBuiltin, keyframesAnimate)

var keyframeIndex = buildKeyframeIndex(keyframesCatalog)

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
	discard bool
	desktop []string
	tablet  []string
	mobile  []string
	hover   []string
	active  []string

	containersAuto  []string
	containersTheme []string
	containersLocal []string

	topLevel  []string
	keyframes map[string]bool

	seen map[string]bool

	customKeyframes map[string]string
	customOrder     []string
}

func NewCSSBuckets() *CSSBuckets {
	return &CSSBuckets{}
}

func DiscardCSS() *CSSBuckets {
	return &CSSBuckets{discard: true}
}

func (b *CSSBuckets) discardCSS() bool {
	return b != nil && b.discard
}

func (b *CSSBuckets) AddKeyframes(name, css string) {
	if b.discardCSS() {
		return
	}

	if strings.HasPrefix(css, "@property") {
		for _, r := range b.topLevel {
			if r == css {
				return
			}
		}
		b.topLevel = append(b.topLevel, css)
		return
	}

	if !strings.HasPrefix(css, "@keyframes ") {
		panic("core.AddKeyframes: " + name + " 的内容必须以 \"@keyframes \" 开头；只写帧体请用 AddKeyframesDecls")
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

func (b *CSSBuckets) AddKeyframesDecls(name string, frames []string) {
	if b.discardCSS() || len(frames) == 0 {
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

func (b *CSSBuckets) AddPropertyDecls(name string, decls []string) {
	if b.discardCSS() || len(decls) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("@property " + name + " {\n")
	for _, d := range decls {
		sb.WriteString("  " + d + "\n")
	}
	sb.WriteString("}")
	b.AddKeyframes(name, sb.String())
}

func (b *CSSBuckets) Add(breakpoint, selector string, decls []string) {
	if b.discardCSS() {
		return
	}

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

func (b *CSSBuckets) AddHover(sel string, decls []string) {
	if b.discardCSS() {
		return
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

func (b *CSSBuckets) AddHoverNone(sel string, decls []string) {
	if b.discardCSS() {
		return
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
	if b.discardCSS() {
		return
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

func (b *CSSBuckets) AddContainer(condition, sel string, decls []string) {
	if b.discardCSS() {
		return
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

func (b *CSSBuckets) AddThemeQuery(containerName, prop, value, sel string, decls []string) {
	b.addQuery(&b.containersTheme, containerName, prop, value, sel, decls)
}

func (b *CSSBuckets) addQuery(bucket *[]string, containerName, prop, value, sel string, decls []string) {
	if b.discardCSS() {
		return
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

func (b *CSSBuckets) ContainerQueryCSS() string {
	if b.discardCSS() {
		return ""
	}
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
			parts = append(parts, "@layer "+g.layer+" {\n"+strings.Join(mergeScopeSiblings(g.rules), "\n")+"\n}")
		}
	}
	return strings.Join(parts, "\n\n")
}

func (b *CSSBuckets) TopLevelCSS() string {
	if b.discardCSS() {
		return ""
	}
	return strings.Join(b.topLevel, "\n")
}

func (b *CSSBuckets) AddStyleQuery(containerName, prop, value, sel string, decls []string) {
	b.addQuery(&b.containersLocal, containerName, prop, value, sel, decls)
}

func (b *CSSBuckets) NeedKeyframes(name string) {
	if b.discardCSS() {
		return
	}
	if b.keyframes == nil {
		b.keyframes = map[string]bool{}
	}
	b.keyframes[name] = true
}

func (b *CSSBuckets) String() string {
	if b.discardCSS() {
		return ""
	}
	parts := make([]string, 0, len(b.desktop)+len(b.tablet)+len(b.mobile)+2)
	for _, k := range keyframesCatalog {
		if b.keyframes[k.Name] {
			parts = append(parts, k.CSS)
		}
	}
	for _, name := range b.customOrder {
		parts = append(parts, b.customKeyframes[name])
	}

	parts = append(parts, mergeScopeSiblings(b.desktop)...)
	if len(b.tablet) > 0 {
		parts = append(parts, fmt.Sprintf("%s {\n%s\n}", breakpointMedia[BreakpointTablet], strings.Join(mergeScopeSiblings(b.tablet), "\n")))
	}
	if len(b.mobile) > 0 {
		parts = append(parts, fmt.Sprintf("%s {\n%s\n}", breakpointMedia[BreakpointMobile], strings.Join(mergeScopeSiblings(b.mobile), "\n")))
	}

	if len(b.hover) > 0 {
		parts = append(parts, strings.Join(mergeScopeSiblings(b.hover), "\n"))
	}

	if len(b.active) > 0 {
		parts = append(parts, strings.Join(mergeScopeSiblings(b.active), "\n"))
	}

	return strings.Join(parts, "\n\n")
}

func NodeClass(id string) string {
	return "sky-c-" + id
}

func indentDecl(decls []string) string {
	var sb strings.Builder
	for _, d := range decls {
		sb.WriteString("  ")
		sb.WriteString(d)
		sb.WriteString(";\n")
	}
	return sb.String()
}
