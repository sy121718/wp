package seo

type ScoreGradeLabel struct{ Key, Fallback string }

var ScoreGradeLabels = map[string]ScoreGradeLabel{
	"green":        {"admin.seo.score.grade.excellent", "优秀"},
	"lightgreen":   {"admin.seo.score.grade.good", "良好"},
	"yellow":       {"admin.seo.score.grade.needsWork", "需改进"},
	"red":          {"admin.seo.score.grade.poor", "差"},
	"red-blocking": {"admin.seo.score.grade.missing", "缺失"},
}

func ScoreGradeText(tr func(key, fallback string) string, color string) string {
	if l, ok := ScoreGradeLabels[color]; ok {
		return tr(l.Key, l.Fallback)
	}
	return color
}
