package pagehttp

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var viewShapePattern = regexp.MustCompile(`^type \w+(View|Row|Item) struct`)

func TestViewShapesLiveInDTO(t *testing.T) {

	allow := map[string]string{}

	hits := scanViewShapes(t)
	for file, lines := range hits {
		if _, ok := allow[file]; !ok {
			t.Errorf("%s 声明了渲染行形状（第 %s 行）—— 纯数据视图放 internal/module/page/dto/page_view.go，"+
				"确属例外就登记进 allow 并写明理由", file, strings.Join(lines, ", "))
		}
	}
	for file, reason := range allow {
		if _, ok := hits[file]; !ok {
			t.Errorf("allow 里的 %s 已不再声明行形状（搬完了就从清单删掉）", file)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("allow 里的 %s 缺理由", file)
		}
	}
}

func scanViewShapes(t *testing.T) map[string][]string {
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
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if viewShapePattern.MatchString(line) {
				out[name] = append(out[name], strconv.Itoa(i+1))
			}
		}
	}
	return out
}
