package templates

// admin_inline_script_boundary_test.go — 后台模板不得新增内联业务脚本。
//
// 为什么需要它：内联 <script> 与模板同生共死 —— 不能单测、不能被打包/缓存策略统一处理、
//  CSP 收紧时整页失效，而它的改动散在 HTML 里靠 review 看不见。product 的捆绑配置页
//  曾经有 150 行成员解析内联脚本，迁到 static/js/ui/bundle_members.js 后才第一次有单测
//  （同目录 bundle_members.test.cjs）。本测试把「不许再长回来」钉成判据：
//
//   · 白名单里登记的是**既有存量**，逐条写明为什么必须内联（主题防闪烁要在 CSS 之前跑、
//     数据回显要读同页 JSON …），并登记该文件允许的处数；
//   · 双向失败：出现未登记的内联脚本 → 失败；登记了却不再命中 → 也失败（条目不得退化成注释）。
//
// 判据只看「真脚本」：<script src=…> 是外部引用，type="application/json" 是数据块，都不算。
import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// inlineScriptAllowance 既有内联脚本的登记表：相对 internal/templates 的路径 → 处数与理由。
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

// inlineScripts 返回一个模板文件里的内联业务脚本数（剥掉 Jet 注释后判定）。
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

	// 反向：登记了却不再命中（迁移已完成）时把条目删掉，别让登记表退化成注释。
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
