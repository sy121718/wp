package counter

import (
	"strconv"

	"go_wp/pkg/builder/core"
)

func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

type View struct {
	StartData    string
	EndData      string
	DecimalsData string
	DurationData string

	CSSMode bool

	Value string

	Prefix string
	Suffix string
	Label  string
}

func BuildView(p *Props) View {
	duration := p.Duration
	if duration <= 0 {
		duration = 2
	}
	cssMode := p.Decimals == 0
	value := formatNum(p.End, p.Decimals)
	if cssMode {
		value = ""
	}
	return View{
		CSSMode:      cssMode,
		StartData:    strconv.FormatFloat(p.Start, 'f', -1, 64),
		EndData:      strconv.FormatFloat(p.End, 'f', -1, 64),
		DecimalsData: strconv.Itoa(p.Decimals),
		DurationData: strconv.FormatFloat(duration, 'f', -1, 64),
		Value:        value,
		Prefix:       p.Prefix,
		Suffix:       p.Suffix,
		Label:        p.Label,
	}
}

func (v View) DeclareFeatures() (attrs, classes []string) {
	if v.CSSMode {
		return nil, nil
	}
	return []string{"data-counter", "data-start", "data-end", "data-decimals", "data-duration"}, nil
}
