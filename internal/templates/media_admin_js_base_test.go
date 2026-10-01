package templates

// media_admin_js_base_test.go — 「可见表单控件必须带基座类」的 **JS 侧**契约（审计条目 UIK-009 的续集）。
//
// 这一条约束有两个侧面，谁也不能少：
//   · 模板侧 —— admin_form_base_test.go 扫模板里的静态控件（当前 594 个，裸写 0）；
//   · JS 侧 —— **本文件**扫 static/js 里 createElement('input'|'select'|'textarea') 的动态生成点。
//
// 分成两份不是洁癖，是 2026-10-01 的真实漏网：media-admin.js 在媒体详情面板里动态生成
// textarea / select / input[type=text]，模板门禁一个都扫不到，于是这三个控件一直吃着
// media-lib.css 的容器兜底外观（7px 10px / #e5e7eb / 8px），与同一个抽屉里模板渲染的基座控件
// （8px 12px / #c6ccd4 / 6px）并存成两种外观 —— 详细对照见 task-7 报告。
// 兜底规则与动态控件同时收口：控件带 form-input / form-select / form-textarea，容器只留布局。
//
// **新增「JS 动态生成控件」的文件时，判据加进本文件，不要另开一份测试**：一旦「控件必须带基座类」
// 有了第二个真源，就是这轮在收的东西（容器兜底曾经就是第二个真源）。
//
// 扫描面与豁免口径写在下面三个变量上，各有理由；豁免不是逃生舱 —— 条目必须命中，过期即失败。

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

// jsControlCreateRe 只认 createElement('input'|'select'|'textarea') 这种字面量形态。
// 选择理由：动态标签名（createElement(tag)）在本仓只出现在 mkBaseControl 这类工厂里，
// 工厂自己就是「必须带基座类」的实现，不该被当作裸写点；而字面量正则能精确指到控件创建的位置，
// 比在此处引入 JS 解析器稳。代价是识别不了 `createElement('INPUT')` 这类大小写变体 —— 见下面的
// 自检用例，真实代码不写这种形态，出现时会先被 code review 拦下。
var jsControlCreateRe = regexp.MustCompile(`createElement\(\s*['"](input|select|textarea)['"]\s*\)`)

// jsTagBaseClasses 每个标签认可的基座类。标签与类必须对应：input 挂 form-select 不算覆盖
// （自检里有这条坏样本）—— 否则「有类名就算过」会把错配放过去。
var jsTagBaseClasses = map[string][]string{
	"input":    {"form-input", "wbs-native"},
	"select":   {"form-select", "wbs-native"},
	"textarea": {"form-textarea", "wbs-native"},
}

// jsSkipMarkerRe 文件内联豁免：`// js-base-skip: <理由>`。理由必须非空 ——
// 空理由等于没有理由，直接判失败。
var jsSkipMarkerRe = regexp.MustCompile(`js-base-skip:\s*(\S[^\n]*)`)

// jsTypeAssignRe 取窗口里 `x.type = 'checkbox'` 这类显式类型赋值，用于识别「不归基座管的控件」。
// 判定表复用 admin_form_base_test.go 的 adminFormSkipTypes —— 两份口径只留一份。
var jsTypeAssignRe = regexp.MustCompile(`type\s*=\s*['"]([a-zA-Z]+)['"]`)

// jsScanRoot 与 workbench 的边界：工作台检查器控件族（.wb-unit-value / .wb-unit-select /
// .wb-color-text …）的外观真源是 workbench.css，不吃后台基座 —— 把它们扫进来只会得到一片
// 与本次收敛无关的误报。workbench 目录的控件统一性由工作台自己的口径负责。
const jsScanRoot = "static/js"

var jsScanExcludedDirs = map[string]string{
	"workbench": "工作台检查器控件族的外观真源是 workbench.css，不吃后台基座类",
}

// jsFileExemptions 文件级豁免：**只给「外观真源明确不在后台基座」的既有控件族**，
// 理由必须写清楚归属。三个条件同时成立才允许登记：① 该族的样式由自己的 CSS 定义；
// ② 该文件不在这次收敛的作用域里（改它会把本轮改动扩散出去）；③ 理由里能指出接管的代码位置。
// 条目必须命中（该文件确实还有动态控件），否则测试失败 —— 防「只增不减」的豁免表。
var jsFileExemptions = map[string]string{
	"icons.js": "公开站点图标选择器（.sky-ip-* 族）：外观真源是前台主题 CSS，不属于后台基座作用域",
	"rich-editor/dialog.js": "Trix 富文本对话框字段（.sre-dialog-field 族）：外观真源见 rich-editor.css，" +
		"基座的 form-input 尺寸会与对话框的紧凑排版打架",
	"ui/select.js": "WBUI.select 的自定义下拉：原生 select 在 enhance() 里被加上 wbs-native（ui/select.js:73），" +
		"实际外观走 .wbs 族（ui.css），不重复挂 form-select",
}

// jsControlHit 一个动态控件生成点及其判定窗口。
type jsControlHit struct {
	file   string
	line   int // 1-based
	tag    string
	window string
}

// jsWindowLines 判定窗口的行数上限。窗口从命中行开始，到「下一个 createElement 命中行」为止，
// 最多这么多行 —— 只看这个控件自己的创建代码，避免相邻控件的基座类把它「误救」。
const jsWindowLines = 12

// scanJSControlCreations 扫一份 JS 源里的动态控件生成点，并给出各自的判定窗口。
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

// jsControlVerdict 判定单个命中点。返回 (是否覆盖, 依据)；未覆盖时依据为空串。
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

// TestJSGeneratedControlsCarryBaseClass 是本文件的主判据：static/js 下每个动态生成的
// input / select / textarea 都必须带对应基座类，或按上面的口径豁免。
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
	// 防「扫描退化成空转」：命中数为 0 说明扫描面或正则已经失效，那这条门禁就没有在守任何东西。
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

	// 豁免清单必须命中：条目过期（文件已没有动态控件 / 已改名）就失败，防只增不减。
	for f, reason := range jsFileExemptions {
		if hitFiles[f] == 0 {
			t.Errorf("jsFileExemptions 里的 %q 已不再命中动态控件（理由：%s）—— 条目应随之删除", f, reason)
		}
		if strings.TrimSpace(reason) == "" {
			t.Errorf("jsFileExemptions 里的 %q 没写理由", f)
		}
	}
	// 本轮修的这个文件永远不许进豁免表：它正是漏网的地方，进了就等于把漏洞写进契约。
	if _, ok := jsFileExemptions["media-admin.js"]; ok {
		t.Error("media-admin.js 不得登记为豁免 —— 它是本轮漏网的文件，必须每个动态控件都带基座类")
	}
}

// TestJSControlScannerBadSamples 自检：判据本身必须能变红。
// 只跑「实现正确」的路径而不验坏样本，是门禁退化成摆设的常见方式（扫描面写错、正则写歪、
// 或类型豁免被放宽成「任何页面都能过」时，主判据仍然会绿）。
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
