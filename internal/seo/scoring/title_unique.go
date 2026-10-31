package scoring

import (
	"cmp"
	"slices"
	"strings"
)

type TitleEntry struct {
	Title string

	Kind string
	Page string
	ID   string
}

func DuplicateTitles(target string, entries []TitleEntry, selfID string) []TitleEntry {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []TitleEntry
	for _, e := range entries {
		if selfID != "" && e.ID != "" && e.ID == selfID {
			continue
		}
		if strings.TrimSpace(e.Title) != target {
			continue
		}
		page := strings.TrimSpace(e.Page)
		if page == "" {
			page = strings.TrimSpace(e.ID)
		}

		if page == "" || seen[page] {
			continue
		}
		seen[page] = true
		out = append(out, TitleEntry{Title: target, Kind: e.Kind, Page: page, ID: e.ID})
	}
	slices.SortFunc(out, func(a, b TitleEntry) int { return cmp.Compare(a.Page, b.Page) })
	return out
}

func DuplicateTitleMessage(title string, dups []TitleEntry) string {
	if len(dups) == 0 {
		return ""
	}
	pages := make([]string, 0, len(dups))
	for _, d := range dups {
		pages = append(pages, d.Page)
	}
	return "重复的 title（" + strings.TrimSpace(title) + "）出现在 " + itoa(len(pages)) + " 个页面：" + strings.Join(pages, "、")
}
