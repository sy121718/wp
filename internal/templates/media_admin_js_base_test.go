package templates

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var jsControlCreateRe = regexp.MustCompile(`createElement\(\s*['"](input|select|textarea)['"]\s*\)`)

var jsTagBaseClasses = map[string][]string{
	"input":    {"form-input", "wbs-native"},
	"select":   {"form-select", "wbs-native"},
	"textarea": {"form-textarea", "wbs-native"},
}

var jsSkipMarkerRe = regexp.MustCompile(`js-base-skip:\s*(\S[^\n]*)`)

var jsTypeAssignRe = regexp.MustCompile(`type\s*=\s*['"]([a-zA-Z]+)['"]`)

const jsScanRoot = "static/js"

var jsScanExcludedDirs = map[string]string{
	"workbench": "工作台检查器控件族的外观真源是 workbench.css，不吃后台基座类",
}

var jsFileExemptions = map[string]string{
	"icons.js": "公开站点图标选择器（.sky-ip-* 族）：外观真源是前台主题 CSS，不属于后台基座作用域",
	"rich-editor/dialog.js": "Trix 富文本对话框字段（.sre-dialog-field 族）：外观真源见 rich-editor.css，" +
		"基座的 form-input 尺寸会与对话框的紧凑排版打架",
	"ui/select.js": "WBUI.select 的自定义下拉：原生 select 在 enhance() 里被加上 wbs-native（ui/select.js:73），" +
		"实际外观走 .wbs 族（ui.css），不重复挂 form-select",
}

type jsControlHit struct {
	file   string
	line   int
	tag    string
	window string
}

const jsWindowLines = 12

func scanJSControlCreations(relPath, src string) []jsControlHit {
	lines := strings.Split(src, "\n")
	var hitLines []int
	for i, ln := range lines {
		if jsControlCreateRe.MatchString(ln) {
			hitLines = append(hitLines, i)
		}
	}
	var out []jsControlHit
	for k, i := range hitLines {
		end := i + jsWindowLines
		if k+1 < len(hitLines) && hitLines[k+1] < end {
			end = hitLines[k+1]
		}
		if end > len(lines) {
			end = len(lines)
		}
		m := jsControlCreateRe.FindStringSubmatch(lines[i])
		out = append(out, jsControlHit{
			file:   relPath,
			line:   i + 1,
			tag:    m[1],
			window: strings.Join(lines[i:end], "\n"),
		})
	}
	return out
}

func jsControlVerdict(h jsControlHit) (bool, string) {
	for _, cls := range jsTagBaseClasses[h.tag] {
		if strings.Contains(h.window, cls) {
			return true, "基座类 " + cls
		}
	}
	for _, m := range jsTypeAssignRe.FindAllStringSubmatch(h.window, -1) {
		if adminFormSkipTypes[strings.ToLower(m[1])] {
			return true, "非录入控件 type=" + strings.ToLower(m[1])
		}
	}
	if m := jsSkipMarkerRe.FindStringSubmatch(h.window); m != nil {
		return true, "文件内联豁免（" + strings.TrimSpace(m[1]) + "）"
	}
	return false, ""
}

func TestJSGeneratedControlsCarryBaseClass(t *testing.T) {
	var hits []jsControlHit
	hitFiles := map[string]int{}
	err := filepath.WalkDir(jsScanRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if _, skip := jsScanExcludedDirs[d.Name()]; skip {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".min.js") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, jsScanRoot+string(filepath.Separator)))
		hs := scanJSControlCreations(rel, string(data))
		if len(hs) > 0 {
			hitFiles[rel] = len(hs)
			hits = append(hits, hs...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 %s 失败: %v", jsScanRoot, err)
	}

	if len(hits) == 0 {
		t.Fatalf("在 %s 下没有扫到任何 createElement('input'|'select'|'textarea') —— 扫描面或正则已失效", jsScanRoot)
	}

	var bad []string
	for _, h := range hits {
		if ok, _ := jsControlVerdict(h); ok {
			continue
		}
		if reason, ok := jsFileExemptions[h.file]; ok {
			_ = reason
			continue
		}
		bad = append(bad, "  "+h.file+":"+strconv.Itoa(h.line)+" createElement('"+h.tag+"') 无基座类（期望 "+strings.Join(jsTagBaseClasses[h.tag], " / ")+"）")
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("动态生成的控件必须自带基座类（外观真源只有一个）：\n%s\n"+
			"参照 media-admin.js 的 mkBaseControl：新增字段一律经它创建；\n"+
			"确属别的控件族（外观真源在别处）时，在 JS 里写 `// js-base-skip: <理由>`，\n"+
			"或在本文件的 jsFileExemptions 里登记并写明归属。", strings.Join(bad, "\n"))
	}

	for f, reason := range jsFileExemptions {
		if hitFiles[f] == 0 {
			t.Errorf("jsFileExemptions 里的 %q 已不再命中动态控件（理由：%s）—— 条目应随之删除", f, reason)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("jsFileExemptions 里的 %q 没写理由", f)
		}
	}

	if _, ok := jsFileExemptions["media-admin.js"]; ok {
		t.Error("media-admin.js 不得登记为豁免 —— 它是本轮漏网的文件，必须每个动态控件都带基座类")
	}
}

func TestJSControlScannerBadSamples(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"裸写 text input（本轮漏网的原形）", "var a = document.createElement('input'); a.type = 'text';", false},
		{"裸写 textarea", "var a = document.createElement('textarea'); a.rows = 3;", false},
		{"裸写 select", "var a = document.createElement('select');", false},
		{"input 带 form-input", "var a = document.createElement('input'); a.className = 'form-input';", true},
		{"textarea 带 form-textarea", "var a = document.createElement('textarea'); a.className = 'form-textarea';", true},
		{"select 带 form-select", "var a = document.createElement('select'); a.className = 'form-select';", true},
		{"错配：input 挂 form-select 不算过", "var a = document.createElement('input'); a.className = 'form-select';", false},
		{"checkbox 不吃基座", "var a = document.createElement('input'); a.type = 'checkbox';", true},
		{"radio 不吃基座", "var a = document.createElement('input'); a.type = 'radio';", true},
		{"hidden 不吃基座", "var a = document.createElement('input'); a.type = 'hidden';", true},
		{"内联豁免缺理由 = 失败", "var a = document.createElement('input'); // js-base-skip:", false},
		{"内联豁免带理由 = 通过", "var a = document.createElement('input'); // js-base-skip: 前台控件族，外观在主题 CSS", true},
	}
	for _, c := range cases {
		hits := scanJSControlCreations("probe.js", c.src)
		if len(hits) != 1 {
			t.Errorf("%s: 期望扫到 1 个命中，实际 %d", c.name, len(hits))
			continue
		}
		got, why := jsControlVerdict(hits[0])
		if got != c.want {
			t.Errorf("%s: 判定 = %v（依据 %q），期望 %v", c.name, got, why, c.want)
		}
	}
}
