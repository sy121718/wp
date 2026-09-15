package templates

// admin_form_base_test.go — 后台表单控件外观收敛的契约（审计条目 UIK-009）。
//
// 口径复核（2026-09-14 实测，口径就是本文件里的 scanAdminFormControls）：
//   admin 下有可见表单控件的模板 50 个，控件 451 个（计入 input / select / textarea，
//   排除 hidden / checkbox / radio / submit / button / file / color / range）；
//   其中控件自身带基座类 395、仅靠桥接容器（.pages-form / .locale-add）兜住 1、真裸 55。
//   控件口径的覆盖率是 87.8%；审计条目写的是「10 个页面（21%）」—— 那是**页面**口径
//   （出现真裸控件的页面数 / 页面总数），两个口径都能复算，但页面清单对不齐：
//     · 审计点名而本次未复现：plugins.html 已无可见控件；mail_automation_run /
//       mail_campaign 在 admin 下没有同名模板（疑似指 mail_automation.html 与
//       mail_marketing.html，这两个实测全部带基座类）；
//     · 审计未点名而实测确实裸写：analytics.html、article_translations.html、i18n.html、
//       navigation_translations.html、partials/locale_rows.html 这 5 个。
//   结论：21% 不能当控件覆盖率引用（会高估缺口），按审计的页面清单去补也会漏 5 个文件。
//   另有口径噪声：本文件（Go x/net/html）与复核用的 Python HTMLParser 在 Jet 模板的
//   属性/注释写法上对个别文件的**控件总数**识别相差 1~5 个（如 mail_marketing 12 vs 14），
//   但两套实现对「真裸控件」的判定与 55 这个总数完全一致。
//
// 本文件钉三件事：
//   ① 兜底层存在、作用域与优先级都收紧（.admin-layout + :where），不外溢到产物与工作台；
//   ② 真裸控件清单只减不增 —— 新增裸控件即失败，这就是审计要求的检查脚本；
//   ③ login 页的例外如实记录：它不在 .admin-layout 内，且只引 theme.css 不引 ui.css。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// adminFormControlBareBaseline 真裸控件的文件级基线（既无基座类，也不在桥接容器内）。
// 只允许下降：这些数字每降一个，都说明有页面迁到了基座类，请同步把基线改小。
var adminFormControlBareBaseline = map[string]int{
	"analytics.html":               2,
	"article_translations.html":    3,
	"articles.html":                2,
	"blocks.html":                  4,
	"i18n.html":                    8,
	"login.html":                   3,
	"media.html":                   5,
	"navigation_translations.html": 2,
	"navigations.html":             9,
	"pages.html":                   4,
	"partials/locale_rows.html":    1,
	"settings.html":                10,
	"theme.html":                   2,
}

var adminFormBaseClasses = map[string]bool{
	"form-input": true, "form-select": true, "form-textarea": true, "wbs-native": true,
}

var adminFormBridgeContainers = map[string]bool{
	"pages-form": true, "attr-form-head": true, "locale-add": true,
}

// adminFormSkipTypes 这些控件的观感由各自控件族负责（.checkbox / 按钮 / 取色器等），
// 与「输入框裸写」不是同一个问题，排除后口径才干净。
var adminFormSkipTypes = map[string]bool{
	"hidden": true, "checkbox": true, "radio": true, "submit": true, "button": true,
	"file": true, "color": true, "range": true, "image": true, "reset": true,
}

// voidElements 不入祖先栈：它们没有闭合标签，入栈会让后面的 EndTag 误截断栈。
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
	"img": true, "input": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true,
}

type adminFormScan struct {
	total  int
	base   int
	bridge int
	bare   int
}

// adminOpenEl 开标签栈元素：判定控件是否落在桥接容器内需要祖先的 class 集合。
type adminOpenEl struct {
	name  string
	class map[string]bool
}

// scanAdminFormControls 按 DOM 上下文判定每个可见表单控件的样式来源。
// 只用标准库 tokenizer：Jet 模板片段（{{ }}）会被当作文本，不影响标签配对。
func scanAdminFormControls(src string) adminFormScan {
	var out adminFormScan
	var stack []adminOpenEl
	z := html.NewTokenizer(strings.NewReader(src))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out
		}
		if tt == html.EndTagToken {
			name, _ := z.TagName()
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name == string(name) {
					stack = stack[:i]
					break
				}
			}
			continue
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		name, _ := z.TagName()
		tag := string(name)
		class := map[string]bool{}
		typ := ""
		for {
			key, val, more := z.TagAttr()
			switch string(key) {
			case "class":
				for _, c := range strings.Fields(string(val)) {
					class[c] = true
				}
			case "type":
				typ = strings.ToLower(strings.TrimSpace(string(val)))
			}
			if !more {
				break
			}
		}
		if tag == "input" || tag == "select" || tag == "textarea" {
			if !adminFormSkipTypes[typ] {
				out.total++
				switch {
				case hasAnyClass(class, adminFormBaseClasses):
					out.base++
				case hasAnyClass(class, adminFormBridgeContainers) || stackHasAnyClass(stack, adminFormBridgeContainers):
					out.bridge++
				default:
					out.bare++
				}
			}
			continue
		}
		if tt == html.StartTagToken && !voidElements[tag] {
			stack = append(stack, adminOpenEl{name: tag, class: class})
		}
	}
}

func hasAnyClass(class map[string]bool, want map[string]bool) bool {
	for c := range class {
		if want[c] {
			return true
		}
	}
	return false
}

func stackHasAnyClass(stack []adminOpenEl, want map[string]bool) bool {
	for _, el := range stack {
		if hasAnyClass(el.class, want) {
			return true
		}
	}
	return false
}

// TestAdminFormControlBaseCoverageBaseline 真裸控件只减不增。
func TestAdminFormControlBaseCoverageBaseline(t *testing.T) {
	actual := map[string]int{}
	total, base, bridge, bare := 0, 0, 0, 0
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				walk(path)
				continue
			}
			if !strings.HasSuffix(entry.Name(), ".html") || entry.Name() == "layout.html" {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			s := scanAdminFormControls(string(src))
			total += s.total
			base += s.base
			bridge += s.bridge
			bare += s.bare
			if s.bare > 0 {
				actual[strings.TrimPrefix(path, "admin"+string(filepath.Separator))] = s.bare
			}
			if s.total > 0 {
				t.Logf("  %-34s total=%d base=%d bridge=%d bare=%d",
					strings.TrimPrefix(path, "admin"+string(filepath.Separator)), s.total, s.base, s.bridge, s.bare)
			}
		}
	}
	walk("admin")
	t.Logf("后台可见表单控件 %d：基座类 %d / 桥接容器 %d / 真裸 %d（覆盖率 %.1f%%）",
		total, base, bridge, bare, float64(base+bridge)*100/float64(total))

	var grew []string
	for file, n := range actual {
		want, ok := adminFormControlBareBaseline[file]
		if !ok {
			grew = append(grew, fmt.Sprintf("%s: %d（基线里没有这个文件）", file, n))
			continue
		}
		if n > want {
			grew = append(grew, fmt.Sprintf("%s: %d > 基线 %d", file, n, want))
		}
	}
	sort.Strings(grew)
	if len(grew) > 0 {
		t.Errorf("出现新的裸表单控件（既不进基座也不进桥接）：%s；修法：给控件加 class=form-input / form-select / form-textarea", strings.Join(grew, "; "))
	}
	for file, want := range adminFormControlBareBaseline {
		if actual[file] < want {
			t.Logf("收敛进展：%s 的裸控件 %d → %d，请把基线改小", file, want, actual[file])
		}
	}
}
