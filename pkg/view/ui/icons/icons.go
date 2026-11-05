package icons

import (
	"embed"
	"sort"
	"strings"
)

//go:embed svg/*.svg
var iconsFS embed.FS

var builtinIconSet = loadBuiltinIcons()

func loadBuiltinIcons() map[string]string {
	set := make(map[string]string)
	entries, err := iconsFS.ReadDir("svg")
	if err != nil {
		return set
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		data, err := iconsFS.ReadFile("svg/" + e.Name())
		if err != nil {
			continue
		}
		set[strings.TrimSuffix(e.Name(), ".svg")] = strings.TrimSpace(string(data))
	}
	return set
}

func IconSVG(name string) (string, bool) {
	svg, ok := builtinIconSet[name]
	return svg, ok
}

func IconSVGClass(name, class string) (string, bool) {
	svg, ok := builtinIconSet[name]
	if !ok {
		return "", false
	}
	if class == "" {
		return svg, true
	}

	return `<svg class="` + class + `" aria-hidden="true"` + strings.TrimPrefix(svg, "<svg"), true
}

func IconInner(name string) (string, bool) {
	svg, ok := builtinIconSet[name]
	if !ok {
		return "", false
	}
	start := strings.Index(svg, ">")
	end := strings.LastIndex(svg, "</svg>")
	if start < 0 || end <= start {
		return "", false
	}
	return strings.TrimSpace(svg[start+1 : end]), true
}

func IconNames() []string {
	names := make([]string, 0, len(builtinIconSet))
	for name := range builtinIconSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IconNames 返回全部图标名（已排序）。
