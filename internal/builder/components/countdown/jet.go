// Package countdown — Jet 渲染路径辅助导出（Phase 1）。
//
// 与 render 函数并行的新路径：目标时间解析 / CSS 生成 / 单元列表预计算保留在 Go，
// HTML 拼装交给 countdown.jet 模板。render 函数保持不变（旧输出），
// 本文件只做最小导出与等价的数据准备。
package countdown

import (
	"time"

	"go_wp/internal/builder/core"
)

// CompileCSS 导出倒计时样式编译（复用 render 内部的 compileCSS）。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// UnitView 单个时间单元渲染视图（供 countdown.jet 模板使用）。
type UnitView struct {
	// Unit 单元标识（days/hours/minutes/seconds），写入 data-unit 供客户端增强定位。
	Unit string
	// Label 单元标签（天/时/分/秒；构建期按当前语言填充，多语言 P4）。
	Label string
	// Sep 该单元后是否输出分隔符「:」（最后一个单元为 false）。
	Sep bool
}

// View countdown 渲染视图数据（供 countdown.jet 模板使用）。
type View struct {
	// TargetData 规范化目标时间（RFC3339 UTC），写入 data-target 供客户端计算。
	TargetData string
	// ShowDaysData data-show-days 增强属性值（"1" 显示天 / "0" 不显示）。
	ShowDaysData string
	// Units 时间单元列表（按 ShowDays 决定是否含 days 位）。
	Units []UnitView
}

// BuildView 生成倒计时渲染视图：解析目标时间规范化 + 时间单元列表。
// 静态输出数字位固定为 00 占位（确定性），剩余时间由客户端按 TargetData 实时计算。
func BuildView(p *Props) View {
	v := View{ShowDaysData: "0"}
	if p.ShowDays {
		v.ShowDaysData = "1"
	}
	if t, ok := parseTargetDate(p.TargetDate); ok {
		v.TargetData = t.UTC().Format(time.RFC3339)
	}

	units := []UnitView{
		{Unit: "hours", Label: "时"},
		{Unit: "minutes", Label: "分"},
		{Unit: "seconds", Label: "秒"},
	}
	if p.ShowDays {
		units = append([]UnitView{{Unit: "days", Label: "天"}}, units...)
	}
	for i := range units {
		units[i].Sep = i < len(units)-1
	}
	v.Units = units
	return v
}

// 访客面组件文案 key：site.component.{type}.{prop}（docs/06-D §10.3）。
const (
	// TextKeyDays 「天」单元标签的词条 key。
	TextKeyDays = "site.component.countdown.days"
	// TextKeyHours 「时」单元标签的词条 key。
	TextKeyHours = "site.component.countdown.hours"
	// TextKeyMinutes 「分」单元标签的词条 key。
	TextKeyMinutes = "site.component.countdown.minutes"
	// TextKeySeconds 「秒」单元标签的词条 key。
	TextKeySeconds = "site.component.countdown.seconds"
)

// unitText 单元标识 → (词条 key, 缺词条时的原中文兜底)。
var unitText = map[string]struct{ Key, Fallback string }{
	"days":    {Key: TextKeyDays, Fallback: "天"},
	"hours":   {Key: TextKeyHours, Fallback: "时"},
	"minutes": {Key: TextKeyMinutes, Fallback: "分"},
	"seconds": {Key: TextKeySeconds, Fallback: "秒"},
}

// ApplyI18n 按当前语言填充时间单元标签（实现 core.I18nAware）。
// text 为 nil 或未命中词条时使用包内中文兜底，保证标签永不为空。
func (v *View) ApplyI18n(text func(key, fallback string) string) {
	if v == nil {
		return
	}
	for i := range v.Units {
		u := &v.Units[i]
		meta, ok := unitText[u.Unit]
		if !ok {
			continue
		}
		if text == nil {
			u.Label = meta.Fallback
			continue
		}
		u.Label = text(meta.Key, meta.Fallback)
	}
}
