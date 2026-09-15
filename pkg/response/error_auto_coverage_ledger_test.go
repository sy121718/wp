package response

// error_auto_coverage_ledger_test.go — ErrorAuto 消费者的状态码断言覆盖率账本。
//
// 背景：CQ-010 把 170 处错误出口换成 ErrorAuto 之后，判据在一类输入上漏档时**不会让任何既有
// 测试变红** —— 那个模块的业务错误会静默变成「500 + 通用文案」。这个缺口能长期存活的原因很
// 具体：大部分模块的测试根本没断言过错误响应的状态码（只有 masterdata 与 inventory 断言过）。
//
// 所以这里把覆盖率做成账本：基线是「已知没有状态码断言的模块」，只减不增。
// 新增一个用 ErrorAuto 却不断言状态码的模块会直接变红；基线里已经补上断言的模块也必须从基线
// 删掉（过期项会掩盖「其实已经修好了」这个事实）。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// stateAssertionGapBaseline 已知没有状态码断言的模块（只减不增）。
//
// 修法不是往这里加名字，而是给该模块补一条「业务错误 400 / 内部错误 500」的断言 ——
// 参考 public/test/masterdata/feature 与 public/test/media/feature 的写法。
var stateAssertionGapBaseline = map[string]bool{
	"blueprint":       true,
	"build":           true,
	"content":         true,
	"contenttemplate": true,
	"mail":            true,
	"plugin":          true,
	"presentation":    true,
	"user":            true,
}

// statusAssertionMarkers 判定「测试断言过错误响应的状态码」的标记。
// 兼容字面量写法（media 用的是 w.Code != 400）与 http 常量写法。
var statusAssertionMarkers = []string{
	"StatusBadRequest", "StatusInternalServerError",
	"!= 400", "!= 500", "== 400", "== 500",
}

// TestErrorAutoConsumersHaveStatusCodeAssertions 用 ErrorAuto 的模块必须有状态码断言，缺口只减不增。
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

// dirHasStatusAssertion 目录下是否至少有一个测试文件断言过错误状态码。
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
