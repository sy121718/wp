package css

// tokens_contract_test.go — 档位令牌层的存在性与覆盖面契约。
//
// 为什么这条值得测：theme.css 原本有颜色(46) + 阴影(3) + 字体(2)，**没有**间距、
// 圆角、字号档位。结果是同一类值在各处手写（border-radius 6px 出现 60 次、
// gap 8px 66 次），改一次视觉密度要动几十个文件，而没人能说出「一共用了几档」。
//
// 判据是「档位存在且彼此不重复」而不是「有多少处用了令牌」：后者是第三批的
// 收敛目标（要视觉基线），而**先把档位定下来**是它的前置条件 —— 没有档位，
// 收敛就是无参照地乱改。

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

// TestTokenFamiliesExist 四个档位族必须都在（间距/圆角/字号/动效）。
func TestTokenFamiliesExist(t *testing.T) {
	src := readTheme(t)
	families := map[string]*regexp.Regexp{
		"间距 --space-*":   regexp.MustCompile(`--space-(\d+)\s*:\s*([0-9.]+)(px|rem)`),
		"圆角 --radius-*":  regexp.MustCompile(`--radius-([a-z]+)\s*:\s*([0-9.]+)(px|rem)`),
		"字号 --text-*":    regexp.MustCompile(`--text-([a-z0-9]+)\s*:\s*([0-9.]+)(px|rem)`),
		"动效 --motion-*":  regexp.MustCompile(`--motion-([a-z]+)\s*:\s*([0-9.]+)(ms|s)`),
	}
	for label, re := range families {
		matches := re.FindAllStringSubmatch(src, -1)
		if len(matches) < 3 {
			t.Errorf("%s 只定义了 %d 档（至少 3 档才算一套档位）：%v", label, len(matches), matches)
		}
	}
}

// TestSpaceScaleIsMonotonic 间距档位必须严格递增，且档位号与数值同向。
//
// 判据取「递增」而不是「是 4 的倍数」：现有 codebase 大量使用 6px / 10px
// （不是 4 的倍数），把档位改成纯 8pt 会改变视觉。这里要的是**有序可用**，
// 不是美学洁癖 —— 后者要视觉基线，属于第三批。
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

// TestTokenValuesAreNotDuplicated 同一族内不许两个档位是同一个值（那样档位没有意义）。
func TestTokenValuesAreNotDuplicated(t *testing.T) {
	src := readTheme(t)
	for label, re := range map[string]*regexp.Regexp{
		"间距":  regexp.MustCompile(`--space-\d+\s*:\s*([0-9.]+)(px|rem)`),
		"圆角":  regexp.MustCompile(`--radius-[a-z]+\s*:\s*([0-9.]+)(px|rem)`),
		"字号":  regexp.MustCompile(`--text-[a-z0-9]+\s*:\s*([0-9.]+)(px|rem)`),
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

// TestTokensDeclaredForBothThemes 档位必须在两个主题下都可见。
//
// 颜色令牌是分主题的（亮/暗各一套），而间距/圆角/字号/时长**不分主题**：
// 把它们写在 [data-theme="light"] 里，暗色主题的元素就取不到值。
func TestTokensDeclaredForBothThemes(t *testing.T) {
	src := readTheme(t)
	// 档位块的选择器必须是「亮 + 暗」都覆盖的那一条。
	anchor := regexp.MustCompile(`\[data-theme="light"\],\s*\[data-theme="dark"\]\s*\{[^}]*--space-1`)
	if !anchor.MatchString(src) {
		t.Error("档位令牌没有同时声明给亮色与暗色主题 —— 暗色主题下会取不到值（回退成无效声明）")
	}
}
