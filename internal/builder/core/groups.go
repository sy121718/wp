package core

import (
	"fmt"
	"regexp"
)

// 共享排版组（对标 Elementor Group_Control_Typography，但声明式实现）。
// heading/text 等文本类组件嵌入 TextStyle，使用同一份校验与 CSS 生成，
// 消除组件间重复定义。组内字段（fontSize/lineHeight/textAlign）为组件通用
// 组合件，专有字段（如 heading 的字重/转换）仍留在组件自己的 Props。
type TextStyle struct {
	Desktop TextStyleValue `json:"desktop,omitempty"`
	Tablet  TextStyleValue `json:"tablet,omitempty"`
	Mobile  TextStyleValue `json:"mobile,omitempty"`
}

// TextStyleValue 单端排版值。
type TextStyleValue struct {
	// FontSize 字号：px/rem/em/vw 或 clamp() 流式字号。
	FontSize string `json:"fontSize,omitempty"`
	// LineHeight 行高：倍数（如 1.2）或长度值。
	LineHeight string `json:"lineHeight,omitempty"`
	// TextAlign 文字对齐：left/center/right/justify。
	TextAlign string `json:"textAlign,omitempty"`
}

// 共享排版白名单。
var (
	typographyLenRe    = regexp.MustCompile(`^[0-9.]+(px|rem|em|vw|%)?$`)
	typographyClampRe  = regexp.MustCompile(`^clamp\([0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw),\s*[0-9.]+(px|rem|em|vw)\)$`)
	typographyAlignMap = map[string]bool{"left": true, "center": true, "right": true, "justify": true}
)

// ValidateTextStyle 校验三端排版值（共享组校验，多组件复用）。
func ValidateTextStyle(nodeID string, ts *TextStyle) (err error) {
	for bp, v := range map[string]TextStyleValue{
		"desktop": ts.Desktop, "tablet": ts.Tablet, "mobile": ts.Mobile,
	} {
		if v.FontSize != "" && !isTypographyValue(v.FontSize) {
			return fmt.Errorf("节点 %s: 无效的 %s 端字号: %q", nodeID, bp, v.FontSize)
		}
		if v.LineHeight != "" && !isTypographyValue(v.LineHeight) {
			return fmt.Errorf("节点 %s: 无效的 %s 端行高: %q", nodeID, bp, v.LineHeight)
		}
		if v.TextAlign != "" && !typographyAlignMap[v.TextAlign] {
			return fmt.Errorf("节点 %s: 无效的 %s 端对齐: %q", nodeID, bp, v.TextAlign)
		}
	}
	return nil
}

// isTypographyValue 长度值或 clamp() 流式字号白名单。
func isTypographyValue(v string) bool {
	return len(v) <= 40 && (typographyLenRe.MatchString(v) || typographyClampRe.MatchString(v))
}

// Decls 单端排版值 → CSS 声明列表（组件 compileCSS 直接 append，共享生成逻辑）。
func (v TextStyleValue) Decls() []string {
	var decls []string
	if v.FontSize != "" {
		decls = append(decls, "font-size: "+v.FontSize)
	}
	if v.LineHeight != "" {
		decls = append(decls, "line-height: "+v.LineHeight)
	}
	if v.TextAlign != "" {
		decls = append(decls, "text-align: "+v.TextAlign)
	}
	return decls
}

// BreakpointDecls 按断点取声明列表（桌面/平板/手机，供三端 bucket 拼接）。
func (ts TextStyle) BreakpointDecls(bp string) []string {
	switch bp {
	case BreakpointDesktop:
		return ts.Desktop.Decls()
	case BreakpointTablet:
		return ts.Tablet.Decls()
	case BreakpointMobile:
		return ts.Mobile.Decls()
	}
	return nil
}
