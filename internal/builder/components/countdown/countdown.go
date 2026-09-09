// Package countdown 实现 core.countdown 倒计时组件（对标 GrapesJS countdown 组件生态）。
//
// 轻量增强型原子组件：目标时间 + 天/时/分/秒四段数字位。零 JS 输出静态占位 00
// 与 data-target 增强属性，客户端经 enhance.js 按目标时间计算剩余并逐秒刷新；
// BuildView 解析目标时间（RFC3339 / 常见日期时间格式）规范化后写入 data-target，
// 保证确定性构建（构建期不依赖 time.Now，剩余时间由客户端实时计算）。
package countdown

import (
	"fmt"
	"time"

	"go_wp/internal/builder/core"
)

// Type 组件类型标识。
const Type = "core.countdown"

// targetDateLayouts 支持的输入时间格式（按优先级顺序解析）。
var targetDateLayouts = []string{
	time.RFC3339,          // 2006-01-02T15:04:05Z07:00
	"2006-01-02 15:04:05", // 无时区（UTC）
	"2006-01-02T15:04:05", // 无时区 ISO（UTC）
	"2006-01-02 15:04",    // 分钟精度
	"2006-01-02",          // 日期（当日 00:00:00）
}

// Props countdown 属性。
type Props struct {
	// TargetDate 目标时间（RFC3339 或 2006-01-02 15:04:05 / 2006-01-02）。
	TargetDate string `json:"targetDate,omitempty" ct:"text,maxlen=30,sec=content,label=目标时间"`
	// ShowDays 是否显示「天」位（目标超过 24 小时时开启）。
	ShowDays bool `json:"showDays,omitempty" ct:"bool,sec=content,label=显示天数"`
	// Advanced 通用高级属性（docs/02-C0）。
	Advanced core.AdvancedProps `json:"advanced" ct:"group"`
}

// Widget 基座实例。
var Widget = core.Atom[Props]{
	Spec: core.AtomSpec[Props]{
		TypeName:      Type,
		ValidateExtra: validateExtra,
	},
}

// validateExtra 关系性校验：目标时间必填且可解析。
func validateExtra(p *Props, nodeID string) (err error) {
	if p.TargetDate == "" {
		return fmt.Errorf("必须提供目标时间")
	}
	if _, ok := parseTargetDate(p.TargetDate); !ok {
		return fmt.Errorf("无效的目标时间: %q（支持 RFC3339 或 2006-01-02 15:04:05）", p.TargetDate)
	}
	return nil
}

// parseTargetDate 解析目标时间字符串（支持多格式），失败返回 ok=false。
func parseTargetDate(s string) (time.Time, bool) {
	for _, layout := range targetDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// compileCSS 倒计时容器/数字位/分隔符/单位标签样式。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)

	b.Add(core.BreakpointDesktop, sel, []string{
		"display: inline-flex",
		"align-items: flex-start",
		"gap: 8px",
		"font-variant-numeric: tabular-nums",
	})
	b.Add(core.BreakpointDesktop, sel+" .cd-item", []string{
		"display: inline-flex",
		"flex-direction: column",
		"align-items: center",
		"gap: 2px",
	})
	b.Add(core.BreakpointDesktop, sel+" .cd-num", []string{
		"min-width: 2ch",
		"font-size: 2rem",
		"font-weight: 700",
		"line-height: 1",
		"text-align: center",
		"padding: 8px 10px",
		"background: rgba(0,0,0,0.05)",
		"border-radius: 6px",
	})
	b.Add(core.BreakpointDesktop, sel+" .cd-sep", []string{
		"font-size: 2rem",
		"font-weight: 700",
		"line-height: 1",
		"opacity: 0.5",
		"padding-top: 8px",
	})
	b.Add(core.BreakpointDesktop, sel+" .cd-label", []string{
		"font-size: 0.75rem",
		"font-weight: 400",
		"opacity: 0.6",
	})
}

// init 注册倒计时组件。
func init() {
	core.Register(Widget)
}
