package compliance

// site.go — 跨产物的互指校验（同一份巡检集合内部）。
//
// 为什么只能在「集合内」判：hreflang 的正确性是两个页面之间的事实（A 声明 en 版本是 B，
// B 就必须声明 zh 版本是 A）。只拿到一份产物时，指向外部的那一侧根本无从判断 ——
// 报告把「按激活清单抓取校验」归到发布后那一段（本轮不做），本文件只回答
// 「两份产物都在手上、且都声称互指对方」这一种能当场证伪的情况，不猜、不误报。

import (
	"sort"
	"strings"

	"go_wp/internal/seo"
)

// SiteReport 一次全站巡检的结论。
type SiteReport struct {
	// Reports 逐产物的结论，按 URL 升序（确定性输出）。
	Reports []*Report `json:"reports"`
	// Findings 跨产物的结论（互指不成立 / 互指目标语言与事实不符），按 URL、规则升序。
	Findings []Finding `json:"findings"`
	// Checked 巡检的产物份数。
	Checked int `json:"checked"`
	// Checks 每份产物上评估的规则条目数（含跨产物两项）。
	Checks int `json:"checks"`
}

// InspectSite 对一组产物做确定性校验：逐产物规则 + 跨产物互指规则。
//
// 入参顺序不影响结论（内部按 URL 排序后处理）；同一组产物重复调用得到同一份结论。
func InspectSite(arts []Artifact) *SiteReport {
	in := make([]Artifact, len(arts))
	copy(in, arts)
	sort.SliceStable(in, func(i, j int) bool { return in[i].URL < in[j].URL })

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
				continue // x-default 指向默认语言版本，它的语言码是「兜底」语义，不参与语言比对
			}
			idx, ok := byPath[seo.CanonicalPublicPath(pathOf(e.Href))]
			if !ok {
				// 目标不在本次巡检集合里：可能未发布、也可能压根不存在。
				// 这一条属于「按激活清单抓取校验」的范畴（报告后半段，本轮不做），
				// 在这里下结论只会产生误报。
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
	sort.SliceStable(cross, func(i, j int) bool {
		if cross[i].URL != cross[j].URL {
			return cross[i].URL < cross[j].URL
		}
		return cross[i].Rule < cross[j].Rule
	})
	rep.Findings = cross
	return rep
}

// declaresBack 目标产物是否回指了声明方的地址与语言。
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

// AllFindings 汇总结论：逐产物的 + 跨产物的，按 URL、规则升序（巡检报告的一维清单）。
func (s *SiteReport) AllFindings() []Finding {
	if s == nil {
		return nil
	}
	out := make([]Finding, 0, len(s.Findings))
	for _, r := range s.Reports {
		out = append(out, r.Findings...)
	}
	out = append(out, s.Findings...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].URL != out[j].URL {
			return out[i].URL < out[j].URL
		}
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		return out[i].Level < out[j].Level
	})
	return out
}

// ErrorCount / WarnCount / Healthy 巡检汇总计数。
func (s *SiteReport) ErrorCount() int {
	n := 0
	for _, f := range s.AllFindings() {
		if f.Level == LevelError {
			n++
		}
	}
	return n
}

// WarnCount 非阻断级结论数。
func (s *SiteReport) WarnCount() int { return len(s.AllFindings()) - s.ErrorCount() }

// Healthy 没有 error 级结论的产物份数。
func (s *SiteReport) Healthy() int {
	n := 0
	for _, r := range s.Reports {
		if r.OK() {
			n++
		}
	}
	return n
}
