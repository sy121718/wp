package scoring

import (
	"cmp"
	"slices"
	"strings"
)

type LinkCandidate struct {
	ID           string
	Title        string
	URL          string
	FocusKeyword string
	Tags         []string
	Categories   []string
}

type LinkDocument struct {
	SelfID       string
	SelfURL      string
	FocusKeyword string
	Tags         []string
	Categories   []string
	LinkedURLs   map[string]bool
}

type LinkSuggestion struct {
	ID     string
	Title  string
	URL    string
	Score  int
	Reason string
}

const (
	linkScoreKeyword  = 3
	linkScoreTag      = 2
	linkScoreCategory = 1
)

func InternalLinkSuggestions(doc *LinkDocument, candidates []LinkCandidate, limit int) []LinkSuggestion {
	if doc == nil || len(candidates) == 0 {
		return nil
	}
	keyword := strings.ToLower(strings.TrimSpace(doc.FocusKeyword))
	linked := doc.LinkedURLs
	selfURL := strings.TrimSpace(doc.SelfURL)

	out := make([]LinkSuggestion, 0, len(candidates))
	for _, cand := range candidates {
		cand.URL = strings.TrimSpace(cand.URL)
		if cand.URL == "" {
			continue
		}
		if strings.TrimSpace(cand.ID) != "" && cand.ID == doc.SelfID {
			continue
		}
		if selfURL != "" && cand.URL == selfURL {
			continue
		}
		if linked[cand.URL] {
			continue
		}
		score, reason := linkOverlapScore(keyword, doc.Tags, doc.Categories, cand)
		out = append(out, LinkSuggestion{
			ID: cand.ID, Title: cand.Title, URL: cand.URL,
			Score: score, Reason: reason,
		})
	}
	slices.SortStableFunc(out, func(a, b LinkSuggestion) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.Title, b.Title))
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func linkOverlapScore(keyword string, docTags, docCategories []string, cand LinkCandidate) (score int, reason string) {
	var parts []string
	candKeyword := strings.ToLower(strings.TrimSpace(cand.FocusKeyword))
	if keyword != "" && candKeyword != "" && (keyword == candKeyword || strings.Contains(keyword, candKeyword) || strings.Contains(candKeyword, keyword)) {
		score += linkScoreKeyword
		parts = append(parts, "关键词匹配")
	}
	if n := overlapCount(docTags, cand.Tags); n > 0 {
		score += n * linkScoreTag
		parts = append(parts, "标签重叠")
	}
	if n := overlapCount(docCategories, cand.Categories); n > 0 {
		score += n * linkScoreCategory
		parts = append(parts, "分类重叠")
	}
	return score, strings.Join(parts, "、")
}

func overlapCount(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]bool, len(b))
	for _, v := range b {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			set[v] = true
		}
	}
	n := 0
	for _, v := range a {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && set[v] {
			n++
		}
	}
	return n
}
