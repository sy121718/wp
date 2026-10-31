package templates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

var adminFormBaseClasses = map[string]bool{
	"form-input": true, "form-select": true, "form-textarea": true, "wbs-native": true,
}

var adminFormSkipTypes = map[string]bool{
	"hidden": true, "checkbox": true, "radio": true, "submit": true, "button": true,
	"file": true, "color": true, "range": true, "image": true, "reset": true,
}

type adminFormScan struct {
	total int
	base  int
	bare  int
}

func scanAdminFormControls(src string) adminFormScan {
	var out adminFormScan
	z := html.NewTokenizer(strings.NewReader(stripJetComments(src)))
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
