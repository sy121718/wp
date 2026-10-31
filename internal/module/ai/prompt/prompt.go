package prompt

import (
	"cmp"
	"embed"
	"slices"
	"sort"
	"strings"

	_ "embed"
)

//go:embed site_rules.md
var siteRules string

//go:embed manual/*.md
var manuals embed.FS

func ManualCatalog() string {
	entries := ManualTopics()
	if len(entries) == 0 {
		return ""
	}
	names := make([]string, 0, len(entries))
	entries2 := make(map[string]string, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
		entries2[e.Name] = e.Title
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("可用手册（需要其中某个领域的口径细节时，用 guide 工具按名字取全文）：\n")
	for _, n := range names {
		b.WriteString("- ")
		b.WriteString(n)
		b.WriteString("：")
		b.WriteString(entries2[n])
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

type ManualTopic struct {
	Name string

	Title string
}

func ManualTopics() []ManualTopic {
	dir, err := manuals.ReadDir("manual")
	if err != nil {
		return nil
	}
	out := make([]ManualTopic, 0, len(dir))
	for _, f := range dir {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(f.Name(), ".md")
		title := name
		if raw, err := manuals.ReadFile("manual/" + f.Name()); err == nil {
			title = firstHeading(string(raw), name)
		}
		out = append(out, ManualTopic{Name: name, Title: title})
	}
	slices.SortFunc(out, func(a, b ManualTopic) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func firstHeading(text, fallback string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}

		break
	}
	return fallback
}

func Manual(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\\.") {
		return "", false
	}
	raw, err := manuals.ReadFile("manual/" + name + ".md")
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func SiteRules() string {
	return siteRules
}
