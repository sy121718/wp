package scoring

import (
	"fmt"
	"strings"
	"unicode"
)

type Heading struct {
	Level int
	Text  string
}

type Image struct {
	Src    string
	Alt    string
	SizeKB int
	Kind   string
}

type Input struct {
	Title             string
	MetaDescription   string
	URL               string
	FocusKeyword      string
	SecondaryKeywords []string
	Intent            QueryIntent
	Headings          []Heading
	BodyText          string
	WordCount         int
	Images            []Image
	InternalLinks     int
	ExternalLinks     int
	AnchorTexts       []string
	HasCanonical      bool
	HasSchema         bool
	IsHTTPS           bool
	Locale            string

	HasComparisonTable bool
}

type CheckResult struct {
	Key     string
	Score   int
	Max     int
	Skipped bool
	Actual  string
	Target  string
}

type SectionResult struct {
	Key     string
	Score   int
	Max     int
	Weight  float64
	Percent int
	Color   string
	Checks  []CheckResult
}

type Result struct {
	Total int
	Grade string

	Profile  *Profile
	Sections []SectionResult
}

func Score(in *Input, profile *Profile) *Result {
	if in == nil {
		in = &Input{}
	}
	weights := defaultWeights()
	if profile != nil {
		for k, v := range profile.Weights {
			weights[k] = v
		}
	}

	weightSum := 0.0
	for _, sec := range allSections() {
		w, ok := weights[sec.Key]
		if !ok {
			w = sec.Weight
		}
		weightSum += w
	}
	if weightSum <= 0 {
		weightSum = 1
	}

	res := &Result{Profile: profile}
	total := 0.0
	for _, sec := range allSections() {
		w, ok := weights[sec.Key]
		if !ok {
			w = sec.Weight
		}
		w = w / weightSum
		sr := SectionResult{Key: sec.Key, Weight: w}
		for _, ck := range sec.Checks {
			score, actual := ck.Score(in)
			skipped := score < 0
			if skipped {
				sr.Checks = append(sr.Checks, CheckResult{
					Key: ck.Key, Score: 0, Max: ck.Max, Skipped: true,
					Actual: actual,
					Target: TargetOf(ck.Key),
				})
				continue
			}
			if score > ck.Max {
				score = ck.Max
			}
			sr.Score += score
			sr.Max += ck.Max
			sr.Checks = append(sr.Checks, CheckResult{
				Key: ck.Key, Score: score, Max: ck.Max,
				Actual: actual,
				Target: TargetOf(ck.Key),
			})
		}
		if sr.Max > 0 {
			sr.Percent = sr.Score * 100 / sr.Max
			total += float64(sr.Score) / float64(sr.Max) * w * 100
		}
		sr.Color = colorOf(sr.Percent)
		res.Sections = append(res.Sections, sr)
	}
	res.Total = int(total + 0.5)
	for _, b := range gradeBands {
		if res.Total >= b.Min {
			res.Grade = b.Grade
			break
		}
	}
	return res
}

func defaultWeights() map[string]float64 {
	return map[string]float64{
		"title": 0.15, "meta": 0.05, "headings": 0.10, "content": 0.25,
		"keywords": 0.15, "links": 0.10, "images": 0.10, "tech": 0.10,
	}
}

type Profile struct {
	Type    string
	Weights map[string]float64
	Reason  string
}

func ProductProfile() *Profile {
	return &Profile{Type: "product", Weights: map[string]float64{
		"images": 0.15, "tech": 0.15, "content": 0.15,
	}, Reason: "商品页：图片与技术权重上调，长文深度权重下调"}
}

func LandingProfile() *Profile {
	return &Profile{Type: "landing", Weights: map[string]float64{
		"tech": 0.15, "title": 0.20, "meta": 0.10, "content": 0.15,
	}, Reason: "落地页：技术与标题/元描述权重上调，内容深度下调"}
}

func GuideProfile() *Profile {
	return &Profile{Type: "guide", Weights: map[string]float64{
		"content": 0.30, "keywords": 0.18, "links": 0.12, "images": 0.05,
	}, Reason: "长文指南：内容/关键词/链接权重上调，图片权重下调"}
}

func isCJK(locale string) bool {
	return locale == "" || strings.HasPrefix(strings.ToLower(locale), "zh")
}

func countKeyword(text, kw string) int {
	if kw == "" || text == "" {
		return 0
	}
	textLower := strings.ToLower(text)
	kwLower := strings.ToLower(kw)
	count := 0
	start := 0
	for {
		i := strings.Index(textLower[start:], kwLower)
		if i < 0 {
			break
		}
		count++
		start += i + len(kwLower)
	}
	return count
}

func keywordDensity(in *Input) float64 {
	if in.FocusKeyword == "" {
		return 0
	}
	total := in.WordCount
	if total <= 0 {
		total = len([]rune(in.BodyText))
	}
	if total <= 0 {
		return 0
	}
	hits := countKeyword(in.BodyText, in.FocusKeyword)
	unit := 1
	if !isCJK(in.Locale) {
		unit = len(strings.Fields(in.FocusKeyword))
		if unit < 1 {
			unit = 1
		}
	} else {
		unit = len([]rune(in.FocusKeyword))
	}
	return float64(hits*unit) / float64(total) * 100
}

func fmtPct(v float64) string { return fmt.Sprintf("%.1f%%", v) }

func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r <= 0x007F {
			w++
			continue
		}
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			w += 2
			continue
		}

		if r > 0x00FF {
			w += 2
			continue
		}
		w++
	}
	return w
}
