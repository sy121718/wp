package css

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

func readTheme(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("theme.css")
	if err != nil {
		t.Fatalf("读 theme.css 失败: %v", err)
	}
	return string(b)
}

func TestTokenFamiliesExist(t *testing.T) {
	src := readTheme(t)
	families := map[string]*regexp.Regexp{
		"间距 --space-*":  regexp.MustCompile(`--space-(\d+)\s*:\s*([0-9.]+)(px|rem)`),
		"圆角 --radius-*": regexp.MustCompile(`--radius-([a-z]+)\s*:\s*([0-9.]+)(px|rem)`),
		"字号 --text-*":   regexp.MustCompile(`--text-([a-z0-9]+)\s*:\s*([0-9.]+)(px|rem)`),
		"动效 --motion-*": regexp.MustCompile(`--motion-([a-z]+)\s*:\s*([0-9.]+)(ms|s)`),
	}
	for label, re := range families {
		matches := re.FindAllStringSubmatch(src, -1)
		if len(matches) < 3 {
			t.Errorf("%s 只定义了 %d 档（至少 3 档才算一套档位）：%v", label, len(matches), matches)
		}
	}
}

func TestSpaceScaleIsMonotonic(t *testing.T) {
	src := readTheme(t)
	re := regexp.MustCompile(`--space-(\d+)\s*:\s*([0-9.]+)(px|rem)`)
	type step struct{ index, value int }
	var steps []step
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		idx, _ := strconv.Atoi(m[1])
		val, _ := strconv.ParseFloat(m[2], 64)
		if m[3] == "rem" {
			val *= 16
		}
		steps = append(steps, step{idx, int(val)})
	}
	if len(steps) < 3 {
		t.Fatalf("间距档位不足: %v", steps)
	}
	for i := 1; i < len(steps); i++ {
		if steps[i].index <= steps[i-1].index {
			t.Errorf("档位号没有递增：--space-%d 在 --space-%d 之后", steps[i].index, steps[i-1].index)
		}
		if steps[i].value <= steps[i-1].value {
			t.Errorf("档位 --space-%d(%dpx) 不小于 --space-%d(%dpx) —— 档位失去单调性就没法推理",
				steps[i].index, steps[i].value, steps[i-1].index, steps[i-1].value)
		}
	}
}

func TestTokenValuesAreNotDuplicated(t *testing.T) {
	src := readTheme(t)
	for label, re := range map[string]*regexp.Regexp{
		"间距": regexp.MustCompile(`--space-\d+\s*:\s*([0-9.]+)(px|rem)`),
		"圆角": regexp.MustCompile(`--radius-[a-z]+\s*:\s*([0-9.]+)(px|rem)`),
		"字号": regexp.MustCompile(`--text-[a-z0-9]+\s*:\s*([0-9.]+)(px|rem)`),
	} {
		seen := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			key := m[1] + m[2]
			if seen[key] {
				t.Errorf("%s 族里有两个档位都是 %s —— 重复档位等于没分档", label, key)
			}
			seen[key] = true
		}
	}
}

func TestTokensDeclaredForBothThemes(t *testing.T) {
	src := readTheme(t)

	anchor := regexp.MustCompile(`\[data-theme="light"\],\s*\[data-theme="dark"\]\s*\{[^}]*--space-1`)
	if !anchor.MatchString(src) {
		t.Error("档位令牌没有同时声明给亮色与暗色主题 —— 暗色主题下会取不到值（回退成无效声明）")
	}
}
