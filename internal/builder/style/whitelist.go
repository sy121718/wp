package style

import (
	"regexp"

	"go_wp/internal/builder/core"
)

var pseudoClasses = map[string]bool{
	"hover": true, "hover-none": true, "focus": true, "active": true,
	"focus-visible": true, "focus-within": true, "disabled": true,
	"first-child": true, "last-child": true, "only-child": true,
}

var dedicatedBucketPseudos = map[string]bool{
	"hover": true, "hover-none": true, "active": true,
}

var varNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

var containerCondRe = regexp.MustCompile(`^\([a-z-]{2,20} *(>=|<=|>|<|=) *[0-9.]{1,8}(px|em|rem|%|ch|vw|vh)?\)$`)

var containerNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

var queryPropRe = regexp.MustCompile(`^--[a-z][a-z0-9-]{0,40}$`)

var queryValueRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

var targetRe = regexp.MustCompile(`^(\.[A-Za-z0-9_-]{1,60})( \.[A-Za-z0-9_-]{1,60}){0,2}$`)

var keyCharRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

var nodeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func IsSafeNodeID(id string) bool {
	return nodeIDRe.MatchString(id)
}

var safeProps = map[string]bool{}

func init() {
	for _, p := range []string{

		"display", "flex-direction", "flex-wrap", "flex-grow", "flex-shrink", "flex-basis",
		"align-items", "align-content", "align-self", "justify-content", "justify-items",
		"justify-self", "order", "gap", "row-gap", "column-gap",
		"grid-template-columns", "grid-template-rows", "grid-auto-flow",
		"grid-column", "grid-row", "place-items", "place-content", "place-self",

		"width", "height", "min-width", "min-height", "max-width", "max-height",
		"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
		"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
		"border", "border-width", "border-style", "border-color", "border-radius",
		"border-top", "border-right", "border-bottom", "border-left",

		"border-top-width", "border-top-style", "border-top-color",
		"border-right-width", "border-right-style", "border-right-color",
		"border-bottom-width", "border-bottom-style", "border-bottom-color",
		"border-left-width", "border-left-style", "border-left-color",
		"box-sizing", "aspect-ratio", "object-fit", "object-position",
		"overflow", "overflow-x", "overflow-y",

		"color", "background", "background-color", "opacity", "box-shadow",
		"outline", "outline-width", "outline-style", "outline-color",
		"cursor", "visibility", "pointer-events",

		"font-size", "font-weight", "font-family", "font-style", "line-height",
		"letter-spacing", "word-spacing", "text-align", "text-decoration",
		"text-transform", "text-indent", "text-overflow", "white-space",
		"word-break", "direction",

		"transform", "transform-origin", "transition", "transition-property",
		"transition-duration", "transition-timing-function", "transition-delay",
	} {
		safeProps[p] = true
	}
}

func IsSafeProp(prop string) bool {
	return safeProps[prop]
}

func IsSafePseudo(pseudo string) bool {
	return pseudo == "" || pseudoClasses[pseudo]
}

func IsDedicatedBucketPseudo(pseudo string) bool {
	return dedicatedBucketPseudos[pseudo]
}

func IsSafeVarName(name string) bool {
	return varNameRe.MatchString(name)
}

func IsSafeContainerCondition(cond string) bool {
	return containerCondRe.MatchString(cond)
}

func IsSafeContainerName(name string) bool {
	return containerNameRe.MatchString(name)
}

func IsSafeQueryProp(prop string) bool {
	return queryPropRe.MatchString(prop)
}

func IsSafeQueryValue(v string) bool {
	return queryValueRe.MatchString(v)
}

func IsSafeTarget(target string) bool {
	return target == "" || targetRe.MatchString(target)
}

func breakpointsLegal(bp string) bool {
	return bp == core.BreakpointDesktop || bp == core.BreakpointTablet || bp == core.BreakpointMobile
}
