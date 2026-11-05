package pagehttp

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var controllerTranslatePattern = regexp.MustCompile(`adminui\.MsgText\(|adminui\.TranslateArgs\(|shell\.TranslateFor\(`)

func TestControllerI18nBoundary(t *testing.T) {
	// 控制器取词已全部收敛：page_jump.go 改走共享出口后不再有成品串。
	allow := map[string]string{}

	hits := scanControllerTranslate(t)
	for file, lines := range hits {
		if _, ok := allow[file]; !ok {
			t.Errorf("%s 出现控制器取词（第 %s 行）—— 控制器只交事实、取词在模板；"+
				"确属例外就登记进 allow 并写明属于哪一类", file, strings.Join(lines, ", "))
		}
	}
	for file, reason := range allow {
		if _, ok := hits[file]; !ok {
			t.Errorf("allow 里的 %s 已不再命中控制器取词（改完了就从清单删掉）", file)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allow 里的 %s 缺理由", file)
		}
	}
}

func scanControllerTranslate(t *testing.T) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读目录失败: %v", err)
	}
	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("读 %s 失败: %v", name, rerr)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)

			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if controllerTranslatePattern.MatchString(line) {
				out[name] = append(out[name], strconv.Itoa(i+1))
			}
		}
	}
	return out
}
