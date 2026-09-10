// Package scoring 实现 SEO 评分引擎（纯函数、无 IO、表驱动）。
//
// 规则源：docs/02-E1-seo-scoring-rules.md（本地 rubric + Yoast 公开方法论 + RankMath + Lighthouse 印证）。
// 输入由调用方从 Page Document AST + settings.seo 提取，本包只做计算。
package scoring

import (
	"fmt"
	"strings"
)

// Heading 标题结构项。
type Heading struct {
	Level int // 1..6
	Text  string
}

// Image 图片项。
type Image struct {
	Src    string
	Alt    string
	SizeKB int
	Kind   string // hero / content / screenshot / icon / thumb（空按 content）
}

// Input 评分输入。
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
	Locale            string // zh / en，空按 zh
}

// CheckResult 单项检查结果。
type CheckResult struct {
	Key       string
	Label     string
	Score     int
	Max       int
	Actual    string // 实测值描述
	Benchmark string
	Hint      string
	Target    string // 改进位置（settings.seo.x / node:type），前端点击跳转
}

// SectionResult 维度结果（侧栏一个圆点）。
type SectionResult struct {
	Key     string
	Label   string
	Score   int
	Max     int
	Weight  float64
	Percent int
	Color   string
	Checks  []CheckResult
}

// Result 评分结果。
type Result struct {
	Total    int
	Grade    string
	Sections []SectionResult
}

// Score 计算总分与逐项结果。profile 为 nil 时使用默认权重。
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
	// 权重预归一：profile 覆盖后的权重和不一定等于 1（GuideProfile 覆盖后是 1.05），
	// 直接累加会让满分变成 105 分。先求和再逐项归一，保证任何 profile 下满分都是 100。
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

	res := &Result{}
	total := 0.0
	for _, sec := range allSections() {
		w, ok := weights[sec.Key]
		if !ok {
			w = sec.Weight
		}
		w = w / weightSum
		sr := SectionResult{Key: sec.Key, Label: sec.Label, Weight: w}
		for _, ck := range sec.Checks {
			score, actual := ck.Score(in)
			if score > ck.Max {
				score = ck.Max
			}
			if score < 0 {
				score = 0
			}
			sr.Score += score
			sr.Max += ck.Max
			sr.Checks = append(sr.Checks, CheckResult{
				Key: ck.Key, Label: ck.Label, Score: score, Max: ck.Max,
				Actual: actual, Benchmark: ck.Benchmark, Hint: ck.Hint,
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

// defaultWeights 默认维度权重（rubric 权重卡）。
func defaultWeights() map[string]float64 {
	return map[string]float64{
		"title": 0.15, "meta": 0.05, "headings": 0.10, "content": 0.25,
		"keywords": 0.15, "links": 0.10, "images": 0.10, "tech": 0.10,
	}
}

// Profile 页型调权（rubric Weight Adjustments 表）。
type Profile struct {
	Type    string             // home / article / product / landing / local
	Weights map[string]float64 // 覆盖默认权重
	Reason  string             // 调权理由（结果里回显）
}

// ProductProfile 商品页：提图片/技术，降内容深度。
func ProductProfile() *Profile {
	return &Profile{Type: "product", Weights: map[string]float64{
		"images": 0.15, "tech": 0.15, "content": 0.15,
	}, Reason: "商品页：图片与技术权重上调，长文深度权重下调"}
}

// LandingProfile 落地页：提技术/标题。
func LandingProfile() *Profile {
	return &Profile{Type: "landing", Weights: map[string]float64{
		"tech": 0.15, "title": 0.20, "content": 0.15,
	}, Reason: "落地页：技术与标题权重上调，内容深度下调"}
}

// GuideProfile 长文指南：提内容/关键词/链接。
func GuideProfile() *Profile {
	return &Profile{Type: "guide", Weights: map[string]float64{
		"content": 0.30, "keywords": 0.18, "links": 0.12, "images": 0.05,
	}, Reason: "长文指南：内容/关键词/链接权重上调，图片权重下调"}
}

// ---------- 文本统计辅助（首版：CJK 按字符、英文按空格切词） ----------

// isCJK 是否中文语境（影响句长/段长阈值）。
func isCJK(locale string) bool {
	return locale == "" || strings.HasPrefix(strings.ToLower(locale), "zh")
}

// countKeyword 统计关键词出现次数（大小写不敏感）。
func countKeyword(text, kw string) int {
	if kw == "" || text == "" {
		return 0
	}
	return strings.Count(strings.ToLower(text), strings.ToLower(kw))
}

// keywordDensity 关键词密度（百分比）。CJK 以字符数、英文以词数近似统计总量。
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

// fmtPct 百分比格式化（一位小数）。
func fmtPct(v float64) string { return fmt.Sprintf("%.1f%%", v) }
