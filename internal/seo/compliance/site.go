package compliance

import (
	"cmp"
	"slices"
	"strings"

	"go_wp/internal/seo"
)

type SiteReport struct {
	Reports []*Report `json:"reports"`

	Findings []Finding `json:"findings"`

	Checked int `json:"checked"`

	Checks int `json:"checks"`
}

func InspectSite(arts []Artifact) *SiteReport {
	in := make([]Artifact, len(arts))
	copy(in, arts)
	slices.SortStableFunc(in, func(a, b Artifact) int { return cmp.Compare(a.URL, b.URL) })

	rep := &SiteReport{Checked: len(in), Checks: perArtifactChecks + siteChecks}
	byPath := make(map[string]int, len(in))
	for i, a := range in {
		rep.Reports = append(rep.Reports, Inspect(a))
		byPath[seo.CanonicalPublicPath(a.URL)] = i
	}

	var cross []Finding
	for _, a := range in {
		doc := parseHead(headOf(string(a.HTML)))
		for _, e := range doc.Hreflangs {
			lang := strings.TrimSpace(e.Lang)
			if lang == "" || strings.EqualFold(lang, "x-default") {
				continue
			}
			idx, ok := byPath[seo.CanonicalPublicPath(pathOf(e.Href))]
			if !ok {

				continue
			}
			target := in[idx]
			if target.Lang != "" && !strings.EqualFold(target.Lang, lang) {
				cross = append(cross, Finding{
					Rule: RuleHreflangTargetLangMismatch, Level: LevelError,
					URL: a.URL, Lang: a.Lang, ArtifactHash: a.ArtifactHash,
					Evidence: truncateEvidence("声明 hreflang=" + lang + " → " + e.Href +
						"，但该地址的产物构建语言是 " + target.Lang),
					Expect: "hreflang 的语言码必须与目标产物的实际语言一致，否则搜索引擎会按错误的语言版本配对",
				})
			}
			if !declaresBack(target, a.URL, a.Lang) {
				cross = append(cross, Finding{
					Rule: RuleHreflangNotReciprocal, Level: LevelError,
					URL: a.URL, Lang: a.Lang, ArtifactHash: a.ArtifactHash,
					Evidence: truncateEvidence("本页声明 " + lang + " → " + e.Href + "，但该页面的产物没有回指 " +
						seo.CanonicalPublicPath(a.URL)),
					Expect: "互指必须双向：单向声明会被搜索引擎整组忽略",
				})
			}
		}
	}
	slices.SortStableFunc(cross, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.URL, b.URL), cmp.Compare(a.Rule, b.Rule))
	})
	rep.Findings = cross
	return rep
}

func declaresBack(target Artifact, fromURL, fromLang string) bool {
	doc := parseHead(headOf(string(target.HTML)))
	want := seo.CanonicalPublicPath(fromURL)
	for _, e := range doc.Hreflangs {
		if !strings.EqualFold(strings.TrimSpace(e.Lang), fromLang) {
			continue
		}
		if seo.CanonicalPublicPath(pathOf(e.Href)) == want {
			return true
		}
	}
	return false
}

func (s *SiteReport) AllFindings() []Finding {
	if s == nil {
		return nil
	}
	out := make([]Finding, 0, len(s.Findings))
	for _, r := range s.Reports {
		out = append(out, r.Findings...)
	}
	out = append(out, s.Findings...)
	slices.SortStableFunc(out, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.URL, b.URL),
			cmp.Compare(a.Rule, b.Rule),
			cmp.Compare(a.Level, b.Level),
		)
	})
	return out
}

func (s *SiteReport) ErrorCount() int {
	n := 0
	for _, f := range s.AllFindings() {
		if f.Level == LevelError {
			n++
		}
	}
	return n
}

func (s *SiteReport) WarnCount() int { return len(s.AllFindings()) - s.ErrorCount() }

func (s *SiteReport) Healthy() int {
	n := 0
	for _, r := range s.Reports {
		if r.OK() {
			n++
		}
	}
	return n
}
