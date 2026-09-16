// Package countdown 实现 core.countdown 倒计时组件（对标 GrapesJS countdown 组件生态）。
//
// 轻量增强型原子组件：目标时间 + 天/时/分/秒四段数字位。零 JS 输出静态占位 00
// 与 data-target 增强属性，客户端经 enhance.js 按目标时间计算剩余并逐秒刷新；
// BuildView 解析目标时间（RFC3339 / 常见日期时间格式）规范化后写入 data-target，
// 保证确定性构建（构建期不依赖 time.Now，剩余时间由客户端实时计算）。
package countdown

import (
	_ "embed" // enhance.js 经 //go:embed 打进二进制
	"fmt"
	"time"

	"go_wp/internal/builder/core"
)

// enhanceJS 组件行为源。与 .go / .css / .jet 同目录：改交互不必再去 enhance.js 里找。
//
//go:embed enhance.js
var enhanceJS string

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
		DisplayName:     "倒计时",
		Hint:            "营销倒计时",
		PaletteCategory: core.PaletteCategoryBasic,
		DefaultProps: map[string]any{
			"targetDate": "2030-01-01 00:00:00",
			"showDays":   true,
		},
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

// countdownCSS 组件样式源。与组件同目录：改样式不必再进 Go 字符串数组
// （有补全 / lint / 格式化），而作用域替换、桶划分、确定性输出仍由构建期负责。
//
//go:embed countdown.css
var countdownCSS string

// compileCSS 倒计时容器/数字位/分隔符/单位标签样式（纯静态，与 Props 无关）。
func compileCSS(id string, p *Props, b *core.CSSBuckets) {
	sel := "." + core.NodeClass(id)
	if err := core.ApplyComponentCSS(b, sel, countdownCSS); err != nil {
		// 样式源解析失败属于构建期缺陷，必须在测试/构建时暴露；静默跳过的后果是产物悄悄少了样式。
		panic(fmt.Sprintf("countdown 组件样式解析失败: %v", err))
	}
}

// init 注册倒计时组件。
func init() {
	core.Register(Widget)
	core.RegisterTemplate("countdown", countdownTemplate)
	core.RegisterEnhanceBlock(core.EnhanceBlock{
		Fns:    []string{"initCountdowns"},
		Feats:  []string{"data-countdown"},
		Source: enhanceJS,
	})
}

// countdownTemplate 组件模板。与 .go / .css 同目录：改结构不必去 internal/templates/components/ 找
// （注册后由 loader 优先采用，见 core.RegisterTemplate）。
//
//go:embed countdown.jet
var countdownTemplate string
