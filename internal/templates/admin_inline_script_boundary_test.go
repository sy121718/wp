package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var inlineScriptAllowance = map[string]struct {
	Count  int
	Reason string
}{
	"admin/layout.html": {1, "首屏主题防闪烁：必须在 CSS 加载前从 localStorage 读主题并写 data-theme，外链脚本会晚一拍，页面先白后黑"},
	"admin/login.html":  {2, "同上一条（登录页不 extends layout，自己带一份）；第二处是验证码图片的刷新逻辑，只操作本页一个 img 与一个按钮"},
	"admin/project/theme_settings.html": {1, "把同页内嵌的 ThemeSettings JSON 按提交点分键回显到各表单字段：数据就在本页，" +
		"拆成外链要先约定取数方式，反而多一层"},
	"admin/page/page_redirects.html": {1, "6 行：给新增重定向行做编号占位（DOMContentLoaded 一次），无网络、无业务口径"},
}

var (
	scriptTagRe  = regexp.MustCompile(`(?is)<script\b([^>]*)>`)
	jetCommentRe = regexp.MustCompile(`(?s)\{\*.*?\*\}`)
	attrsRe      = regexp.MustCompile(`(?i)\b(src|type)\s*=\s*("[^"]*"|'[^']*')`)
)

func inlineScripts(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取模板失败 %s: %v", path, err)
	}
	text := jetCommentRe.ReplaceAllString(string(raw), "")
	count := 0
	for _, m := range scriptTagRe.FindAllStringSubmatch(text, -1) {
		attrs := m[1]
		hasSrc := false
		isJSON := false
		for _, a := range attrsRe.FindAllStringSubmatch(attrs, -1) {
			value := strings.Trim(strings.TrimSpace(a[2]), `"'`)
			switch strings.ToLower(a[1]) {
			case "src":
				hasSrc = true
			case "type":
				isJSON = strings.Contains(strings.ToLower(value), "json")
			}
		}
		if hasSrc || isJSON {
			continue
		}
		count++
	}
	return count
}

func TestAdminTemplatesHaveNoNewInlineScripts(t *testing.T) {
	root := "admin"
	seen := map[string]int{}
	var unexpected []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		rel := filepath.ToSlash(path)
		n := inlineScripts(t, path)
		if n == 0 {
			return nil
		}
		seen[rel] = n
		allowance, ok := inlineScriptAllowance[rel]
		if !ok {
			unexpected = append(unexpected, rel)
			return nil
		}
		if allowance.Count != n {
			t.Errorf("%s 的内联脚本处数变了：登记 %d 处，实际 %d 处 —— 新增的那一处请抽到 internal/templates/static/js/ui/ 下（并在模板里用 <script src> 引入）",
				rel, allowance.Count, n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历后台模板失败: %v", err)
	}

	sort.Strings(unexpected)
	for _, rel := range unexpected {
		t.Errorf("%s 出现未登记的内联业务脚本：抽到 internal/templates/static/js/ui/ 下（形态见 bundle_members.js），"+
			"确实必须内联时在 inlineScriptAllowance 里登记处数与理由", rel)
	}

	var stale []string
	for rel := range inlineScriptAllowance {
		if seen[rel] == 0 {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	for _, rel := range stale {
		t.Errorf("inlineScriptAllowance 里的 %s 已不再有内联脚本：删掉这一条", rel)
	}
}
