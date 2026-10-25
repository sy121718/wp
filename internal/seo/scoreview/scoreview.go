// Package scoreview 把 SEO 评分结果转成「只含事实」的片段视图。
//
// 为什么单独一个包：这份视图由三处（页面草稿 / 文章 / 商品及其分类品牌）共用，
// 模板只有一份 fragments/seo_score.html。三处各写一遍转换的下场是「哪些检查项算未达标」
// 这种判断分叉，而分叉后每一处单独看都对。
//
// 只给事实：维度 key、分数、实测值、跳转目标、冲突页面（种类 + 路径）。维度名、检查名、
// 基准、建议、评分档位文案全部由模板按 key 取词 —— 后端不产出任何给用户看的字符串。
package scoreview

import "go_wp/internal/seo/scoring"

// View 评分片段的根数据。
type View struct {
	OK       bool
	Total    int
	Grade    string
	Sections []Section
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由：编辑者要能看出
	// 「这份分数是按哪种页型的尺子量的」，否则同一段内容在两种页型下差几分无从解释。
	ProfileType   string
	ProfileReason string
	// Duplicates 与本页标题重复的其它页面（列出页面本身，而不是只报数量）。
	Duplicates []Duplicate
	// Title 原始标题（重复提示句由模板按它拼）。
	Title string
	// EmptyKey / EmptyFallback 空态文案事实（读不到实体时给一句可读的话）。
	EmptyKey      string
	EmptyFallback string
	SerpTitle     string
	SerpURL       string
	SerpDesc      string
	HasSerpTitle  bool
	HasSerpURL    bool
	HasSerpDesc   bool
}

// Section 单个评分维度。
type Section struct {
	Key    string
	Color  string
	Score  int
	Max    int
	Issues []Issue
}

// Issue 单条未达标检查：哪一项、实测多少、点了跳哪里。
type Issue struct {
	Key    string
	Actual string
	Target string
}

// Duplicate 一个标题重复的页面：种类（product / category / brand / page / article）+ 路径。
type Duplicate struct {
	Kind string
	Page string
}

// Of 评分结果 → 视图（只转维度与未达标项；SERP、重复标题、空态由调用方按自己的来源补）。
func Of(res *scoring.Result) View {
	if res == nil {
		return View{}
	}
	v := View{OK: true, Total: res.Total, Grade: res.Grade}
	if res.Profile != nil {
		v.ProfileType = res.Profile.Type
		v.ProfileReason = res.Profile.Reason
	}
	v.Sections = Sections(res.Sections)
	return v
}

// Sections 评分分项 → 视图行（达标项不进 Issues：只有一个「未达标才是建议」的口径）。
func Sections(sections []scoring.SectionResult) []Section {
	out := make([]Section, 0, len(sections))
	for _, sec := range sections {
		item := Section{Key: sec.Key, Color: sec.Color, Score: sec.Score, Max: sec.Max}
		for _, ck := range sec.Checks {
			if ck.Score >= ck.Max {
				continue
			}
			item.Issues = append(item.Issues, Issue{Key: ck.Key, Actual: ck.Actual, Target: ck.Target})
		}
		out = append(out, item)
	}
	return out
}

// Duplicates 冲突页面 → 视图行（种类 + 路径）。
func Duplicates(entries []scoring.TitleEntry) []Duplicate {
	if len(entries) == 0 {
		return nil
	}
	out := make([]Duplicate, 0, len(entries))
	for _, e := range entries {
		out = append(out, Duplicate{Kind: e.Kind, Page: e.Page})
	}
	return out
}
