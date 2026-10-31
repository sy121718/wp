package builder

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var i18nExemptions = map[string]string{}

var i18nPending = map[string]string{}

var (
	cjkRe         = regexp.MustCompile(`[\p{Han}]`)
	jetCommentRe  = regexp.MustCompile(`(?s)\{\*.*?\*\}`)
	htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)
	applyI18nRe   = regexp.MustCompile(`func \([^)]*\) ApplyI18n\(`)
)

func visibleText(tpl string) string {
	tpl = jetCommentRe.ReplaceAllString(tpl, "")
	return htmlCommentRe.ReplaceAllString(tpl, "")
}

func hasVisibleCJK(tpl string) bool {
	return cjkRe.MatchString(visibleText(tpl))
}

func TestComponentI18nScanLogic(t *testing.T) {
	cases := []struct {
		name string
		tpl  string
		want bool
	}{
		{"可见中文命中", `<button class="btn">提交</button>`, true},
		{"中文属性值也命中", `<input placeholder="请输入关键词">`, true},
		{"纯英文不命中", `<button class="btn">Submit</button>`, false},
		{"Jet 注释里的中文不命中", `{* 提交按钮：说明 *}<span>Submit</span>`, false},
		{"HTML 注释里的中文不命中", `<!-- 提交按钮 --><span>Submit</span>`, false},
		{"多行 Jet 注释里的中文不命中", "{*\n第一行说明\n第二行说明\n*}\n<p>ok</p>", false},
		{"注释之外有中文则命中", "{* 注释 *}<p>确定</p>", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasVisibleCJK(tc.tpl); got != tc.want {
				t.Fatalf("hasVisibleCJK(%q) = %v，期望 %v", tc.tpl, got, tc.want)
			}
		})
	}
}

func TestComponentI18nDeclarations(t *testing.T) {
	entries, err := os.ReadDir("components")
	if err != nil {
		t.Fatalf("读取组件目录失败: %v", err)
	}
	var checked, implemented, gaps []string

	var dirCount, jetCount int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirCount++
		dir := filepath.Join("components", e.Name())
		files, derr := os.ReadDir(dir)
		if derr != nil {
			continue
		}
		var hasText, hasI18n bool
		for _, f := range files {
			data, rerr := os.ReadFile(filepath.Join(dir, f.Name()))
			if rerr != nil {
				continue
			}
			switch {
			case strings.HasSuffix(f.Name(), ".jet"):
				jetCount++
				if hasVisibleCJK(string(data)) {
					hasText = true
				}
			case strings.HasSuffix(f.Name(), ".go"):
				if applyI18nRe.MatchString(string(data)) {
					hasI18n = true
				}
			}
		}
		if !hasText {
			continue
		}
		checked = append(checked, e.Name())
		if hasI18n {
			implemented = append(implemented, e.Name())
			continue
		}
		if _, ok := i18nExemptions[e.Name()]; ok {
			continue
		}
		if _, ok := i18nPending[e.Name()]; ok {
			continue
		}
		gaps = append(gaps, e.Name())
	}

	if dirCount == 0 || jetCount == 0 {
		t.Fatalf("组件扫描覆盖面为零：读到 %d 个组件目录、%d 个 .jet 模板（路径或工作目录变了）",
			dirCount, jetCount)
	}
	if len(checked) == 0 {

		t.Logf("全部 %d 个组件（%d 个 .jet 模板）均无访客可见中文 —— i18n key 化已完成，这是目标状态",
			dirCount, jetCount)
	}
	if len(gaps) > 0 {
		sort.Strings(gaps)
		for _, name := range gaps {
			t.Errorf("组件 %s 的模板里有访客可见的硬编码中文，但既没实现 I18nAware、也不在豁免/待办表里", name)
		}
		t.Fatalf("补齐 I18nAware（推荐）或登记到 i18nExemptions / i18nPending 并写明归属")
	}

	done := map[string]bool{}
	for _, name := range implemented {
		done[name] = true
	}
	withText := map[string]bool{}
	for _, name := range checked {
		withText[name] = true
	}
	for name := range i18nPending {
		if done[name] {
			t.Errorf("i18nPending 里的 %s 已实现 I18nAware，请从待办表删掉", name)
		}

		if !withText[name] {
			t.Errorf("i18nPending 里的 %s 扫描不到硬编码文案，请从待办表删掉", name)
		}
	}
	for name := range i18nExemptions {
		if done[name] {
			t.Errorf("i18nExemptions 里的 %s 已实现 I18nAware，豁免可以撤掉了", name)
		}
	}
	t.Logf("含硬编码文案的组件 %d 个（其中 %d 个已实现 I18nAware，%d 个待办，%d 个豁免）",
		len(checked), len(implemented), len(i18nPending), len(i18nExemptions))
}
