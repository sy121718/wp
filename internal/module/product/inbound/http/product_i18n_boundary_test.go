package producthttp

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var controllerTranslatePattern = regexp.MustCompile(`response\.MsgText\(|response\.ArgsText\(|text\.MsgText\(|text\.TranslateArgs\(|shell\.TranslateFor\(`)

// TestControllerI18nBoundary 商品模块控制器取词边界（internal/module/CLAUDE.md §inbound/http）。
//
// 判据：控制器只交**文案事实**（`adminui.Msg` = `adminui.Msg{Key, Fallback, Args}`），
// 取词在模板里（`{{tr := .["t"]}}` / `{{tf := .["tf"]}}`）；片段一律经 `text.Inject`。
// 理由是二开：后台是 Go 渲染、词条在 sys_i18n 里，控制器里写「错误 → 文案」的映射，
// 等于加一门语言要回头改控制器（这是本模块 41 处取词收口时的起点）。
//
// 允许保留的只有下表这几处，各属四类例外之一。清单**只减不增**，两个方向都判失败：
//
//	· 新写一处控制器取词 → 失败（否则这条规则会在下一次并行改造里被悄悄加回来）；
//	· 登记项不再命中 → 也失败（登记表退化成注释就没人看它了，与 rawSQLDebtList 同一判据）。
func TestControllerI18nBoundary(t *testing.T) {
	allow := map[string]string{
		"product_router.go": "中间件：productTranslateMiddleware 把取词函数放进 Request Context 给 " +
			"service 用（service 要产出展示文案，语言只有 gin.Context 知道）",
		"product_score_handle.go": "评分等级文案：seoscore.ScoreGradeText 只接受 tr；空态文案是 service 给的 " +
			"key + 兜底事实，取值在 HTTP 层（scoreEmptyView）",
		"product_translations_handle.go": "两处视图槽成品串（抽屉 body / 标题）+" +
			"具名占位符 {n} 句式；0.3 随视图槽删除",
		"product_subject_handle.go": "复合句与视图槽：变体保存的 Tail 拼接、定价回执三段拼句、" +
			"来源标签字面量、面板 ErrorText；0.3 随视图槽删除",
	}

	hits := scanControllerTranslate(t)
	for file, lines := range hits {
		if _, ok := allow[file]; !ok {
			t.Errorf("%s 出现控制器取词（第 %s 行）—— 控制器只交事实、取词在模板（片段经 "+
				"text.Inject）；确属例外就登记进 allow 并写明属于哪一类",
				file, strings.Join(lines, ", "))
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

// scanControllerTranslate 扫本目录下非测试文件的控制器取词（跳过注释行），返回 文件 → 行号列表。
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
