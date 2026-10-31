package response

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var stateAssertionGapBaseline = map[string]bool{
	"blueprint":    true,
	"build":        true,
	"presentation": true,
}

var statusAssertionMarkers = []string{
	"StatusBadRequest", "StatusInternalServerError",
	"!= 400", "!= 500", "== 400", "== 500",
}

func TestErrorAutoConsumersHaveStatusCodeAssertions(t *testing.T) {
	const moduleRoot = "../../internal/module"
	const testRoot = "../../public/test"

	consumers := map[string]bool{}
	walkErr := filepath.Walk(moduleRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		slashed := filepath.ToSlash(path)
		if !strings.HasSuffix(slashed, ".go") || strings.HasSuffix(slashed, "_test.go") {
			return nil
		}
		if !strings.Contains(slashed, "/inbound/http/") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil || !strings.Contains(string(raw), "response.ErrorAuto") {
			return nil
		}
		rel := strings.TrimPrefix(slashed, moduleRoot+"/")
		consumers[strings.SplitN(rel, "/", 2)[0]] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("遍历模块目录失败: %v", walkErr)
	}
	if len(consumers) == 0 {
		t.Fatal("没有扫到任何 ErrorAuto 消费者，路径写错了（测试会变成空转）")
	}

	var gaps []string
	for mod := range consumers {
		dir := filepath.Join(testRoot, mod)
		if _, statErr := os.Stat(dir); statErr != nil {
			gaps = append(gaps, mod)
			continue
		}
		if !dirHasStatusAssertion(dir) {
			gaps = append(gaps, mod)
		}
	}
	sort.Strings(gaps)
	t.Logf("用了 ErrorAuto 的模块 %d 个，其中没有状态码断言的 %d 个：%v", len(consumers), len(gaps), gaps)

	seen := map[string]bool{}
	for _, mod := range gaps {
		seen[mod] = true
		if !stateAssertionGapBaseline[mod] {
			t.Errorf("模块 %s 用了 response.ErrorAuto 但测试里没有任何错误状态码断言："+
				"它的业务错误被判成内部错误时不会有测试变红。请补一条断言（参考 public/test/media/feature），"+
				"而不是往基线里加名字", mod)
		}
	}
	for mod := range stateAssertionGapBaseline {
		if !seen[mod] {
			t.Errorf("基线里的 %s 已经不在缺口里（已有断言或已不再使用 ErrorAuto），请从 stateAssertionGapBaseline 删掉", mod)
		}
	}
}

func dirHasStatusAssertion(dir string) bool {
	found := false
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		src := string(raw)
		for _, marker := range statusAssertionMarkers {
			if strings.Contains(src, marker) {
				found = true
				return nil
			}
		}
		return nil
	})
	return found
}
