package pagehttp

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var controllerTranslatePattern = regexp.MustCompile(`adminui\.MsgText\(|adminui\.TranslateArgs\(|shell\.TranslateFor\(`)

// TestControllerI18nBoundary 页面模块控制器取词边界（internal/module/CLAUDE.md §inbound/http）。
//
// 判据：控制器只交**文案事实**（`adminui.Msg{Key, Fallback, Args}`），取词在模板里 ——
// 后台是 Go 渲染、词条在库里，控制器里写「错误 → 文案」的映射等于加一门语言要回头改控制器。
// 片段一律走 `adminui.PrepareBare`，手工塞 `"t": shell.TranslateFor(c)` 是旧写法。
//
// 允许保留的只有下表这几处（各属于四类例外之一）。清单**只减不增**，两个方向都判失败：
//
//	· 新写一处控制器取词 → 失败（否则这条规则会在下一次并行改造里被悄悄加回来）；
//	· 登记项不再命中 → 也失败（登记表退化成注释就没人看它了，与 rawSQLDebtList 同一判据）。
func TestControllerI18nBoundary(t *testing.T) {
	allow := map[string]string{
		"pages_handle.go":  "JSON 出口成品串：错误事实的 Tail 复合形态（多段拼接）",
		"page_jump.go":     "提示页出口（RenderJump 那条链要成品串）+ Tail 复合形态",
		"page_schedule.go": "JSON 出口成品串：定时保存失败原因要嵌进响应体的整句",
	}

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

// scanControllerTranslate 扫本目录下非测试文件的控制器取词，返回 文件 → 行号列表。
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
			// 注释里会**解释**这条例外（"曾经由控制器填 shell.TranslateFor(c)"），不是取词点。
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
