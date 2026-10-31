package builder

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const animationNamespace = "sky-"

var (
	reKeyframeDef  = regexp.MustCompile(`@keyframes\s+([A-Za-z0-9_-]+)`)
	reAnimationRef = regexp.MustCompile(`animation(?:-name)?:\s*([^;}]+)`)
)

func verifyAnimationRefs(css string) error {
	defined := make(map[string]bool)
	for _, m := range reKeyframeDef.FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	var missing []string
	seen := make(map[string]bool)
	for _, m := range reAnimationRef.FindAllStringSubmatch(css, -1) {

		for _, seg := range strings.Split(m[1], ",") {
			for _, tok := range strings.Fields(seg) {
				if !strings.HasPrefix(tok, animationNamespace) {
					continue
				}
				if !defined[tok] && !seen[tok] {
					seen[tok] = true
					missing = append(missing, tok)
				}
				break
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("产物引用了未定义的关键帧: %s（动画名来自动效词汇表，对应关键帧应写在 core/keyframes/ 里）",
			strings.Join(missing, ", "))
	}

	return guardProductCSS(css)
}
