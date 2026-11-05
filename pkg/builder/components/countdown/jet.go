package countdown

import (
	"time"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type UnitView struct {
	Unit string

	Label string

	Sep bool
}

type View struct {
	TargetData string

	ShowDaysData string

	Units []UnitView
}

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

const (
	TextKeyDays = "site.component.countdown.days"

	TextKeyHours = "site.component.countdown.hours"

	TextKeyMinutes = "site.component.countdown.minutes"

	TextKeySeconds = "site.component.countdown.seconds"
)

var unitText = map[string]struct{ Key, Fallback string }{
	"days":    {Key: TextKeyDays, Fallback: "天"},
	"hours":   {Key: TextKeyHours, Fallback: "时"},
	"minutes": {Key: TextKeyMinutes, Fallback: "分"},
	"seconds": {Key: TextKeySeconds, Fallback: "秒"},
}

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

func (v View) DeclareFeatures() (attrs, classes []string) {
	attrs = append(attrs, "data-countdown", "data-target", "data-show-days")
	if len(v.Units) > 0 {
		attrs = append(attrs, "data-unit")
	}
	return attrs, nil
}
