package scoreview

import "go_wp/internal/seo/scoring"

type View struct {
	OK       bool
	Total    int
	Grade    string
	Sections []Section

	ProfileType   string
	ProfileReason string

	Duplicates []Duplicate

	Title string

	EmptyKey      string
	EmptyFallback string
	SerpTitle     string
	SerpURL       string
	SerpDesc      string
	HasSerpTitle  bool
	HasSerpURL    bool
	HasSerpDesc   bool
}

type Section struct {
	Key    string
	Color  string
	Score  int
	Max    int
	Issues []Issue
}

type Issue struct {
	Key    string
	Actual string
	Target string
}

type Duplicate struct {
	Kind string
	Page string
}

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
