package templates

// admin_form_base_test.go — 后台表单控件外观收敛的契约（审计条目 UIK-009，2026-10-01 收尾）。
//
// 现在的口径只有一条：**每个可见表单控件都必须自带基座类**（form-input / form-select /
// form-textarea / wbs-native），因为外观只有一个真源 —— 基座类。
// 实测 594 个控件（计入 input / select / textarea，排除 hidden / checkbox / radio /
// submit / button / file / color / range）全部达标，裸写 0。
//
// 收敛过程中两样东西先后消失，本文件不再保留它们的痕迹：
//   · 容器桥接（.pages-form / .attr-form-head / .locale-add）：靠祖先容器给控件上样式，
//     「换个地方用就掉样式」，也把真源变成两份；
//   · 后台兜底层（.admin-layout :where(input…, select, textarea)，审计 UIK-009 的产物）：
//     它想兜住的 55 个裸控件（2026-09-14 实测，分布在 analytics / i18n / navigations /
//     settings / media / login 等 13 个文件）已全部迁到基座类，而 login 那 3 个从不在它的
//     作用域内（不在 .admin-layout、也只引 theme.css）—— 零消费者。
//     留着一层「容器可以强制给外观」的依赖，代价是每次改基座都要重新判断谁赢。
//
// 本文件钉一件事：**裸写即失败**。新增一个没带基座类的输入框，就是新增一份浏览器默认外观 ——
// 没有别的东西会把它补上（兜底层已删）。
// 登录页现在同样走基座：它引了 ui.css，3 个输入框都带 form-input，只保留本页的尺寸差异。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// 2026-10-01：真裸控件清零，文件级基线表随之删除 —— 现在没有「允许存在的裸控件」这回事，
// 测试直接断言「必须为 0」（见 TestAdminFormControlBaseCoverageAllMigrated）。
// 曾经的 55 个分布在 13 个文件（analytics / i18n / navigations / settings / media / login …），
// 它们不是靠容器兜底补齐的，而是各自带上了基座类；容器规则只留布局差异。
// 桥接容器（.pages-form / .attr-form-head / .locale-add）同时退役：判定只看控件自己有没有基座类。

var adminFormBaseClasses = map[string]bool{
	"form-input": true, "form-select": true, "form-textarea": true, "wbs-native": true,
}

// adminFormSkipTypes 这些控件的观感由各自控件族负责（.checkbox / 按钮 / 取色器等），
// 与「输入框裸写」不是同一个问题，排除后口径才干净。
var adminFormSkipTypes = map[string]bool{
	"hidden": true, "checkbox": true, "radio": true, "submit": true, "button": true,
	"file": true, "color": true, "range": true, "image": true, "reset": true,
}

type adminFormScan struct {
	total int
	base  int
	bare  int
}

// scanAdminFormControls 判定每个可见表单控件有没有基座类。
// 只用标准库 tokenizer：Jet 模板片段（{{ }}）会被当作文本，不影响标签配对。
// 桥接容器退役后不再需要祖先栈 —— 判定只看控件自己的 class。
func scanAdminFormControls(src string) adminFormScan {
	var out adminFormScan
	z := html.NewTokenizer(strings.NewReader(src))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out
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
				if hasAnyClass(class, adminFormBaseClasses) {
					out.base++
				} else {
					out.bare++
				}
			}
			continue
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

// TestAdminFormControlBaseCoverageAllMigrated 后台每个可见表单控件都自带基座类。
//
// 这曾经是一张「只减不增」的基线表（55 个真裸控件 / 13 个文件）。2026-10-01 全部迁完，
// 断言随之收紧为「必须为 0」：基座里那层 .admin-layout :where(…) 兜底已经删掉，
// 控件外观只有「基座类」一个真源 —— 新增一个裸写的输入框就是新增一份浏览器默认外观，
// 没有别的东西会把它补上。
func TestAdminFormControlBaseCoverageAllMigrated(t *testing.T) {
	total, base, bare := 0, 0, 0
	var offending []string
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
			bare += s.bare
			if s.bare > 0 {
				offending = append(offending,
					fmt.Sprintf("%s: %d 个", strings.TrimPrefix(path, "admin"+string(filepath.Separator)), s.bare))
			}
			if s.total > 0 {
				t.Logf("  %-34s total=%d base=%d bare=%d",
					strings.TrimPrefix(path, "admin"+string(filepath.Separator)), s.total, s.base, s.bare)
			}
		}
	}
	walk("admin")
	t.Logf("后台可见表单控件 %d：带基座类 %d / 裸写 %d", total, base, bare)

	sort.Strings(offending)
	if len(offending) > 0 {
		t.Errorf("出现裸写的表单控件（没有任何基座类，会掉回浏览器默认外观）：%s；"+
			"修法：给控件加 class=form-input / form-select / form-textarea —— "+
			"容器不再提供兜底外观（.admin-layout :where(…) 已删）", strings.Join(offending, "; "))
	}
}
